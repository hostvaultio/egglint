package lint

import (
	"encoding/json"
	"net/mail"
	"regexp"
	"strings"

	"github.com/hostvaultio/egglint/egg"
)

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// ruleInvalidJSON is emitted by the engine rather than by a Check function,
// because a file that does not parse has no egg for a rule to inspect. It is
// registered so it appears in `egglint rules` and can be referenced in config.
var ruleInvalidJSON = &Rule{
	ID:       "EGG001",
	Name:     "invalid-json",
	Summary:  "File is not valid JSON",
	Severity: Error,
	Docs: `The egg could not be parsed as JSON, so the panel cannot import it at all.
Common causes are a trailing comma, an unescaped quote inside the install
script, or a file that was edited by hand rather than exported from the panel.`,
}

var ruleUnknownSchema = &Rule{
	ID:         "EGG002",
	Name:       "unknown-schema",
	Summary:    "meta.version is missing or not a known PTDL schema",
	Severity:   Error,
	NeedsParse: true,
	Docs: `Every egg export carries meta.version identifying the schema (PTDL_v1 or
PTDL_v2). The panel refuses to import a file without a version it recognises.
Re-export the egg from the panel rather than adding the field by hand.`,
	Check: func(c *Context) {
		v := c.Egg().Meta.Version
		switch v {
		case egg.SchemaV1, egg.SchemaV2:
			return
		case "":
			c.ReportHelp(egg.Ptr("meta", "version"),
				"Re-export the egg from the panel to get a correct meta block.",
				"meta.version is missing; the panel cannot import an egg without a schema version")
		default:
			c.ReportHelp(egg.Ptr("meta", "version"),
				"Known versions are PTDL_v1 and PTDL_v2.",
				"meta.version %q is not a known egg schema version", string(v))
		}
	},
}

var ruleMissingField = &Rule{
	ID:         "EGG003",
	Name:       "missing-field",
	Summary:    "A required top-level field is missing or empty",
	Severity:   Error,
	NeedsParse: true,
	Docs: `The panel requires name, author, startup and a docker image to import an
egg. An egg missing any of them fails import with a validation error that does
not always name the offending field.`,
	Check: func(c *Context) {
		e := c.Egg()
		if strings.TrimSpace(e.Name) == "" {
			c.Report(egg.Ptr("name"), "name is required and must not be empty")
		}
		if strings.TrimSpace(e.Author) == "" {
			c.Report(egg.Ptr("author"), "author is required and must not be empty")
		}
		if strings.TrimSpace(e.Startup) == "" {
			c.ReportHelp(egg.Ptr("startup"),
				"The startup command is what wings runs to boot the server.",
				"startup is required and must not be empty")
		}
	},
}

var ruleAuthorEmail = &Rule{
	ID:         "EGG004",
	Name:       "author-email",
	Summary:    "author must be a valid email address",
	Severity:   Error,
	NeedsParse: true,
	Docs: `The panel validates author as an email address on import. A name, handle or
URL in this field makes the import fail with a validation error, which is a
frequent and confusing cause of "this egg won't import" reports.`,
	Check: func(c *Context) {
		author := strings.TrimSpace(c.Egg().Author)
		if author == "" {
			return // reported by EGG003
		}
		if _, err := mail.ParseAddress(author); err != nil {
			c.ReportHelp(egg.Ptr("author"),
				"Use a plain email address, e.g. eggs@example.com",
				"author %q is not a valid email address; the panel validates this field on import", author)
		}
	},
}

var ruleInvalidUUID = &Rule{
	ID:         "EGG005",
	Name:       "invalid-uuid",
	Summary:    "uuid is present but not a valid UUID",
	Severity:   Warning,
	NeedsParse: true,
	Docs: `The uuid identifies the egg across exports and imports; the panel uses it to
decide whether an import updates an existing egg or creates a new one. A
malformed uuid causes a new egg to be created on every import.`,
	Check: func(c *Context) {
		u := strings.TrimSpace(c.Egg().UUID)
		if u == "" {
			return
		}
		if !uuidRe.MatchString(u) {
			c.Report(egg.Ptr("uuid"), "uuid %q is not a valid UUID", u)
		}
	},
}

