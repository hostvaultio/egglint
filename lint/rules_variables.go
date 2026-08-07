package lint

import (
	"regexp"
	"strings"

	"github.com/hostvaultio/egglint/egg"
)

var envNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// panelProvidedEnv are variables wings injects into every container. An egg
// variable using one of these names collides with the value wings sets.
var panelProvidedEnv = map[string]string{
	"STARTUP":                   "wings sets this to the rendered startup command",
	"SERVER_MEMORY":             "wings sets this from the server's memory limit",
	"SERVER_IP":                 "wings sets this from the server's primary allocation",
	"SERVER_PORT":               "wings sets this from the server's primary allocation",
	"P_SERVER_LOCATION":         "wings sets this from the node's location",
	"P_SERVER_UUID":             "wings sets this to the server UUID",
	"P_SERVER_ALLOCATION_LIMIT": "wings sets this from the server's allocation limit",
	"TZ":                        "wings sets this from the node timezone",
}

// shellCriticalEnv would break the container environment itself.
//
// Deliberately excluded: LD_LIBRARY_PATH and LD_PRELOAD. Declaring those as egg
// variables looks alarming but is an established and working technique — the
// stock Bedrock egg uses LD_LIBRARY_PATH so the server can find its bundled
// shared objects — so flagging them would be a false positive on eggs that are
// correct.
var shellCriticalEnv = map[string]bool{
	"PATH": true, "HOME": true, "USER": true, "SHELL": true,
	"PWD": true, "IFS": true,
}

var ruleInvalidEnvName = &Rule{
	ID:         "EGG020",
	Name:       "invalid-env-name",
	Summary:    "env_variable is not a valid environment variable name",
	Severity:   Error,
	NeedsParse: true,
	Docs: `env_variable becomes a real environment variable in the container, so it must
be a valid shell identifier: letters, digits and underscores only, not starting
with a digit. A name containing a space, dash or dot cannot be exported and the
variable silently never reaches the server.`,
	Check: func(c *Context) {
		for i, v := range c.Egg().Variables {
			name := strings.TrimSpace(v.EnvVariable.String())
			if name == "" {
				c.Report(egg.Ptr("variables", i, "env_variable"),
					"variable %q has an empty env_variable", v.Name)
				continue
			}
			if !envNameRe.MatchString(name) {
				c.ReportHelp(egg.Ptr("variables", i, "env_variable"),
					"Use A-Z, 0-9 and underscore only, e.g. SERVER_JARFILE.",
					"env_variable %q is not a valid environment variable name", name)
			}
		}
	},
}

var ruleDuplicateEnv = &Rule{
	ID:         "EGG021",
	Name:       "duplicate-env",
	Summary:    "Two variables declare the same env_variable",
	Severity:   Error,
	NeedsParse: true,
	Docs: `Each env_variable must be unique within an egg. When two variables share a
name the panel rejects the import, and if one is edited later it is ambiguous
which value the server receives.`,
	Check: func(c *Context) {
		seen := map[string]int{}
		for i, v := range c.Egg().Variables {
			name := strings.TrimSpace(v.EnvVariable.String())
			if name == "" {
				continue
			}
			if first, dup := seen[name]; dup {
				c.Report(egg.Ptr("variables", i, "env_variable"),
					"env_variable %q is already declared by variable %d (%q)",
					name, first+1, c.Egg().Variables[first].Name)
				continue
			}
			seen[name] = i
		}
	},
}

var ruleReservedEnv = &Rule{
	ID:         "EGG022",
	Name:       "reserved-env",
	Summary:    "Variable shadows one the panel or shell already provides",
	Severity:   Error,
	NeedsParse: true,
	Docs: `Some environment variables are supplied by wings for every server, and
others are fundamental to the container's shell. Declaring an egg variable with
the same name means either the egg's value is ignored or the runtime value is
clobbered — both produce servers that behave differently from what the egg
describes.`,
	Check: func(c *Context) {
		for i, v := range c.Egg().Variables {
			name := strings.TrimSpace(v.EnvVariable.String())
			if reason, reserved := panelProvidedEnv[name]; reserved {
				c.ReportHelp(egg.Ptr("variables", i, "env_variable"),
					"Reference it directly in the startup command instead of redeclaring it.",
					"env_variable %q is provided by the panel (%s)", name, reason)
				continue
			}
			if shellCriticalEnv[name] {
				c.ReportHelp(egg.Ptr("variables", i, "env_variable"),
					"Pick a name specific to this egg, e.g. SERVER_"+name+".",
					"env_variable %q overwrites a core shell variable in the container", name)
			}
		}
	},
}

