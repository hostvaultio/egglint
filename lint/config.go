package lint

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ConfigFileNames are the names searched for when no config path is given.
var ConfigFileNames = []string{".egglint.yaml", ".egglint.yml"}

// Config controls which rules run and how severely they report.
type Config struct {
	// Disable lists rule IDs or names to switch off entirely.
	Disable []string `yaml:"disable"`
	// Enable restricts the run to these rules when non-empty.
	Enable []string `yaml:"enable"`
	// Severity overrides a rule's default severity, keyed by ID or name.
	Severity map[string]string `yaml:"severity"`
	// Exclude lists glob patterns for files to skip. "**" matches any number of
	// path segments.
	Exclude []string `yaml:"exclude"`
	// FailOn is the lowest severity that makes the run exit non-zero.
	FailOn string `yaml:"fail-on"`
	// SkipNonEggs skips JSON files that are not egg exports. Published egg
	// repositories keep game configuration files beside the eggs, so this
	// defaults to true.
	SkipNonEggs *bool `yaml:"skip-non-eggs"`
}

// LoadConfig reads a config file. A missing file at an explicitly requested path
// is an error; a missing default file is not.
func LoadConfig(path string) (Config, error) {
	var cfg Config
	if path == "" {
		for _, name := range ConfigFileNames {
			if _, err := os.Stat(name); err == nil {
				path = name
				break
			}
		}
		if path == "" {
			return cfg, nil
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("reading config: %w", err)
	}
	dec := yaml.NewDecoder(strings.NewReader(string(raw)))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil && err.Error() != "EOF" {
		return cfg, fmt.Errorf("parsing %s: %w", path, err)
	}
	return cfg, nil
}

// resolved is a validated Config with rule references turned into rule pointers.
type resolved struct {
	active      map[string]*Rule
	severity    map[string]Severity
	exclude     []string
	failOn      Severity
	skipNonEggs bool
}

func (c Config) resolve() (*resolved, error) {
	r := &resolved{
		active:      map[string]*Rule{},
		severity:    map[string]Severity{},
		exclude:     c.Exclude,
		failOn:      Error,
		skipNonEggs: true,
	}
	if c.SkipNonEggs != nil {
		r.skipNonEggs = *c.SkipNonEggs
	}
	if c.FailOn != "" {
		s, err := ParseSeverity(c.FailOn)
		if err != nil {
			return nil, fmt.Errorf("fail-on: %w", err)
		}
		r.failOn = s
	}

	if len(c.Enable) > 0 {
		for _, ref := range c.Enable {
			rule, ok := RuleByRef(ref)
			if !ok {
				return nil, fmt.Errorf("enable: unknown rule %q", ref)
			}
			r.active[rule.ID] = rule
		}
	} else {
		for _, rule := range Rules() {
			r.active[rule.ID] = rule
		}
	}

	for _, ref := range c.Disable {
		rule, ok := RuleByRef(ref)
		if !ok {
			return nil, fmt.Errorf("disable: unknown rule %q", ref)
		}
		delete(r.active, rule.ID)
	}

	for ref, sev := range c.Severity {
		rule, ok := RuleByRef(ref)
		if !ok {
			return nil, fmt.Errorf("severity: unknown rule %q", ref)
		}
		s, err := ParseSeverity(sev)
		if err != nil {
			return nil, fmt.Errorf("severity for %s: %w", ref, err)
		}
		r.severity[rule.ID] = s
	}

	return r, nil
}

func (r *resolved) severityFor(rule *Rule) Severity {
	if s, ok := r.severity[rule.ID]; ok {
		return s
	}
	return rule.Severity
}

func (r *resolved) excluded(path string) bool {
	clean := filepath.ToSlash(path)
	for _, pattern := range r.exclude {
		if matchGlob(filepath.ToSlash(pattern), clean) {
			return true
		}
	}
	return false
}

// matchGlob matches a path against a glob supporting "*", "?" and "**", where
// "**" spans any number of path segments. filepath.Match alone cannot express
// the recursive form that config files are expected to use.
func matchGlob(pattern, path string) bool {
	return matchSegments(strings.Split(pattern, "/"), strings.Split(path, "/"))
}

func matchSegments(pattern, path []string) bool {
	for len(pattern) > 0 {
		if pattern[0] == "**" {
			// Trailing "**" matches everything that remains.
			if len(pattern) == 1 {
				return true
			}
			for i := 0; i <= len(path); i++ {
				if matchSegments(pattern[1:], path[i:]) {
					return true
				}
			}
			return false
		}
		if len(path) == 0 {
			return false
		}
		ok, err := filepath.Match(pattern[0], path[0])
		if err != nil || !ok {
			return false
		}
		pattern, path = pattern[1:], path[1:]
	}
	return len(path) == 0
}
