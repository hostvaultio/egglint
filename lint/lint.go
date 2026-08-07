// Package lint checks Pterodactyl and Pelican egg exports for problems that
// break import, installation or startup.
//
// The engine is deliberately separate from the CLI so the same checks can be
// embedded: a panel, a CI service or an egg registry can import this package and
// get identical results to the command line tool.
package lint

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hostvaultio/egglint/egg"
	"github.com/hostvaultio/egglint/shell"
)

// Linter runs a resolved rule set over egg files.
type Linter struct {
	cfg      *resolved
	checkers map[string]*shell.Checker
	notes    []string
	seenNote map[string]bool
}

// New builds a Linter from a Config.
func New(cfg Config) (*Linter, error) {
	r, err := cfg.resolve()
	if err != nil {
		return nil, err
	}
	return &Linter{
		cfg:      r,
		checkers: map[string]*shell.Checker{},
		seenNote: map[string]bool{},
	}, nil
}

// FailOn returns the severity at or above which the run should be treated as a
// failure.
func (l *Linter) FailOn() Severity { return l.cfg.failOn }

// ActiveRules returns the rules this Linter will run, sorted by ID.
func (l *Linter) ActiveRules() []*Rule {
	out := make([]*Rule, 0, len(l.cfg.active))
	for _, r := range l.cfg.active {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Discover expands the given paths into a sorted list of candidate JSON files.
// Directories are walked recursively; explicit file arguments are taken as-is
// regardless of extension.
func Discover(paths []string) ([]string, error) {
	var files []string
	seen := map[string]bool{}

	add := func(p string) {
		clean := filepath.Clean(p)
		if !seen[clean] {
			seen[clean] = true
			files = append(files, clean)
		}
	}

	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			add(p)
			continue
		}
		err = filepath.WalkDir(p, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				// Skip directories that never contain eggs but often contain a
				// great many JSON files.
				switch d.Name() {
				case ".git", "node_modules", "vendor":
					return fs.SkipDir
				}
				return nil
			}
			if strings.EqualFold(filepath.Ext(d.Name()), ".json") {
				add(path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(files)
	return files, nil
}

// Run lints every given file and returns the aggregated report.
func (l *Linter) Run(files []string) *Report {
	report := &Report{}
	for _, path := range files {
		report.Results = append(report.Results, l.LintFile(path))
	}
	report.Notes = append(report.Notes, l.notes...)
	return report
}

// LintFile reads and lints a single file.
func (l *Linter) LintFile(path string) Result {
	if l.cfg.excluded(path) {
		return Result{Path: path, Skipped: true, SkipNote: "excluded by config"}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Result{Path: path, Skipped: true, SkipNote: fmt.Sprintf("unreadable: %v", err)}
	}
	return l.LintBytes(path, raw)
}

// LintBytes lints egg JSON already held in memory.
func (l *Linter) LintBytes(path string, raw []byte) Result {
	res := Result{Path: path}

	file, parseErr := egg.Parse(path, raw)
	if parseErr != nil {
		// A file that is not JSON at all is only worth reporting if it was meant
		// to be an egg; skipping keeps unrelated JSON out of the results.
		if l.cfg.skipNonEggs && !looksIntentionallyEgg(path, raw) {
			return Result{Path: path, Skipped: true, SkipNote: "not an egg export"}
		}
		if rule, active := l.cfg.active[ruleInvalidJSON.ID]; active {
			line := 1
			var se *egg.SyntaxError
			if errors.As(parseErr, &se) && se.Line > 0 {
				line = se.Line
			}
			sev := l.cfg.severityFor(rule)
			res.Findings = append(res.Findings, Finding{
				RuleID:       rule.ID,
				RuleName:     rule.Name,
				Severity:     sev,
				SeverityName: sev.String(),
				Path:         path,
				Line:         line,
				Col:          1,
				Message:      parseErr.Error(),
			})
		}
		return res
	}

	if l.cfg.skipNonEggs && !egg.LooksLikeEgg(raw) {
		return Result{Path: path, Skipped: true, SkipNote: "not an egg export"}
	}

	script := file.Egg.Scripts.Installation.Script
	ctx := &Context{File: file, Script: script}
	if strings.TrimSpace(script) != "" {
		ctx.Shell = l.checkerFor(file.Egg.Entrypoint())
	}

	for _, rule := range l.ActiveRules() {
		if rule.Check == nil {
			continue
		}
		if rule.NeedsParse && file.Egg == nil {
			continue
		}
		ctx.rule = rule
		ctx.severity = l.cfg.severityFor(rule)
		ctx.findings = nil
		rule.Check(ctx)
		res.Findings = append(res.Findings, ctx.findings...)
	}

	sort.SliceStable(res.Findings, func(i, j int) bool {
		if res.Findings[i].Line != res.Findings[j].Line {
			return res.Findings[i].Line < res.Findings[j].Line
		}
		return res.Findings[i].RuleID < res.Findings[j].RuleID
	})
	return res
}

// checkerFor resolves and caches a shell checker per entrypoint, recording a
// run-level note the first time an interpreter turns out to be approximate or
// missing. A silently skipped check would otherwise be indistinguishable from a
// clean pass.
func (l *Linter) checkerFor(entrypoint string) *shell.Checker {
	if c, ok := l.checkers[entrypoint]; ok {
		return c
	}
	c, err := shell.For(entrypoint)
	if err != nil && !errors.Is(err, shell.ErrUnknownEntrypoint) {
		l.note(err.Error())
	}
	if c.Note != "" {
		l.note(c.Note)
	}
	l.checkers[entrypoint] = c
	return c
}

func (l *Linter) note(msg string) {
	if l.seenNote[msg] {
		return
	}
	l.seenNote[msg] = true
	l.notes = append(l.notes, msg)
}

// looksIntentionallyEgg decides whether an unparseable file was meant to be an
// egg, using the only evidence available when the JSON cannot be read: its
// filename, its location, and whether egg-specific keys appear in the text.
func looksIntentionallyEgg(path string, raw []byte) bool {
	name := strings.ToLower(filepath.Base(path))
	if strings.HasPrefix(name, "egg-") || strings.Contains(name, "egg") {
		return true
	}
	for _, marker := range []string{`"PTDL_v`, `"docker_images"`, `"scripts"`} {
		if strings.Contains(string(raw), marker) {
			return true
		}
	}
	return false
}
