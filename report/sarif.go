package report

import (
	"encoding/json"
	"io"

	"github.com/hostvaultio/egglint/lint"
)

// SARIF 2.1.0, reduced to the subset GitHub code scanning consumes. Emitting it
// is what turns egglint findings into inline annotations on a pull request
// rather than lines buried in a job log.
type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string      `json:"name"`
	InformationURI string      `json:"informationUri,omitempty"`
	Version        string      `json:"version,omitempty"`
	Rules          []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID                   string          `json:"id"`
	Name                 string          `json:"name"`
	ShortDescription     sarifText       `json:"shortDescription"`
	FullDescription      sarifText       `json:"fullDescription"`
	Help                 sarifText       `json:"help"`
	DefaultConfiguration sarifRuleConfig `json:"defaultConfiguration"`
	Properties           sarifRuleProps  `json:"properties,omitempty"`
}

type sarifRuleProps struct {
	Tags []string `json:"tags,omitempty"`
}

type sarifRuleConfig struct {
	Level string `json:"level"`
}

type sarifText struct {
	Text string `json:"text"`
}

type sarifResult struct {
	RuleID    string          `json:"ruleId"`
	RuleIndex int             `json:"ruleIndex"`
	Level     string          `json:"level"`
	Message   sarifText       `json:"message"`
	Locations []sarifLocation `json:"locations"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysical `json:"physicalLocation"`
}

type sarifPhysical struct {
	ArtifactLocation sarifArtifact `json:"artifactLocation"`
	Region           sarifRegion   `json:"region"`
}

type sarifArtifact struct {
	URI string `json:"uri"`
}

type sarifRegion struct {
	StartLine   int `json:"startLine"`
	StartColumn int `json:"startColumn,omitempty"`
}

func sarifLevel(s lint.Severity) string {
	switch s {
	case lint.Error:
		return "error"
	case lint.Warning:
		return "warning"
	default:
		return "note"
	}
}

func writeSARIF(w io.Writer, rep *lint.Report, opts Options) error {
	rules := lint.Rules()
	index := make(map[string]int, len(rules))
	driverRules := make([]sarifRule, 0, len(rules))
	for i, r := range rules {
		index[r.ID] = i
		driverRules = append(driverRules, sarifRule{
			ID:                   r.ID,
			Name:                 r.Name,
			ShortDescription:     sarifText{Text: r.Summary},
			FullDescription:      sarifText{Text: r.Summary},
			Help:                 sarifText{Text: r.Docs},
			DefaultConfiguration: sarifRuleConfig{Level: sarifLevel(r.Severity)},
		})
	}

	results := make([]sarifResult, 0)
	for _, f := range rep.All() {
		idx, ok := index[f.RuleID]
		if !ok {
			continue
		}
		message := f.Message
		if f.Help != "" {
			message += "\n\n" + f.Help
		}
		results = append(results, sarifResult{
			RuleID:    f.RuleID,
			RuleIndex: idx,
			Level:     sarifLevel(f.Severity),
			Message:   sarifText{Text: message},
			Locations: []sarifLocation{{
				PhysicalLocation: sarifPhysical{
					ArtifactLocation: sarifArtifact{URI: f.Path},
					Region:           sarifRegion{StartLine: maxInt(f.Line, 1), StartColumn: maxInt(f.Col, 1)},
				},
			}},
		})
	}

	log := sarifLog{
		Schema:  "https://json.schemastore.org/sarif-2.1.0.json",
		Version: "2.1.0",
		Runs: []sarifRun{{
			Tool: sarifTool{Driver: sarifDriver{
				Name:           "egglint",
				InformationURI: opts.ToolURI,
				Version:        opts.Version,
				Rules:          driverRules,
			}},
			Results: results,
		}},
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(log)
}
