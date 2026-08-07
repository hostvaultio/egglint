package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hostvaultio/egglint/lint"
)

func sample() *lint.Report {
	return &lint.Report{
		Results: []lint.Result{{
			Path: "eggs/paper.json",
			Findings: []lint.Finding{{
				RuleID:       "EGG004",
				RuleName:     "author-email",
				Severity:     lint.Error,
				SeverityName: "error",
				Path:         "eggs/paper.json",
				Line:         8,
				Col:          5,
				Message:      "author \"Nobody\" is not a valid email address",
				Help:         "Use a plain email address.",
			}},
		}},
		Notes: []string{"busybox not found; using dash"},
	}
}

func TestSARIFIsValidAndLinksRules(t *testing.T) {
	var buf bytes.Buffer
	err := Write(&buf, sample(), Options{Format: FormatSARIF, Version: "1.2.3", ToolURI: "https://example.com"})
	if err != nil {
		t.Fatal(err)
	}

	var log struct {
		Version string `json:"version"`
		Runs    []struct {
			Tool struct {
				Driver struct {
					Name  string `json:"name"`
					Rules []struct {
						ID string `json:"id"`
					} `json:"rules"`
				} `json:"driver"`
			} `json:"tool"`
			Results []struct {
				RuleID    string `json:"ruleId"`
				RuleIndex int    `json:"ruleIndex"`
				Level     string `json:"level"`
				Locations []struct {
					PhysicalLocation struct {
						ArtifactLocation struct {
							URI string `json:"uri"`
						} `json:"artifactLocation"`
						Region struct {
							StartLine int `json:"startLine"`
						} `json:"region"`
					} `json:"physicalLocation"`
				} `json:"locations"`
			} `json:"results"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(buf.Bytes(), &log); err != nil {
		t.Fatalf("SARIF output is not valid JSON: %v", err)
	}
	if log.Version != "2.1.0" {
		t.Errorf("expected SARIF 2.1.0, got %q", log.Version)
	}
	if len(log.Runs) != 1 || len(log.Runs[0].Results) != 1 {
		t.Fatalf("expected one run with one result, got %+v", log.Runs)
	}
	res := log.Runs[0].Results[0]
	if res.Level != "error" {
		t.Errorf("expected level error, got %q", res.Level)
	}
	if res.Locations[0].PhysicalLocation.Region.StartLine != 8 {
		t.Errorf("expected startLine 8, got %d", res.Locations[0].PhysicalLocation.Region.StartLine)
	}
	// ruleIndex must actually address the rule it names, or GitHub renders the
	// wrong description against the annotation.
	rules := log.Runs[0].Tool.Driver.Rules
	if res.RuleIndex < 0 || res.RuleIndex >= len(rules) {
		t.Fatalf("ruleIndex %d out of range (%d rules)", res.RuleIndex, len(rules))
	}
	if rules[res.RuleIndex].ID != res.RuleID {
		t.Errorf("ruleIndex points at %q but ruleId is %q", rules[res.RuleIndex].ID, res.RuleID)
	}
}

func TestGitHubFormatEmitsWorkflowCommands(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, sample(), Options{Format: FormatGitHub}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.HasPrefix(out, "::error file=eggs/paper.json,line=8,col=5,title=EGG004 (author-email)::") {
		t.Errorf("unexpected workflow command: %q", out)
	}
}

func TestWorkflowDataIsEscaped(t *testing.T) {
	rep := sample()
	rep.Results[0].Findings[0].Message = "100% broken\nsecond line"
	var buf bytes.Buffer
	if err := Write(&buf, rep, Options{Format: FormatGitHub}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Count(out, "\n") != 2 { // one per finding, one per note
		t.Errorf("a newline in a message must be escaped, got:\n%s", out)
	}
	if !strings.Contains(out, "100%25 broken") {
		t.Errorf("percent sign should be escaped, got %q", out)
	}
}

func TestJSONFormatRoundTrips(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, sample(), Options{Format: FormatJSON}); err != nil {
		t.Fatal(err)
	}
	var back lint.Report
	if err := json.Unmarshal(buf.Bytes(), &back); err != nil {
		t.Fatalf("JSON output does not round-trip: %v", err)
	}
	if len(back.Results) != 1 || len(back.Results[0].Findings) != 1 {
		t.Errorf("unexpected decoded report: %+v", back)
	}
	if back.Results[0].Findings[0].SeverityName != "error" {
		t.Error("severity should survive as a readable name")
	}
}

func TestTextFormatSummarises(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, sample(), Options{Format: FormatText}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"eggs/paper.json", "EGG004", "1 error(s)", "note:"} {
		if !strings.Contains(out, want) {
			t.Errorf("text output missing %q:\n%s", want, out)
		}
	}
}

func TestTextFormatReportsCleanRun(t *testing.T) {
	var buf bytes.Buffer
	rep := &lint.Report{Results: []lint.Result{{Path: "a.json"}}}
	if err := Write(&buf, rep, Options{Format: FormatText}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "no problems found") {
		t.Errorf("expected a clean summary, got %q", buf.String())
	}
}

func TestParseFormat(t *testing.T) {
	for _, name := range []string{"text", "json", "github", "sarif", "SARIF"} {
		if _, err := ParseFormat(name); err != nil {
			t.Errorf("ParseFormat(%q): %v", name, err)
		}
	}
	if _, err := ParseFormat("xml"); err == nil {
		t.Error("expected an error for an unknown format")
	}
}