var ruleMissingRules = &Rule{
	ID:         "EGG023",
	Name:       "missing-rules",
	Summary:    "Variable has no validation rules",
	Severity:   Warning,
	NeedsParse: true,
	Docs: `The rules field is a Laravel validation string the panel applies whenever a
user edits the variable. With no rules, any value a user types is accepted and
substituted into the startup command verbatim. At minimum declare the type, for
example "required|string|max:20" or "nullable|boolean".`,
	Check: func(c *Context) {
		for i, v := range c.Egg().Variables {
			if strings.TrimSpace(v.Rules.String()) == "" {
				c.ReportHelp(egg.Ptr("variables", i, "rules"),
					`Start with something like "required|string|max:64".`,
					"variable %q has no validation rules; any user input is accepted as-is", v.Name)
			}
		}
	},
}

var ruleRequiredEmptyDefault = &Rule{
	ID:         "EGG024",
	Name:       "required-empty-default",
	Summary:    "Variable is required but its default value is empty",
	Severity:   Warning,
	NeedsParse: true,
	Docs: `A variable marked "required" with an empty default fails validation the
moment anyone saves the server's startup settings without filling it in, and the
egg cannot be used to create a server without manual intervention. Either give it
a usable default or mark it "nullable".`,
	Check: func(c *Context) {
		for i, v := range c.Egg().Variables {
			if !v.Rules.Has("required") {
				continue
			}
			if strings.TrimSpace(v.DefaultValue.String()) != "" {
				continue
			}
			c.ReportHelp(egg.Ptr("variables", i, "default_value"),
				`Provide a default, or change the rule to "nullable".`,
				"variable %q is required but has an empty default_value", v.Name)
		}
	},
}

var ruleUnusedVariable = &Rule{
	ID:         "EGG025",
	Name:       "unused-variable",
	Summary:    "Variable is never referenced anywhere in the egg",
	Severity:   Info,
	NeedsParse: true,
	Docs: `The variable is not referenced by the startup command, the install script or
any configuration block. It will still be shown to users, who will reasonably
expect changing it to do something. Either use it or remove it.

Note that a variable can legitimately be consumed by the game server itself
reading its own environment, so this is advisory rather than a warning.`,
	Check: func(c *Context) {
		e := c.Egg()
		haystack := strings.Join([]string{
			e.Startup,
			e.Scripts.Installation.Script,
			e.Config.Files, e.Config.Startup, e.Config.Logs, e.Config.Stop,
		}, "\n")

		for i, v := range e.Variables {
			name := strings.TrimSpace(v.EnvVariable.String())
			if name == "" || !envNameRe.MatchString(name) {
				continue
			}
			if referencesEnv(haystack, name) {
				continue
			}
			c.Report(egg.Ptr("variables", i, "env_variable"),
				"variable %q (%s) is never referenced by the startup command, install script or config", name, v.Name)
		}
	},
}

// referencesEnv reports whether text uses the variable in any of the forms an
// egg can:
//
//   - {{VAR}}                      short substitution, used in startup commands
//   - {{server.build.env.VAR}}     fully qualified form, used in config blocks
//   - $VAR / ${VAR}                ordinary shell expansion, used in scripts
//
// The qualified form matters: configuration blocks conventionally use it, so a
// checker that only understood {{VAR}} would report every config-driven variable
// as unused.
func referencesEnv(text, name string) bool {
	if strings.Contains(text, "{{"+name+"}}") ||
		strings.Contains(text, "${"+name+"}") ||
		strings.Contains(text, "env."+name+"}}") {
		return true
	}
	// Bare $VAR, ensuring VAR is not merely a prefix of a longer name.
	for idx := 0; ; {
		i := strings.Index(text[idx:], "$"+name)
		if i < 0 {
			return false
		}
		i += idx
		after := i + 1 + len(name)
		if after >= len(text) || !isEnvNameChar(text[after]) {
			return true
		}
		idx = after
	}
}

func isEnvNameChar(b byte) bool {
	return b == '_' ||
		(b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9')
}

func init() {
	register(
		ruleInvalidEnvName,
		ruleDuplicateEnv,
		ruleReservedEnv,
		ruleMissingRules,
		ruleRequiredEmptyDefault,
		ruleUnusedVariable,
	)
}
