package lint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func lintFixture(t *testing.T, name string, cfg Config) Result {
	t.Helper()
	l, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	path := filepath.Join("..", "testdata", "eggs", name)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	return l.LintFile(path)
}

// ruleIDs returns the set of rule IDs present in a result.
func ruleIDs(res Result) map[string]int {
	out := map[string]int{}
	for _, f := range res.Findings {
		out[f.RuleID]++
	}
	return out
}

func TestCleanEggHasNoFindings(t *testing.T) {
	res := lintFixture(t, "clean.json", Config{})
	if len(res.Findings) != 0 {
		for _, f := range res.Findings {
			t.Errorf("unexpected finding %s: %s", f.RuleID, f.Message)
		}
	}
}

func TestProblemEggTriggersExpectedRules(t *testing.T) {
	res := lintFixture(t, "problems.json", Config{})
	got := ruleIDs(res)

	expect := []string{
		"EGG004", // author is not an email
		"EGG005", // uuid is malformed
		"EGG007", // config.files is not valid JSON
		"EGG008", // image has no tag
		"EGG009", // file_denylist is a string, not an array
		"EGG014", // curl -o without --fail
		"EGG015", // downloads but never asserts
		"EGG016", // no errexit
		"EGG020", // env_variable "not-valid"
		"EGG021", // duplicate DUPE
		"EGG022", // shadows SERVER_MEMORY
		"EGG023", // variable with no rules
		"EGG024", // required with empty default
		"EGG030", // startup references {{CONFIG_FILE}}
	}
	for _, id := range expect {
		if got[id] == 0 {
			t.Errorf("expected rule %s to fire, it did not", id)
		}
	}
	if got["EGG021"] != 1 {
		t.Errorf("duplicate env should be reported once, got %d", got["EGG021"])
	}
}

// A malformed field must not stop the remaining rules from running: the egg with
// a problem is precisely the one that most needs the other checks.
func TestWrongFieldTypeDoesNotAbortOtherRules(t *testing.T) {
	res := lintFixture(t, "problems.json", Config{})
	got := ruleIDs(res)
	if got["EGG009"] == 0 {
		t.Fatal("expected EGG009 for the string file_denylist")
	}
	if len(got) < 5 {
		t.Errorf("only %d rules fired; a type error appears to have aborted the run", len(got))
	}
}

func TestNonEggIsSkipped(t *testing.T) {
	res := lintFixture(t, "not-an-egg.json", Config{})
	if !res.Skipped {
		t.Fatalf("expected the file to be skipped, got %d findings", len(res.Findings))
	}
}

func TestAllFilesLintsNonEgg(t *testing.T) {
	no := false
	res := lintFixture(t, "not-an-egg.json", Config{SkipNonEggs: &no})
	if res.Skipped {
		t.Fatal("expected the file to be linted when skip-non-eggs is off")
	}
}

func TestDisableSilencesRule(t *testing.T) {
	res := lintFixture(t, "problems.json", Config{Disable: []string{"EGG004"}})
	if ruleIDs(res)["EGG004"] != 0 {
		t.Error("EGG004 should have been disabled")
	}
}

func TestDisableAcceptsRuleName(t *testing.T) {
	res := lintFixture(t, "problems.json", Config{Disable: []string{"author-email"}})
	if ruleIDs(res)["EGG004"] != 0 {
		t.Error("disabling by rule name should work")
	}
}

func TestEnableRestrictsToListedRules(t *testing.T) {
	res := lintFixture(t, "problems.json", Config{Enable: []string{"EGG004"}})
	for _, f := range res.Findings {
		if f.RuleID != "EGG004" {
			t.Errorf("expected only EGG004, got %s", f.RuleID)
		}
	}
}

func TestSeverityOverride(t *testing.T) {
	res := lintFixture(t, "problems.json", Config{
		Severity: map[string]string{"EGG016": "error"},
	})
	for _, f := range res.Findings {
		if f.RuleID == "EGG016" && f.Severity != Error {
			t.Errorf("EGG016 severity override not applied, got %s", f.Severity)
		}
	}
}

