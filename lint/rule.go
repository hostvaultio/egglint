package lint

import (
	"fmt"
	"sort"
	"strings"

	"github.com/hostvaultio/egglint/egg"
	"github.com/hostvaultio/egglint/shell"
)

// Rule is a single check. Rules are plain values rather than interface
// implementations so the registry stays declarative and a rule can be defined in
// a handful of lines.
type Rule struct {
	// ID is the stable identifier used in config and output, e.g. "EGG012".
	ID string
	// Name is a short kebab-case slug, e.g. "script-syntax".
	Name string
	// Summary is a one-line description shown by `egglint rules`.
	Summary string
	// Severity is the default severity, overridable via config.
	Severity Severity
	// Docs explains why the rule exists and how to fix a violation.
	Docs string
	// Check runs the rule. It must tolerate a nil Context.Egg only when
	// NeedsParse is false.
	Check func(c *Context)
	// NeedsParse skips the rule when the file failed to parse.
	NeedsParse bool
}

// Context is passed to each rule and collects its findings.
type Context struct {
	// File is the parsed egg. Egg is nil when parsing failed.
	File *egg.File
	// Script is the install script with its declared entrypoint resolved.
	Script string
	// Shell is the syntax checker for the egg's entrypoint, nil when the file
	// declares no usable install script.
	Shell *shell.Checker

	rule     *Rule
	severity Severity
	findings []Finding
}

// Egg returns the parsed egg, or nil.
func (c *Context) Egg() *egg.Egg {
	if c.File == nil {
		return nil
	}
	return c.File.Egg
}

// Report attaches a finding to the JSON pointer path.
func (c *Context) Report(pointer, format string, args ...any) {
	c.report(pointer, "", format, args...)
}

// ReportHelp attaches a finding with an actionable hint.
func (c *Context) ReportHelp(pointer, help, format string, args ...any) {
	c.report(pointer, help, format, args...)
}

// ReportScript attaches a finding that occurs at a line *within* the install
// script. Because the script is stored as a single JSON string, every one of its
// lines collapses onto one line of the egg file; the finding is therefore
// anchored at the script field and names the script-relative line in its
// message, which is the most precise location the format allows.
func (c *Context) ReportScript(scriptLine int, help, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if scriptLine > 0 {
		msg = fmt.Sprintf("install script line %d: %s", scriptLine, msg)
	}
	c.report(egg.Ptr("scripts", "installation", "script"), help, "%s", msg)
}

// ReportLine attaches a finding to an explicit line, used where the position
// cannot be derived from a JSON pointer.
func (c *Context) ReportLine(line int, format string, args ...any) {
	c.findings = append(c.findings, Finding{
		RuleID:       c.rule.ID,
		RuleName:     c.rule.Name,
		Severity:     c.severity,
		SeverityName: c.severity.String(),
		Path:         c.File.Path,
		Line:         line,
		Col:          1,
		Message:      fmt.Sprintf(format, args...),
	})
}

func (c *Context) report(pointer, help, format string, args ...any) {
	pos := egg.Position{Line: 1, Col: 1}
	if c.File != nil && c.File.Index != nil {
		pos = c.File.Index.Pos(pointer)
	}
	path := ""
	if c.File != nil {
		path = c.File.Path
	}
	c.findings = append(c.findings, Finding{
		RuleID:       c.rule.ID,
		RuleName:     c.rule.Name,
		Severity:     c.severity,
		SeverityName: c.severity.String(),
		Path:         path,
		Line:         pos.Line,
		Col:          pos.Col,
		Pointer:      pointer,
		Message:      fmt.Sprintf(format, args...),
		Help:         help,
	})
}

// registry holds every rule known to the linter.
var registry = map[string]*Rule{}

func register(rules ...*Rule) {
	for _, r := range rules {
		if _, dup := registry[r.ID]; dup {
			panic("egglint: duplicate rule id " + r.ID)
		}
		registry[r.ID] = r
	}
}

// Rules returns every registered rule sorted by ID.
func Rules() []*Rule {
	out := make([]*Rule, 0, len(registry))
	for _, r := range registry {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// RuleByRef looks a rule up by ID or by name, so config files can use whichever
// is more readable.
func RuleByRef(ref string) (*Rule, bool) {
	ref = strings.TrimSpace(ref)
	if r, ok := registry[strings.ToUpper(ref)]; ok {
		return r, true
	}
	for _, r := range registry {
		if strings.EqualFold(r.Name, ref) {
			return r, true
		}
	}
	return nil, false
}
