package lint

import (
	"fmt"
	"sort"
	"strings"
)

// Severity ranks a finding.
type Severity int

const (
	// Info is advisory; it never fails a run by default.
	Info Severity = iota
	// Warning marks something very likely wrong but not provably fatal.
	Warning
	// Error marks something that breaks import or installation.
	Error
)

// ParseSeverity converts a severity name to its value.
func ParseSeverity(s string) (Severity, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "info", "note":
		return Info, nil
	case "warn", "warning":
		return Warning, nil
	case "error", "err":
		return Error, nil
	default:
		return Info, fmt.Errorf("unknown severity %q (want info, warning or error)", s)
	}
}

// String implements fmt.Stringer.
func (s Severity) String() string {
	switch s {
	case Error:
		return "error"
	case Warning:
		return "warning"
	default:
		return "info"
	}
}

// Finding is a single reported problem.
type Finding struct {
	RuleID   string   `json:"rule"`
	RuleName string   `json:"rule_name"`
	Severity Severity `json:"-"`
	// SeverityName mirrors Severity for JSON consumers.
	SeverityName string `json:"severity"`
	Path         string `json:"file"`
	Line         int    `json:"line"`
	Col          int    `json:"column,omitempty"`
	// Pointer is the JSON pointer the finding refers to, when applicable.
	Pointer string `json:"pointer,omitempty"`
	Message string `json:"message"`
	// Help is an optional actionable hint shown in verbose output.
	Help string `json:"help,omitempty"`
}

// Result is the outcome of linting one file.
type Result struct {
	Path     string    `json:"file"`
	Skipped  bool      `json:"skipped,omitempty"`
	SkipNote string    `json:"skip_reason,omitempty"`
	Findings []Finding `json:"findings"`
}

// Report aggregates results across every linted file.
type Report struct {
	Results []Result `json:"results"`
	// Notes carries run-level messages such as an unavailable shell interpreter,
	// which callers must see so a skipped check is never mistaken for a pass.
	Notes []string `json:"notes,omitempty"`
}

// All returns every finding across all files, sorted by file then line.
func (r *Report) All() []Finding {
	var out []Finding
	for _, res := range r.Results {
		out = append(out, res.Findings...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		if out[i].Line != out[j].Line {
			return out[i].Line < out[j].Line
		}
		return out[i].RuleID < out[j].RuleID
	})
	return out
}

// Count returns how many findings are at exactly the given severity.
func (r *Report) Count(s Severity) int {
	n := 0
	for _, f := range r.All() {
		if f.Severity == s {
			n++
		}
	}
	return n
}

// Max returns the highest severity present, and false when there are none.
func (r *Report) Max() (Severity, bool) {
	found := false
	max := Info
	for _, f := range r.All() {
		if !found || f.Severity > max {
			max, found = f.Severity, true
		}
	}
	return max, found
}

// FilesLinted returns the number of files that were actually checked.
func (r *Report) FilesLinted() int {
	n := 0
	for _, res := range r.Results {
		if !res.Skipped {
			n++
		}
	}
	return n
}
