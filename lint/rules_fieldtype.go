package lint

import (
	"encoding/json"

	"github.com/hostvaultio/egglint/egg"
)

// expectedKinds maps top-level egg fields to the JSON type the schema requires.
// Only fields whose type is unambiguous across PTDL_v1 and PTDL_v2 appear here.
var expectedKinds = map[string]string{
	"meta":          "object",
	"exported_at":   "string",
	"name":          "string",
	"author":        "string",
	"uuid":          "string",
	"description":   "string",
	"features":      "array",
	"docker_images": "object",
	"docker_image":  "string",
	"file_denylist": "array",
	"startup":       "string",
	"config":        "object",
	"scripts":       "object",
	"variables":     "array",
}

var ruleWrongFieldType = &Rule{
	ID:         "EGG009",
	Name:       "wrong-field-type",
	Summary:    "A field has the wrong JSON type for the egg schema",
	Severity:   Error,
	NeedsParse: true,
	Docs: `The file is valid JSON, but a field holds the wrong kind of value — most
often an empty string "" where the schema wants an empty array []. The panel's
importer validates these types and rejects the egg, with an error that names the
validation rule rather than the field a human would recognise.

This is easy to introduce when editing an egg by hand, and easy to miss because
every JSON tool will happily confirm the file is well-formed.`,
	Check: func(c *Context) {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(c.File.Raw, &fields); err != nil {
			return
		}
		for name, want := range expectedKinds {
			raw, present := fields[name]
			if !present {
				continue
			}
			got := kindOf(raw)
			if got == "null" || got == want {
				continue
			}
			c.ReportHelp(egg.Ptr(name),
				fixHint(want),
				"%s must be %s, but it is %s", name, article(want), article(got))
		}
	},
}

// kindOf reports the JSON type of a raw value without fully decoding it.
func kindOf(raw json.RawMessage) string {
	for _, b := range raw {
		switch b {
		case ' ', '\t', '\n', '\r':
			continue
		case '{':
			return "object"
		case '[':
			return "array"
		case '"':
			return "string"
		case 't', 'f':
			return "boolean"
		case 'n':
			return "null"
		default:
			return "number"
		}
	}
	return "null"
}

func article(kind string) string {
	switch kind {
	case "object", "array":
		return "an " + kind
	case "null":
		return "null"
	default:
		return "a " + kind
	}
}

func fixHint(want string) string {
	switch want {
	case "array":
		return `Use [] for an empty list, not "".`
	case "object":
		return `Use {} for an empty object, not "".`
	default:
		return ""
	}
}

func init() {
	register(ruleWrongFieldType)
}
