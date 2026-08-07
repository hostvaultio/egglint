package lint

import (
	"strings"

	"github.com/hostvaultio/egglint/egg"
	"github.com/hostvaultio/egglint/shell"
)

var ruleNoInstallScript = &Rule{
	ID:         "EGG010",
	Name:       "no-install-script",
	Summary:    "The egg has no installation script",
	Severity:   Warning,
	NeedsParse: true,
	Docs: `Most eggs need an install script to fetch the server binary before first
boot. An empty script is legitimate for eggs whose image already contains
everything, so this is a warning rather than an error — but for anything that
downloads a server it means the install step does nothing at all.`,
	Check: func(c *Context) {
		if strings.TrimSpace(c.Egg().Scripts.Installation.Script) == "" {
			c.ReportHelp(egg.Ptr("scripts", "installation"),
				"If the image ships everything the server needs, this warning can be disabled for the file.",
				"no installation script; nothing will run during server installation")
		}
	},
}

var ruleUnknownEntrypoint = &Rule{
	ID:         "EGG011",
	Name:       "unknown-entrypoint",
	Summary:    "Install entrypoint is not a known shell",
	Severity:   Error,
	NeedsParse: true,
	Docs: `The entrypoint names the interpreter the install container runs the script
with. If it is not present in the install image the installation fails
immediately. In practice only bash, ash and sh are safe choices, and the
installer images built for the panel provide those.`,
	Check: func(c *Context) {
		if strings.TrimSpace(c.Egg().Scripts.Installation.Script) == "" {
			return
		}
		ep := c.Egg().Entrypoint()
		if !shell.IsKnownEntrypoint(ep) {
			c.ReportHelp(egg.Ptr("scripts", "installation", "entrypoint"),
				"Use bash for debian-based installer images or ash for alpine ones.",
				"install entrypoint %q is not a known shell", ep)
		}
	},
}

var ruleScriptSyntax = &Rule{
	ID:         "EGG012",
	Name:       "script-syntax",
	Summary:    "Install script fails its interpreter's syntax check",
	Severity:   Error,
	NeedsParse: true,
	Docs: `The install script is parsed with the interpreter declared as its
entrypoint (the equivalent of "bash -n"). A script that does not parse fails at
install time, after the server has already been created, which makes the failure
much more expensive to diagnose than a check at review time.`,
	Check: func(c *Context) {
		if c.Script == "" || !c.Shell.Available() {
			return
		}
		problem, err := c.Shell.Check(c.Script)
		if err != nil {
			c.ReportScript(0, "", "could not run the syntax check: %v", err)
			return
		}
		if problem == nil {
			return
		}
		help := ""
		if c.Shell.Approximate {
			help = c.Shell.Note
		}
		c.ReportScript(problem.Line, help, "%s", problem.Message)
	},
}

var ruleCRLF = &Rule{
	ID:         "EGG013",
	Name:       "crlf-line-endings",
	Summary:    "Install script contains CRLF line endings",
	Severity:   Info,
	NeedsParse: true,
	Docs: `The install script is stored with Windows line endings, usually because the
egg was edited on Windows or passed through an editor that rewrote it.

This is reported as advisory, not as an error, because the panel normalises line
endings before wings ever sees the script: the remote endpoint that serves the
install script applies str_replace(["\r\n", "\n", "\r"], "\n", ...) to it. A
carriage return that would genuinely break a shell — "then\r" is not the "then"
keyword — is therefore removed in transit, and the majority of published eggs
carry CRLF without ever failing because of it.

It is still worth fixing. CRLF makes diffs noisy, and any tooling that runs the
script outside the panel's delivery path does not get the same normalisation.`,
	Check: func(c *Context) {
		if c.Script == "" {
			return
		}
		if !strings.Contains(c.Script, "\r") {
			return
		}
		line := 1
		for i, ch := range c.Script {
			if ch == '\r' {
				line = 1 + strings.Count(c.Script[:i], "\n")
				break
			}
		}
		c.ReportScript(line,
			"Convert the script to LF-only line endings before re-exporting the egg.",
			"carriage return in the install script; a POSIX shell treats it as part of the preceding word, not as whitespace")
	},
}

