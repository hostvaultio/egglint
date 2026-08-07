package lint

import (
	"strings"
)

// logicalLine is one shell command after line continuations have been joined,
// paired with the 1-based line it started on so findings point at the source.
type logicalLine struct {
	Text string
	Line int
}

// scriptLines splits an install script into logical lines, joining backslash
// continuations and dropping whole-line comments.
//
// Comments are only stripped when the line starts with '#'. Stripping from the
// first '#' anywhere would mangle URLs containing fragments, which install
// scripts routinely contain.
func scriptLines(script string) []logicalLine {
	raw := strings.Split(strings.ReplaceAll(script, "\r\n", "\n"), "\n")
	var out []logicalLine

	i := 0
	for i < len(raw) {
		startLine := i + 1
		text := strings.TrimRight(raw[i], "\r")

		for strings.HasSuffix(strings.TrimRight(text, " \t"), `\`) && i+1 < len(raw) {
			trimmed := strings.TrimRight(text, " \t")
			text = trimmed[:len(trimmed)-1] + " " + strings.TrimSpace(strings.TrimRight(raw[i+1], "\r"))
			i++
		}
		i++

		if trimmed := strings.TrimSpace(text); trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		out = append(out, logicalLine{Text: text, Line: startLine})
	}
	return out
}

// commandTokens does a light word split that keeps quoted strings together. It
// is not a shell parser — it only needs to be good enough to spot flags.
func commandTokens(line string) []string {
	var tokens []string
	var cur strings.Builder
	var quote byte

	flush := func() {
		if cur.Len() > 0 {
			tokens = append(tokens, cur.String())
			cur.Reset()
		}
	}

	for i := 0; i < len(line); i++ {
		ch := line[i]
		switch {
		case quote != 0:
			if ch == quote {
				quote = 0
			} else {
				cur.WriteByte(ch)
			}
		case ch == '\'' || ch == '"':
			quote = ch
		case ch == ' ' || ch == '\t':
			flush()
		case ch == ';' || ch == '|' || ch == '&':
			flush()
			tokens = append(tokens, string(ch))
		default:
			cur.WriteByte(ch)
		}
	}
	flush()
	return tokens
}

// curlInvocation describes one curl call found in the script.
type curlInvocation struct {
	Line int
	// HasFailFlag is true when -f, --fail or --fail-with-body is present,
	// including inside a combined short-flag cluster such as -fsSL.
	HasFailFlag bool
	// WritesFile is true when the call saves its body to a named file.
	WritesFile bool
	// PipesToShell is true when the body is piped straight into an interpreter.
	PipesToShell bool
}

// findCurlInvocations locates curl calls and inspects their flags.
func findCurlInvocations(lines []logicalLine) []curlInvocation {
	var out []curlInvocation
	for _, ll := range lines {
		tokens := commandTokens(ll.Text)
		for idx, tok := range tokens {
			if base(tok) != "curl" {
				continue
			}
			inv := curlInvocation{Line: ll.Line}
			// Scan this curl's arguments up to the next command separator.
			for j := idx + 1; j < len(tokens); j++ {
				t := tokens[j]
				if t == ";" || t == "&" {
					break
				}
				if t == "|" {
					// Look at what the body is piped into.
					if j+1 < len(tokens) && isShellInterpreter(base(tokens[j+1])) {
						inv.PipesToShell = true
					}
					break
				}
				switch {
				case t == "--fail", t == "--fail-early", t == "--fail-with-body":
					inv.HasFailFlag = true
				case t == "--output", t == "--remote-name", t == "-O", t == "--output-dir":
					inv.WritesFile = true
				case t == "-o":
					inv.WritesFile = true
					if j+1 < len(tokens) && tokens[j+1] == "/dev/null" {
						inv.WritesFile = false
					}
				case strings.HasPrefix(t, "--"):
					// Long flag we do not model.
				case strings.HasPrefix(t, "-") && len(t) > 1:
					// Combined short flags, e.g. -fsSL or -sSLo.
					cluster := t[1:]
					if strings.ContainsRune(cluster, 'f') {
						inv.HasFailFlag = true
					}
					if strings.ContainsRune(cluster, 'o') || strings.ContainsRune(cluster, 'O') {
						inv.WritesFile = true
					}
				}
			}
			// A redirect on the same line also writes the body to a file.
			if strings.Contains(ll.Text, ">") && !strings.Contains(ll.Text, ">/dev/null") &&
				!strings.Contains(ll.Text, "> /dev/null") {
				inv.WritesFile = true
			}
			out = append(out, inv)
		}
	}
	return out
}

// hasDownload reports whether the script fetches anything from the network.
func hasDownload(lines []logicalLine) bool {
	for _, ll := range lines {
		for _, tok := range commandTokens(ll.Text) {
			switch base(tok) {
			case "curl", "wget", "aria2c":
				return true
			}
		}
	}
	return false
}

// hasArtifactAssertion reports whether the script tests that some file exists
// and is non-empty. This is the generalised form of the boot-artifact check:
// whatever was downloaded, something must confirm it actually arrived.
func hasArtifactAssertion(lines []logicalLine) bool {
	for _, ll := range lines {
		tokens := commandTokens(ll.Text)
		for i, tok := range tokens {
			// Match `[ -s f ]`, `[[ -s f ]]`, `test -s f`, and negated forms.
			if tok != "[" && tok != "[[" && base(tok) != "test" {
				continue
			}
			for j := i + 1; j < len(tokens) && j <= i+3; j++ {
				if tokens[j] == "-s" || tokens[j] == "-f" || tokens[j] == "-e" {
					return true
				}
			}
		}
	}
	return false
}

// hasErrExit reports whether the script aborts on the first failing command.
func hasErrExit(lines []logicalLine) bool {
	for _, ll := range lines {
		tokens := commandTokens(ll.Text)
		for i, tok := range tokens {
			// Only treat `set` as the builtin when it actually starts a command;
			// otherwise "echo set -e is mentioned" would count as enabling it.
			if tok != "set" || !startsCommand(tokens, i) {
				continue
			}
			for j := i + 1; j < len(tokens); j++ {
				t := tokens[j]
				if t == "-o" && j+1 < len(tokens) && tokens[j+1] == "errexit" {
					return true
				}
				if strings.HasPrefix(t, "-") && !strings.HasPrefix(t, "--") &&
					strings.ContainsRune(t[1:], 'e') {
					return true
				}
			}
		}
	}
	return false
}

// startsCommand reports whether the token at i is in command position: either
// first on the line, or immediately after a separator.
func startsCommand(tokens []string, i int) bool {
	if i == 0 {
		return true
	}
	switch tokens[i-1] {
	case ";", "|", "&":
		return true
	}
	return false
}

// base strips any directory prefix from a command word so /usr/bin/curl and
// curl are treated the same.
func base(tok string) string {
	if i := strings.LastIndex(tok, "/"); i >= 0 {
		return tok[i+1:]
	}
	return tok
}

func isShellInterpreter(name string) bool {
	switch name {
	case "sh", "bash", "ash", "dash", "zsh", "python", "python3", "perl":
		return true
	}
	return false
}
