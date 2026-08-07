// Package shell performs static syntax checking of egg installation scripts by
// delegating to the interpreter the egg declares as its entrypoint.
//
// The panel runs the install script inside a container with a specific shell, so
// the only faithful check is the real interpreter's own parser (`sh -n`). Where
// that exact interpreter is unavailable a stand-in is used and the result is
// marked approximate — callers are expected to surface that, because a check
// that silently did not run is worse than no check at all.
package shell

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Checker syntax-checks scripts for one entrypoint.
type Checker struct {
	// Entrypoint is the entrypoint declared by the egg, e.g. "bash" or "ash".
	Entrypoint string
	// Argv is the command used to perform the check, empty when unavailable.
	Argv []string
	// Approximate is true when Argv is a stand-in for the declared interpreter
	// (for example dash standing in for busybox ash).
	Approximate bool
	// Note explains an approximate or unavailable checker.
	Note string
}

// Available reports whether a syntax check can actually be performed.
func (c *Checker) Available() bool { return c != nil && len(c.Argv) > 0 }

// ErrUnknownEntrypoint is returned for entrypoints egglint does not model.
var ErrUnknownEntrypoint = errors.New("unknown install entrypoint")

// KnownEntrypoints lists the entrypoints the panel realistically uses.
var KnownEntrypoints = []string{"bash", "ash", "sh", "dash", "zsh"}

// NormalizeEntrypoint reduces an entrypoint to its interpreter name. Eggs
// legitimately declare an absolute path such as "/bin/bash", which names exactly
// the same shell as "bash".
func NormalizeEntrypoint(ep string) string {
	ep = strings.TrimSpace(ep)
	if i := strings.LastIndex(ep, "/"); i >= 0 {
		ep = ep[i+1:]
	}
	return ep
}

// IsKnownEntrypoint reports whether the entrypoint is one egglint understands.
func IsKnownEntrypoint(ep string) bool {
	ep = NormalizeEntrypoint(ep)
	for _, k := range KnownEntrypoints {
		if ep == k {
			return true
		}
	}
	return false
}

// For builds a Checker for the declared entrypoint, resolving the best
// available interpreter on this machine.
func For(declared string) (*Checker, error) {
	entrypoint := NormalizeEntrypoint(declared)
	c := &Checker{Entrypoint: entrypoint}
	if !IsKnownEntrypoint(entrypoint) {
		return c, fmt.Errorf("%w: %q", ErrUnknownEntrypoint, declared)
	}

	switch entrypoint {
	case "bash", "zsh":
		if p, err := exec.LookPath(entrypoint); err == nil {
			c.Argv = []string{p, "-n"}
			return c, nil
		}
	case "ash":
		// busybox ash is what alpine installer images actually run.
		if p, err := exec.LookPath("busybox"); err == nil && busyboxHasAsh(p) {
			c.Argv = []string{p, "ash", "-n"}
			return c, nil
		}
		if p, err := exec.LookPath("dash"); err == nil {
			c.Argv = []string{p, "-n"}
			c.Approximate = true
			c.Note = "busybox not found; using dash as a POSIX stand-in for ash. " +
				"dash is stricter about some constructs busybox tolerates, so a " +
				"reported error may not reproduce in the install container."
			return c, nil
		}
	case "sh", "dash":
		if p, err := exec.LookPath("dash"); err == nil {
			c.Argv = []string{p, "-n"}
			return c, nil
		}
	}

	// Last resort: whatever /bin/sh is on this host.
	if p, err := exec.LookPath("sh"); err == nil {
		c.Argv = []string{p, "-n"}
		c.Approximate = true
		if c.Note == "" {
			c.Note = fmt.Sprintf("%s not found; using /bin/sh as a stand-in, "+
				"which may not match the install container's interpreter.", entrypoint)
		}
		return c, nil
	}

	c.Note = fmt.Sprintf("no interpreter available to syntax-check %q scripts; check skipped", entrypoint)
	return c, nil
}

// Problem is a syntax error located within the script.
type Problem struct {
	// Line is the 1-based line within the install script, 0 when unknown.
	Line int
	// Message is the interpreter's own diagnostic, with temp paths stripped.
	Message string
}

var lineRe = regexp.MustCompile(`(?m):(?:\s*line)?\s*(\d+):`)

// Check runs the syntax check. It returns nil when the script parses cleanly.
//
// Carriage returns are always normalised away first, because that is what the
// panel itself does: the endpoint wings fetches the install script from applies
// str_replace(["\r\n", "\n", "\r"], "\n", ...) before returning it, so the
// interpreter in the install container never sees a CRLF script.
//
// This matters more than it sounds. CRLF genuinely breaks bash, dash and busybox
// ash — a trailing carriage return stops `then` being the `then` keyword — so
// checking the raw script would report a syntax error for the large majority of
// published eggs, none of which fail in practice. CRLF is still worth knowing
// about, and is reported separately by its own rule at an honest severity.
func (c *Checker) Check(script string) (*Problem, error) {
	if !c.Available() {
		return nil, nil
	}

	toCheck := strings.ReplaceAll(script, "\r\n", "\n")
	toCheck = strings.ReplaceAll(toCheck, "\r", "\n")

	dir, err := os.MkdirTemp("", "egglint-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	file := filepath.Join(dir, "install")
	if err := os.WriteFile(file, []byte(toCheck), 0o600); err != nil {
		return nil, err
	}

	argv := append(append([]string{}, c.Argv...), shoptFlags(c.Entrypoint, toCheck)...)
	argv = append(argv, file)
	cmd := exec.Command(argv[0], argv[1:]...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil, nil
	}

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return nil, fmt.Errorf("running %s: %w", argv[0], err)
	}

	msg := strings.TrimSpace(string(out))
	line := 0
	if m := lineRe.FindStringSubmatch(msg); m != nil {
		if n, convErr := strconv.Atoi(m[1]); convErr == nil {
			line = n
		}
	}
	// Interpreters prefix diagnostics with the temp path; it is noise to a user.
	msg = strings.ReplaceAll(msg, file, "install script")
	msg = strings.ReplaceAll(msg, dir, "")
	if msg == "" {
		msg = "syntax check failed"
	}
	return &Problem{Line: line, Message: firstLine(msg)}, nil
}

// parserShopts are the shell options that change how bash *parses* a script, as
// opposed to how it behaves at runtime.
//
// They need special handling because `-n` parses the whole script without
// executing any of it, so a `shopt -s extglob` on line 2 has not taken effect by
// the time the parser reaches the extended glob on line 12. The script is
// perfectly valid when actually run — bash executes as it reads — but a naive
// syntax check reports an error. Enabling the option up front with `-O` makes
// the check match what really happens.
var parserShopts = map[string]bool{"extglob": true}

var shoptRe = regexp.MustCompile(`(?m)^[ \t]*shopt[ \t]+-s[ \t]+([^\n;&|#]+)`)

// shoptFlags returns the `-O name` flags needed to parse the script the way the
// shell would when running it.
func shoptFlags(entrypoint, script string) []string {
	if entrypoint != "bash" {
		return nil
	}
	var flags []string
	seen := map[string]bool{}
	for _, m := range shoptRe.FindAllStringSubmatch(script, -1) {
		for _, opt := range strings.Fields(m[1]) {
			if parserShopts[opt] && !seen[opt] {
				seen[opt] = true
				flags = append(flags, "-O", opt)
			}
		}
	}
	return flags
}

func busyboxHasAsh(path string) bool {
	return exec.Command(path, "ash", "-c", "true").Run() == nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}