var ruleNoDockerImages = &Rule{
	ID:         "EGG006",
	Name:       "no-docker-images",
	Summary:    "The egg declares no docker image",
	Severity:   Error,
	NeedsParse: true,
	Docs: `Without at least one docker image wings has nothing to run the server in.
PTDL_v2 uses a docker_images map of display name to image; PTDL_v1 used a single
docker_image string.`,
	Check: func(c *Context) {
		e := c.Egg()
		if len(e.Images()) > 0 {
			return
		}
		c.ReportHelp(egg.Ptr("docker_images"),
			`Add at least one entry, e.g. {"Java 21": "ghcr.io/pterodactyl/yolks:java_21"}`,
			"no docker image declared; wings has no container to run the server in")
	},
}

var ruleConfigJSON = &Rule{
	ID:         "EGG007",
	Name:       "config-json-string",
	Summary:    "config.files/startup/logs must contain valid JSON",
	Severity:   Error,
	NeedsParse: true,
	Docs: `The panel stores these three configuration blocks as JSON documents held
inside JSON strings. That double encoding is easy to get wrong when editing an
egg by hand: the file stays valid JSON overall while the embedded document is
malformed, so the problem only appears when wings tries to configure a server.

config.stop is deliberately not checked here — it is a plain command string
(for example "stop"), not a JSON document.`,
	Check: func(c *Context) {
		cfg := c.Egg().Config
		check := func(field, raw string, want string) {
			raw = strings.TrimSpace(raw)
			if raw == "" {
				return
			}
			var v any
			if err := json.Unmarshal([]byte(raw), &v); err != nil {
				c.ReportHelp(egg.Ptr("config", field),
					"This field is a JSON document stored inside a JSON string; both layers must be valid.",
					"config.%s does not contain valid JSON: %s", field, err.Error())
				return
			}
			switch want {
			case "object":
				if _, ok := v.(map[string]any); !ok {
					c.Report(egg.Ptr("config", field),
						"config.%s must be a JSON object, got %s", field, jsonKind(v))
				}
			case "object-or-array":
				switch v.(type) {
				case map[string]any, []any:
				default:
					c.Report(egg.Ptr("config", field),
						"config.%s must be a JSON object or array, got %s", field, jsonKind(v))
				}
			}
		}
		check("files", cfg.Files, "object")
		check("startup", cfg.Startup, "object")
		check("logs", cfg.Logs, "object-or-array")
	},
}

var ruleImageTag = &Rule{
	ID:         "EGG008",
	Name:       "untagged-image",
	Summary:    "Docker image has no explicit tag",
	Severity:   Warning,
	NeedsParse: true,
	Docs: `An image reference without a tag resolves to :latest, so the runtime a
server boots with changes silently whenever the upstream image is rebuilt. Pin
an explicit tag so a server that worked yesterday still works today.`,
	Check: func(c *Context) {
		for name, image := range c.Egg().Images() {
			if hasExplicitTag(image) {
				continue
			}
			c.ReportHelp(egg.Ptr("docker_images", name),
				"Pin a tag, e.g. ghcr.io/pterodactyl/yolks:java_21",
				"docker image %q has no explicit tag, so it resolves to :latest and can change without warning", image)
		}
	},
}

// hasExplicitTag reports whether an image reference carries a tag or digest.
// Only the final path segment can hold a tag, so a registry host:port earlier in
// the reference is not mistaken for one.
func hasExplicitTag(image string) bool {
	if strings.Contains(image, "@") {
		return true // digest-pinned
	}
	last := image
	if i := strings.LastIndex(image, "/"); i >= 0 {
		last = image[i+1:]
	}
	return strings.Contains(last, ":")
}

func jsonKind(v any) string {
	switch v.(type) {
	case map[string]any:
		return "an object"
	case []any:
		return "an array"
	case string:
		return "a string"
	case float64:
		return "a number"
	case bool:
		return "a boolean"
	case nil:
		return "null"
	default:
		return "an unexpected type"
	}
}

func init() {
	register(
		ruleInvalidJSON,
		ruleUnknownSchema,
		ruleMissingField,
		ruleAuthorEmail,
		ruleInvalidUUID,
		ruleNoDockerImages,
		ruleConfigJSON,
		ruleImageTag,
	)
}
