// Command egglint checks Pterodactyl and Pelican egg exports for problems that
// break import, installation or startup.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/hostvaultio/egglint/lint"
	"github.com/hostvaultio/egglint/report"
)

// version is overwritten at build time with the release tag.
var version = "dev"

const toolURI = "https://github.com/hostvaultio/egglint"

// Exit codes. Distinguishing findings (1) from a broken invocation (2) lets CI
// tell "the eggs have problems" apart from "the linter could not run".
const (
	exitOK       = 0
	exitFindings = 1
	exitError    = 2
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "rules":
			return cmdRules(stdout)
		case "explain":
			return cmdExplain(args[1:], stdout, stderr)
		case "version":
			fmt.Fprintf(stdout, "egglint %s\n", version)
			return exitOK
		case "lint":
			args = args[1:]
		}
	}
	return cmdLint(args, stdout, stderr)
}

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error {
	for _, part := range strings.Split(v, ",") {
		if part = strings.TrimSpace(part); part != "" {
			*s = append(*s, part)
		}
	}
	return nil
}

func cmdLint(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("egglint", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		configPath = fs.String("config", "", "path to a config file (default: .egglint.yaml if present)")
		format     = fs.String("format", "", "output format: text, json, github, sarif (default: text, or github under GitHub Actions)")
		outPath    = fs.String("output", "", "write output to a file instead of stdout")
		failOn     = fs.String("fail-on", "", "lowest severity that fails the run: error, warning, info, never (default: error)")
		verbose    = fs.Bool("verbose", false, "include fix hints in text output")
		noColor    = fs.Bool("no-color", false, "disable coloured output")
		allFiles   = fs.Bool("all-files", false, "do not skip JSON files that are not egg exports")
		disable    stringList
		enable     stringList
		exclude    stringList
	)
	fs.Var(&disable, "disable", "rule IDs or names to disable (repeatable, comma-separated)")
	fs.Var(&enable, "enable", "run only these rules (repeatable, comma-separated)")
	fs.Var(&exclude, "exclude", "glob patterns to skip, ** matches any depth (repeatable)")

	fs.Usage = func() {
		fmt.Fprintf(stderr, `egglint %s — lint Pterodactyl and Pelican eggs

Usage:
  egglint [flags] <file-or-directory>...
  egglint rules                 list every rule
  egglint explain <rule>        show why a rule exists and how to fix it
  egglint version

Directories are searched recursively for .json files; files that are not egg
exports are skipped unless --all-files is given.

Exit codes: 0 clean, 1 findings at or above --fail-on, 2 could not run.

Flags:
`, version)
		fs.PrintDefaults()
		fmt.Fprintf(stderr, "\nExamples:\n"+
			"  egglint eggs/\n"+
			"  egglint --format sarif --output egglint.sarif .\n"+
			"  egglint --disable EGG016,unused-variable eggs/\n"+
			"  egglint --fail-on warning eggs/paper.json\n")
	}

	if err := fs.Parse(args); err != nil {
		return exitError
	}

	paths := fs.Args()
	if len(paths) == 0 {
		fs.Usage()
		return exitError
	}

	cfg, err := lint.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(stderr, "egglint: %v\n", err)
		return exitError
	}

	// Command line flags layer on top of the config file.
	cfg.Disable = append(cfg.Disable, disable...)
	cfg.Exclude = append(cfg.Exclude, exclude...)
	if len(enable) > 0 {
		cfg.Enable = enable
	}
	if *allFiles {
		f := false
		cfg.SkipNonEggs = &f
	}

	failNever := false
	if *failOn != "" {
		if strings.EqualFold(*failOn, "never") {
			failNever = true
			cfg.FailOn = ""
		} else {
			cfg.FailOn = *failOn
		}
	}

	linter, err := lint.New(cfg)
	if err != nil {
		fmt.Fprintf(stderr, "egglint: %v\n", err)
		return exitError
	}

	files, err := lint.Discover(paths)
	if err != nil {
		fmt.Fprintf(stderr, "egglint: %v\n", err)
		return exitError
	}
	if len(files) == 0 {
		fmt.Fprintf(stderr, "egglint: no .json files found in %s\n", strings.Join(paths, ", "))
		return exitError
	}

	rep := linter.Run(files)

	outFormat, err := resolveFormat(*format)
	if err != nil {
		fmt.Fprintf(stderr, "egglint: %v\n", err)
		return exitError
	}

	sink := stdout
	if *outPath != "" {
		f, createErr := os.Create(*outPath)
		if createErr != nil {
			fmt.Fprintf(stderr, "egglint: %v\n", createErr)
			return exitError
		}
		defer f.Close()
		sink = f
	}

	opts := report.Options{
		Format:  outFormat,
		Color:   !*noColor && sink == stdout && colorSupported(),
		Verbose: *verbose,
		Version: version,
		ToolURI: toolURI,
	}
	if err := report.Write(sink, rep, opts); err != nil {
		fmt.Fprintf(stderr, "egglint: writing report: %v\n", err)
		return exitError
	}

	if failNever {
		return exitOK
	}
	if max, any := rep.Max(); any && max >= linter.FailOn() {
		return exitFindings
	}
	return exitOK
}

// resolveFormat picks the output format, defaulting to GitHub workflow commands
// when running inside GitHub Actions so findings appear as annotations without
// the user having to configure anything.
func resolveFormat(explicit string) (report.Format, error) {
	if explicit != "" {
		return report.ParseFormat(explicit)
	}
	if os.Getenv("GITHUB_ACTIONS") == "true" {
		return report.FormatGitHub, nil
	}
	return report.FormatText, nil
}

func colorSupported() bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func cmdRules(stdout io.Writer) int {
	fmt.Fprintf(stdout, "%-8s %-24s %-8s %s\n", "ID", "NAME", "SEVERITY", "SUMMARY")
	for _, r := range lint.Rules() {
		fmt.Fprintf(stdout, "%-8s %-24s %-8s %s\n", r.ID, r.Name, r.Severity, r.Summary)
	}
	fmt.Fprintf(stdout, "\nRun `egglint explain <id-or-name>` for the reasoning behind a rule.\n")
	return exitOK
}

func cmdExplain(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintf(stderr, "usage: egglint explain <rule-id-or-name>\n")
		return exitError
	}
	rule, ok := lint.RuleByRef(args[0])
	if !ok {
		fmt.Fprintf(stderr, "egglint: unknown rule %q (see `egglint rules`)\n", args[0])
		return exitError
	}
	fmt.Fprintf(stdout, "%s  %s\n%s\n\nSeverity: %s\n\n%s\n\n%s\n",
		rule.ID, rule.Name,
		strings.Repeat("-", len(rule.ID)+len(rule.Name)+2),
		rule.Severity,
		rule.Summary,
		strings.TrimSpace(rule.Docs),
	)
	return exitOK
}
