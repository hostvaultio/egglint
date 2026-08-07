package lint

import (
	"regexp"
	"sort"
	"strings"

	"github.com/hostvaultio/egglint/egg"
)

// placeholderRe matches the panel's {{VARIABLE}} substitution syntax.
var placeholderRe = regexp.MustCompile(`\{\{\s*([^}]+?)\s*\}\}`)

var ruleUndefinedStartupVar = &Rule{
	ID:         "EGG030",
	Name:       "undefined-startup-var",
	Summary:    "Startup command references an undefined variable",
	Severity:   Error,
	NeedsParse: true,
	Docs: `The panel substitutes {{VARIABLE}} in the startup command from the egg's own
variables plus the handful wings provides. A placeholder that matches neither is
left in the command verbatim, so the server is launched with a literal
"{{SERVER_JARFILE}}" as an argument and fails to start.

This is easy to introduce by renaming a variable and missing one of its uses.`,
	Check: func(c *Context) {
		e := c.Egg()
		if strings.TrimSpace(e.Startup) == "" {
			return
		}

		defined := map[string]bool{}
		for _, v := range e.Variables {
			if name := strings.TrimSpace(v.EnvVariable.String()); name != "" {
				defined[name] = true
			}
		}

		var missing []string
		for _, m := range placeholderRe.FindAllStringSubmatch(e.Startup, -1) {
			ref := strings.TrimSpace(m[1])
			if ref == "" || defined[ref] {
				continue
			}
			if _, provided := panelProvidedEnv[ref]; provided {
				continue
			}
			// Legacy dotted references such as server.build.default.port are
			// resolved by the panel from server state, not from egg variables.
			if strings.Contains(ref, ".") {
				continue
			}
			missing = append(missing, ref)
		}
		if len(missing) == 0 {
			return
		}

		sort.Strings(missing)
		for _, ref := range dedupe(missing) {
			c.ReportHelp(egg.Ptr("startup"),
				"Add a matching variable, or correct the placeholder to an existing env_variable.",
				"startup references {{%s}}, which is not an egg variable and is not provided by the panel", ref)
		}
	},
}

func dedupe(in []string) []string {
	out := in[:0:0]
	seen := map[string]bool{}
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func init() {
	register(ruleUndefinedStartupVar)
}
