package lint

import "testing"

func TestScriptLinesJoinsContinuationsAndDropsComments(t *testing.T) {
	script := "# a comment\ncurl \\\n  -fsSL \\\n  https://example.com\necho done\n"
	lines := scriptLines(script)
	if len(lines) != 2 {
		t.Fatalf("expected 2 logical lines, got %d: %#v", len(lines), lines)
	}
	if lines[0].Line != 2 {
		t.Errorf("continuation should report its first line (2), got %d", lines[0].Line)
	}
	if lines[1].Text != "echo done" {
		t.Errorf("unexpected second line %q", lines[1].Text)
	}
}

// A '#' inside a URL fragment must not be treated as the start of a comment.
func TestScriptLinesKeepsHashInsideURLs(t *testing.T) {
	lines := scriptLines("curl -fsSL https://example.com/a#fragment -o out\n")
	if len(lines) != 1 {
		t.Fatalf("expected the line to survive, got %d lines", len(lines))
	}
}

func TestFindCurlInvocations(t *testing.T) {
	cases := []struct {
		name       string
		script     string
		wantFail   bool
		wantFile   bool
		wantToPipe bool
	}{
		{"combined short flags", "curl -fsSLo out.jar https://e.com\n", true, true, false},
		{"long fail flag", "curl --fail --output out.jar https://e.com\n", true, true, false},
		{"missing fail flag", "curl -o out.jar https://e.com\n", false, true, false},
		{"discarded output", "curl -s -o /dev/null https://e.com\n", false, false, false},
		{"piped to shell", "curl -sSL https://e.com/i.sh | bash\n", false, false, true},
		{"absolute path", "/usr/bin/curl -f -o out https://e.com\n", true, true, false},
		{"redirect to file", "curl -sSL https://e.com > out.jar\n", false, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := findCurlInvocations(scriptLines(tc.script))
			if len(got) != 1 {
				t.Fatalf("expected 1 curl invocation, got %d", len(got))
			}
			if got[0].HasFailFlag != tc.wantFail {
				t.Errorf("HasFailFlag = %v, want %v", got[0].HasFailFlag, tc.wantFail)
			}
			if got[0].WritesFile != tc.wantFile {
				t.Errorf("WritesFile = %v, want %v", got[0].WritesFile, tc.wantFile)
			}
			if got[0].PipesToShell != tc.wantToPipe {
				t.Errorf("PipesToShell = %v, want %v", got[0].PipesToShell, tc.wantToPipe)
			}
		})
	}
}

func TestHasArtifactAssertion(t *testing.T) {
	cases := []struct {
		script string
		want   bool
	}{
		{"if [ -s server.jar ]; then echo ok; fi\n", true},
		{"if [[ ! -s server.jar ]]; then exit 1; fi\n", true},
		{"test -f server.jar || exit 1\n", true},
		{"[ -s \"${JAR}\" ] || exit 1\n", true},
		{"echo no assertions here\n", false},
		{"if [ -z \"$VAR\" ]; then exit 1; fi\n", false},
	}
	for _, tc := range cases {
		if got := hasArtifactAssertion(scriptLines(tc.script)); got != tc.want {
			t.Errorf("hasArtifactAssertion(%q) = %v, want %v", tc.script, got, tc.want)
		}
	}
}

func TestHasErrExit(t *testing.T) {
	cases := []struct {
		script string
		want   bool
	}{
		{"set -e\n", true},
		{"set -euo pipefail\n", true},
		{"set -o errexit\n", true},
		{"set -u\n", false},
		{"echo set -e is only mentioned\n", false},
	}
	for _, tc := range cases {
		if got := hasErrExit(scriptLines(tc.script)); got != tc.want {
			t.Errorf("hasErrExit(%q) = %v, want %v", tc.script, got, tc.want)
		}
	}
}

func TestHasDownload(t *testing.T) {
	if !hasDownload(scriptLines("wget https://example.com/f\n")) {
		t.Error("wget should count as a download")
	}
	if hasDownload(scriptLines("echo hello\nmkdir -p /mnt/server\n")) {
		t.Error("no download should be detected")
	}
}

func TestReferencesEnv(t *testing.T) {
	cases := []struct {
		text, name string
		want       bool
	}{
		{"java -jar {{SERVER_JARFILE}}", "SERVER_JARFILE", true},
		{"{{server.build.env.SERVERNAME}}", "SERVERNAME", true},
		{"echo ${MY_VAR}", "MY_VAR", true},
		{"echo $MY_VAR done", "MY_VAR", true},
		{"echo $MY_VAR_LONGER", "MY_VAR", false},
		{"nothing here", "MY_VAR", false},
	}
	for _, tc := range cases {
		if got := referencesEnv(tc.text, tc.name); got != tc.want {
			t.Errorf("referencesEnv(%q, %q) = %v, want %v", tc.text, tc.name, got, tc.want)
		}
	}
}

func TestHasExplicitTag(t *testing.T) {
	cases := []struct {
		image string
		want  bool
	}{
		{"ghcr.io/pterodactyl/yolks:java_21", true},
		{"ghcr.io/pterodactyl/yolks", false},
		{"alpine:3.20", true},
		{"alpine", false},
		{"localhost:5000/my/image", false},
		{"localhost:5000/my/image:v1", true},
		{"repo@sha256:abc123", true},
	}
	for _, tc := range cases {
		if got := hasExplicitTag(tc.image); got != tc.want {
			t.Errorf("hasExplicitTag(%q) = %v, want %v", tc.image, got, tc.want)
		}
	}
}
