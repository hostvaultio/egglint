# Contributing

## Building and testing

```bash
go build ./...
go test ./...
go run ./cmd/egglint testdata/eggs/problems.json
```

The syntax-check rule shells out to real interpreters. Install `bash`, `dash` and
`busybox` locally, otherwise those tests skip and you will not exercise the paths
you are changing.

## Adding a rule

A rule is a value, not an interface implementation — see `lint/rules_schema.go`
for the shape. To add one:

1. Define it in the relevant `lint/rules_*.go` file and add it to that file's
   `init()`.
2. Give it the next free ID in its band: `EGG0xx` schema, `EGG01x` install
   script, `EGG02x` variables, `EGG03x` startup.
3. Add a case to `testdata/eggs/problems.json` (or a new fixture) and assert it
   in `lint/lint_test.go`.
4. Document it in the README's rule table.

### What makes a good rule

**A real failure mode.** The best rules come from something that actually broke.
Write the failure into the `Docs` field: what goes wrong, why it is hard to
diagnose, and how to fix it. `egglint explain` prints that text, so it is part of
the interface.

**A severity justified against real eggs.** Before choosing a default, run the
rule over a large corpus — the public Pterodactyl and Pelican egg repositories
are the obvious source:

```bash
git clone --depth 1 https://github.com/pelican-eggs/eggs /tmp/eggs
go run ./cmd/egglint --format json --fail-on never /tmp/eggs > /tmp/out.json
jq -r '.results[].findings[]?.rule' /tmp/out.json | sort | uniq -c | sort -rn
```

A rule defaulting to `error` should fire on a small fraction of published eggs,
and every hit should be a defect you can confirm by reading the egg. If it fires
on most of the corpus, either it is wrong or it is advisory — check which before
choosing. This is not a formality: it is how the two calibration corrections
described in the README were found.

**Honest about what it verified.** If a check could not run — a missing
interpreter, an unparseable field — say so through a run note rather than
returning a clean result. A skipped check reported as a pass is worse than no
check.

## Style

Go standard formatting (`gofmt`), no external dependencies beyond `yaml.v3`.
Comments should explain *why* something is done, particularly where the reason is
a non-obvious property of the panel or of shell behaviour.