var ruleUncheckedDownload = &Rule{
	ID:         "EGG014",
	Name:       "unchecked-download",
	Summary:    "curl is used without --fail, so HTTP errors are silently saved",
	Severity:   Warning,
	NeedsParse: true,
	Docs: `Without -f/--fail, curl exits 0 on an HTTP error and writes the error
response body to the output file. A 404 page therefore lands on disk named
server.jar, the install script reports success, and the server fails to boot with
an error that points nowhere near the real cause.

Add -f (or --fail) so curl exits non-zero on 4xx and 5xx. When the body is piped
into a shell the same flag prevents an error page being executed.`,
	Check: func(c *Context) {
		if c.Script == "" {
			return
		}
		for _, inv := range findCurlInvocations(scriptLines(c.Script)) {
			if inv.HasFailFlag {
				continue
			}
			switch {
			case inv.PipesToShell:
				c.ReportScript(inv.Line,
					"Add -f so an error response is never piped into the interpreter.",
					"curl without --fail pipes its response into a shell; an HTTP error page would be executed")
			case inv.WritesFile:
				c.ReportScript(inv.Line,
					"Add -f (e.g. curl -fsSL -o file URL) so HTTP errors fail the install.",
					"curl without --fail writes to a file; an HTTP error page is saved as if it were the download")
			}
		}
	},
}

var ruleUncheckedFetch = &Rule{
	ID:         "EGG017",
	Name:       "unchecked-fetch",
	Summary:    "curl is used without --fail for a request whose body is not saved",
	Severity:   Info,
	NeedsParse: true,
	Docs: `curl without -f/--fail exits 0 on an HTTP error, so a request that looks up a
version number or queries an API returns the error page's body to the script
instead of the value it expected. The consequence is milder than a bad download
landing on disk — which EGG014 covers — because the resulting value is usually
obviously wrong, so this is advisory.`,
	Check: func(c *Context) {
		if c.Script == "" {
			return
		}
		for _, inv := range findCurlInvocations(scriptLines(c.Script)) {
			if inv.HasFailFlag || inv.WritesFile || inv.PipesToShell {
				continue
			}
			c.ReportScript(inv.Line,
				"Add -f so HTTP errors are visible to the script.",
				"curl without --fail exits 0 on HTTP errors, so an error response is used as if it were the real value")
		}
	},
}

var ruleNoArtifactAssertion = &Rule{
	ID:         "EGG015",
	Name:       "no-artifact-assertion",
	Summary:    "Script downloads files but never checks one arrived",
	Severity:   Warning,
	NeedsParse: true,
	Docs: `An install script that downloads a server binary should confirm the file
exists and is non-empty before exiting. Wings treats a zero exit as a successful
installation and marks the server ready; without an assertion, a failed or
truncated download produces a server that is "installed" but cannot start.

Add a check before the end of the script:

    if [ ! -s /mnt/server/server.jar ]; then
        echo "download failed: server.jar is missing or empty"
        exit 1
    fi`,
	Check: func(c *Context) {
		if c.Script == "" {
			return
		}
		lines := scriptLines(c.Script)
		if !hasDownload(lines) {
			return
		}
		if hasArtifactAssertion(lines) {
			return
		}
		c.ReportScript(0,
			`Add a test such as: [ -s /mnt/server/server.jar ] || exit 1`,
			"the install script downloads files but never asserts that one exists and is non-empty, so a failed download still reports a successful install")
	},
}

var ruleNoErrExit = &Rule{
	ID:         "EGG016",
	Name:       "no-errexit",
	Summary:    "Install script does not enable errexit",
	Severity:   Info,
	NeedsParse: true,
	Docs: `Without "set -e" a shell script keeps going after a command fails and exits
with the status of the last command it ran. An install script that fails halfway
through therefore usually still exits 0, and wings marks the installation
successful.

"set -e" is not a complete safety net — it does not fire inside pipelines
without pipefail, nor for commands in a condition — which is why an explicit
artifact assertion (EGG015) is also worth having.`,
	Check: func(c *Context) {
		if c.Script == "" {
			return
		}
		lines := scriptLines(c.Script)
		if len(lines) == 0 || hasErrExit(lines) {
			return
		}
		c.ReportScript(0,
			`Add "set -e" near the top of the script (or "set -euo pipefail" for bash).`,
			"the install script does not enable errexit, so a failing command part-way through still exits 0 and the install is recorded as successful")
	},
}

func init() {
	register(
		ruleNoInstallScript,
		ruleUnknownEntrypoint,
		ruleScriptSyntax,
		ruleCRLF,
		ruleUncheckedDownload,
		ruleUncheckedFetch,
		ruleNoArtifactAssertion,
		ruleNoErrExit,
	)
}
