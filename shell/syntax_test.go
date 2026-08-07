package shell

import (
	"os/exec"
	"strings"
	"testing"
)

func TestNormalizeEntrypoint(t *testing.T) {
	cases := map[string]string{
		"bash":         "bash",
		"/bin/bash":    "bash",
		"/usr/bin/ash": "ash",
		"  /bin/sh  ":  "sh",
		"someshell":    "someshell",
	}
	for in, want := range cases {
		if got := NormalizeEntrypoint(in); got != want {
			t.Errorf("NormalizeEntrypoint(%q) = %q, want %q", in, got, want)
		}
	}
}

// An absolute path names the same interpreter as the bare command; treating
// "/bin/bash" as unknown produced a large number of false positives against real
// published eggs.
func TestAbsolutePathEntrypointIsKnown(t *testing.T) {
	for _, ep := range []string{"/bin/bash", "/usr/bin/ash", "bash", "ash", "sh"} {
		if !IsKnownEntrypoint(ep) {
			t.Errorf("%q should be a known entrypoint", ep)
		}
	}
	if IsKnownEntrypoint("nodejs") {
		t.Error("nodejs should not be a known shell entrypoint")
	}
}

func TestShoptFlagsDetectsExtglob(t *testing.T) {
	got := shoptFlags("bash", "#!/bin/bash\nshopt -s extglob\nrm !(keep)\n")
	if len(got) != 2 || got[0] != "-O" || got[1] != "extglob" {
		t.Errorf("expected [-O extglob], got %v", got)
	}
	if flags := shoptFlags("ash", "shopt -s extglob\n"); flags != nil {
		t.Errorf("shopt is bash-only, got %v", flags)
	}
	if flags := shoptFlags("bash", "shopt -s nullglob\n"); flags != nil {
		t.Errorf("only parser-affecting options should be passed, got %v", flags)
	}
}

func requireShell(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s not available", name)
	}
}

func TestCheckAcceptsValidScript(t *testing.T) {
	requireShell(t, "bash")
	c, err := For("bash")
	if err != nil {
		t.Fatal(err)
	}
	problem, err := c.Check("set -e\ncd /tmp\nif [ -d /tmp ]; then echo yes; fi\n")
	if err != nil {
		t.Fatal(err)
	}
	if problem != nil {
		t.Errorf("expected no problem, got %+v", problem)
	}
}

func TestCheckReportsSyntaxErrorWithLine(t *testing.T) {
	requireShell(t, "bash")
	c, _ := For("bash")
	problem, err := c.Check("echo one\nif [ -d /tmp ]; then\necho two\n")
	if err != nil {
		t.Fatal(err)
	}
	if problem == nil {
		t.Fatal("expected a syntax error for the unterminated if")
	}
	if problem.Line == 0 {
		t.Error("expected a line number in the reported problem")
	}
	if strings.Contains(problem.Message, "/tmp/egglint-") {
		t.Errorf("temp path leaked into the message: %q", problem.Message)
	}
}

// The panel normalises line endings before wings receives the script, so a CRLF
// script must not be reported as a syntax error.
func TestCheckIgnoresCarriageReturns(t *testing.T) {
	requireShell(t, "bash")
	c, _ := For("bash")
	problem, err := c.Check("cd /tmp\r\nif [ -d /tmp ]; then\r\n  echo yes\r\nfi\r\n")
	if err != nil {
		t.Fatal(err)
	}
	if problem != nil {
		t.Errorf("CRLF must not produce a syntax error, got %+v", problem)
	}
}

// `bash -n` never executes `shopt -s extglob`, so an extended glob later in the
// script would be misreported without the -O flag.
func TestCheckHandlesExtglob(t *testing.T) {
	requireShell(t, "bash")
	c, _ := For("bash")
	problem, err := c.Check("shopt -s extglob\nrm -rf !(data|keep)\n")
	if err != nil {
		t.Fatal(err)
	}
	if problem != nil {
		t.Errorf("extglob script should parse cleanly, got %+v", problem)
	}
}

func TestUnknownEntrypointIsReported(t *testing.T) {
	_, err := For("nodejs")
	if err == nil {
		t.Fatal("expected an error for an unknown entrypoint")
	}
	if !strings.Contains(err.Error(), "nodejs") {
		t.Errorf("error should name the entrypoint, got %v", err)
	}
}