func TestUnknownRuleReferenceIsAnError(t *testing.T) {
	if _, err := New(Config{Disable: []string{"EGG999"}}); err == nil {
		t.Error("expected an error for an unknown rule id")
	}
}

func TestFindingsCarryUsefulLineNumbers(t *testing.T) {
	res := lintFixture(t, "problems.json", Config{Enable: []string{"EGG004"}})
	if len(res.Findings) != 1 {
		t.Fatalf("expected one finding, got %d", len(res.Findings))
	}
	// "author" is on line 8 of the fixture.
	if res.Findings[0].Line != 8 {
		t.Errorf("expected the author finding on line 8, got %d", res.Findings[0].Line)
	}
}

func TestVariableFindingPointsAtTheRightVariable(t *testing.T) {
	res := lintFixture(t, "problems.json", Config{Enable: []string{"EGG022"}})
	if len(res.Findings) != 1 {
		t.Fatalf("expected one finding, got %d", len(res.Findings))
	}
	f := res.Findings[0]
	if !strings.Contains(f.Pointer, "variables/3") {
		t.Errorf("expected the finding to point at variables/3, got %q", f.Pointer)
	}
	if f.Line < 60 {
		t.Errorf("expected a line inside the variables array, got %d", f.Line)
	}
}

func TestMatchGlob(t *testing.T) {
	cases := []struct {
		pattern, path string
		want          bool
	}{
		{"**/world_files/**", "eggs/game/world_files/a/b.json", true},
		{"**/*.json", "a/b/c.json", true},
		{"*.json", "a/b/c.json", false},
		{"eggs/*.json", "eggs/paper.json", true},
		{"eggs/*.json", "eggs/nested/paper.json", false},
		{"**", "anything/at/all.json", true},
		{"eggs/**", "eggs/a/b/c.json", true},
		{"eggs/**", "other/a.json", false},
	}
	for _, tc := range cases {
		if got := matchGlob(tc.pattern, tc.path); got != tc.want {
			t.Errorf("matchGlob(%q, %q) = %v, want %v", tc.pattern, tc.path, got, tc.want)
		}
	}
}

func TestExcludeSkipsFile(t *testing.T) {
	res := lintFixture(t, "problems.json", Config{Exclude: []string{"**/problems.json"}})
	if !res.Skipped {
		t.Error("expected the excluded file to be skipped")
	}
}

func TestReportSeverityAccounting(t *testing.T) {
	l, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	rep := l.Run([]string{
		filepath.Join("..", "testdata", "eggs", "problems.json"),
		filepath.Join("..", "testdata", "eggs", "clean.json"),
	})
	if rep.FilesLinted() != 2 {
		t.Errorf("expected 2 files linted, got %d", rep.FilesLinted())
	}
	max, any := rep.Max()
	if !any || max != Error {
		t.Errorf("expected a maximum severity of error, got %v (any=%v)", max, any)
	}
	if rep.Count(Error) == 0 {
		t.Error("expected at least one error")
	}
}

func TestEveryRuleHasDocumentation(t *testing.T) {
	for _, r := range Rules() {
		if strings.TrimSpace(r.Docs) == "" {
			t.Errorf("rule %s (%s) has no documentation", r.ID, r.Name)
		}
		if strings.TrimSpace(r.Summary) == "" {
			t.Errorf("rule %s has no summary", r.ID)
		}
		if r.Name == "" || strings.ToUpper(r.Name) == r.Name {
			t.Errorf("rule %s should have a kebab-case name, got %q", r.ID, r.Name)
		}
	}
}

func TestRuleByRefIsCaseInsensitive(t *testing.T) {
	for _, ref := range []string{"EGG004", "egg004", "author-email", "AUTHOR-EMAIL"} {
		if _, ok := RuleByRef(ref); !ok {
			t.Errorf("RuleByRef(%q) did not resolve", ref)
		}
	}
}
