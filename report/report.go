// Package report renders lint results in the formats a user or a CI system
// needs: human-readable text, machine-readable JSON, GitHub Actions workflow
// commands, and SARIF for code-scanning annotations.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/hostvaultio/egglint/lint"
)

// Format identifies an output renderer.
type Format string

// Supported output formats.
const (
	FormatText   Format = "text"
	FormatJSON   Format = "json"
	FormatGitHub Format = "github"
	FormatSARIF  Format = "sarif"
)

// Formats lists every supported format name.
var Formats = []Format{FormatText, FormatJSON, FormatGitHub, FormatSARIF}

// ParseFormat validates a format name.
func ParseFormat(s string) (Format, error) {
	for _, f := range Formats {
		if string(f) == strings.ToLower(strings.TrimSpace(s)) {
			return f, nil
		}
	}
	return "", fmt.Errorf("unknown format %q (want one of: %s)", s, joinFormats())
}

func joinFormats() string {
	parts := make([]string, len(Formats))
	for i, f := range Formats {
		parts[i] = string(f)
	}
	return strings.Join(parts, ", ")
}

// Options configures rendering.
type Options struct {
	Format Format
	// Color enables ANSI colour in text output.
	Color bool
	// Verbose includes each finding's help text and the rule documentation URL.
	Verbose bool
	// Version is the egglint version recorded in SARIF output.
	Version string
	// ToolURI is the tool's home page, recorded in SARIF output.
	ToolURI string
}

// Write renders the report in the configured format.
func Write(w io.Writer, rep *lint.Report, opts Options) error {
	switch opts.Format {
	case FormatJSON:
		return writeJSON(w, rep)
	case FormatGitHub:
		return writeGitHub(w, rep)
	case FormatSARIF:
		return writeSARIF(w, rep, opts)
	default:
		return writeText(w, rep, opts)
	}
}

const (
	ansiReset  = "\033[0m"
	ansiBold   = "\033[1m"
	ansiDim    = "\033[2m"
	ansiRed    = "\033[31m"
	ansiYellow = "\033[33m"
	ansiBlue   = "\033[34m"
	ansiGreen  = "\033[32m"
)

func writeText(w io.Writer, rep *lint.Report, opts Options) error {
	color := func(code, s string) string {
		if !opts.Color {
			return s
		}
		return code + s + ansiReset
	}
	sevColor := func(s lint.Severity) string {
		switch s {
		case lint.Error:
			return ansiRed
		case lint.Warning:
			return ansiYellow
		default:
			return ansiBlue
		}
	}

	byFile := map[string][]lint.Finding{}
	for _, f := range rep.All() {
		byFile[f.Path] = append(byFile[f.Path], f)
	}
	paths := make([]string, 0, len(byFile))
	for p := range byFile {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	for _, path := range paths {
		fmt.Fprintf(w, "\n%s\n", color(ansiBold, path))
		for _, f := range byFile[path] {
			fmt.Fprintf(w, "  %s:%d  %s  %s  %s\n",
				color(ansiDim, "line"),
				f.Line,
				color(sevColor(f.Severity), pad(f.Severity.String(), 7)),
				color(ansiDim, f.RuleID),
				f.Message,
			)
			if opts.Verbose && f.Help != "" {
				fmt.Fprintf(w, "          %s %s\n", color(ansiDim, "help:"), f.Help)
			}
		}
	}

	for _, note := range rep.Notes {
		fmt.Fprintf(w, "\n%s %s\n", color(ansiYellow, "note:"), note)
	}

	errors, warnings, infos := rep.Count(lint.Error), rep.Count(lint.Warning), rep.Count(lint.Info)
	total := errors + warnings + infos
	fmt.Fprintf(w, "\n")
	if total == 0 {
		fmt.Fprintf(w, "%s %d file(s) checked, no problems found\n",
			color(ansiGreen, "ok"), rep.FilesLinted())
		return nil
	}
	fmt.Fprintf(w, "%d file(s) checked: %s, %s, %s\n",
		rep.FilesLinted(),
		color(ansiRed, fmt.Sprintf("%d error(s)", errors)),
		color(ansiYellow, fmt.Sprintf("%d warning(s)", warnings)),
		color(ansiBlue, fmt.Sprintf("%d info", infos)),
	)
	if !opts.Verbose {
		fmt.Fprintf(w, "%s\n", color(ansiDim, "run with --verbose for fix hints, or `egglint explain <rule>` for detail"))
	}
	return nil
}

func pad(s string, n int) string {
	for len(s) < n {
		s += " "
	}
	return s
}

func writeJSON(w io.Writer, rep *lint.Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(rep)
}

// writeGitHub emits workflow commands, which GitHub renders as inline
// annotations on the pull request diff.
func writeGitHub(w io.Writer, rep *lint.Report) error {
	for _, f := range rep.All() {
		level := "notice"
		switch f.Severity {
		case lint.Error:
			level = "error"
		case lint.Warning:
			level = "warning"
		}
		fmt.Fprintf(w, "::%s file=%s,line=%d,col=%d,title=%s (%s)::%s\n",
			level, f.Path, f.Line, maxInt(f.Col, 1), f.RuleID, f.RuleName,
			escapeWorkflowData(f.Message))
	}
	for _, note := range rep.Notes {
		fmt.Fprintf(w, "::notice title=egglint::%s\n", escapeWorkflowData(note))
	}
	return nil
}

// escapeWorkflowData escapes the characters that would otherwise terminate a
// GitHub workflow command early.
func escapeWorkflowData(s string) string {
	r := strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A")
	return r.Replace(s)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
