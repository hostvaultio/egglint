# egglint

A linter for [Pterodactyl](https://pterodactyl.io) and [Pelican](https://pelican.dev) **eggs**.

Eggs are JSON documents containing a shell script, a startup command and a set of
variables that reference each other. Nothing checks that any of it holds together
until a server is created and the install fails — at which point the error
usually points nowhere near the cause. `egglint` checks it at review time
instead.

It is a single static binary with no runtime dependencies, a Go library, and a
GitHub Action that annotates pull requests inline.

```console
$ egglint eggs/

eggs/paper.json
  line:29  warning  EGG014  install script line 60: curl without --fail writes to a file; an HTTP error page is saved as if it were the download
  line:29  warning  EGG015  the install script downloads files but never asserts that one exists and is non-empty, so a failed download still reports a successful install

eggs/velocity.json
  line:12  error    EGG004  author "Nobody" is not a valid email address; the panel validates this field on import

2 file(s) checked: 1 error(s), 2 warning(s), 0 info
```

## Why

The failure this was built for: an install script downloads `server.jar`, the URL
404s, `curl` writes the error page to disk and exits `0`, the panel records the
installation as successful, and the server fails to boot with a message that says
nothing about a download. Every individual piece behaved as documented.

`egglint` catches that class of problem — and a dozen others — before the egg
ships.

## Install

```bash
# Go 1.22+
go install github.com/hostvaultio/egglint/cmd/egglint@latest
```

Or download a binary from the [releases page](https://github.com/hostvaultio/egglint/releases).

The shell syntax check shells out to the interpreter an egg declares
(`bash`, `ash`, `sh`). Install the ones your eggs use; `egglint` reports when it
had to substitute a stand-in, and never silently skips a check.

## Usage

```bash
egglint eggs/                       # lint a directory, recursively
egglint eggs/paper.json             # lint one file
egglint --fail-on warning eggs/     # stricter CI gate
egglint --disable EGG016 eggs/      # switch a rule off
egglint rules                       # list every rule
egglint explain EGG015              # why a rule exists and how to fix it
```

Directories are searched for `.json` files. Files that are not egg exports are
skipped, because published egg repositories keep game configuration files
alongside the eggs; pass `--all-files` to lint them anyway.

**Exit codes:** `0` clean, `1` findings at or above `--fail-on` (default
`error`), `2` could not run.

### Output formats

| `--format` | Use |
|---|---|
| `text` | Human-readable, the default |
| `github` | Workflow commands — inline annotations. Used automatically under GitHub Actions |
| `sarif` | Upload to GitHub code scanning for annotations that persist across runs |
| `json` | Anything else |

## GitHub Action

```yaml
name: eggs
on: [push, pull_request]

jobs:
  egglint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: hostvaultio/egglint@v1
        with:
          paths: eggs/
```

Findings appear as annotations on the diff. To send them to code scanning
instead, so they persist and can be triaged:

```yaml
    permissions:
      contents: read
      security-events: write
    steps:
      - uses: actions/checkout@v4
      - uses: hostvaultio/egglint@v1
        with:
          paths: eggs/
          format: sarif
          output: egglint.sarif
          fail-on: never          # let the upload run, triage in the UI
      - uses: github/codeql-action/upload-sarif@v3
        with:
          sarif_file: egglint.sarif
```

| Input | Default | Meaning |
|---|---|---|
| `paths` | `.` | Files or directories to lint |
| `format` | `github` | Output format |
| `output` | — | Write to a file instead of stdout |
| `fail-on` | `error` | Lowest severity that fails the job (`never` to never fail) |
| `config` | — | Path to a config file |
| `disable` | — | Comma-separated rule IDs to switch off |
| `args` | — | Any additional flags |

## Configuration

`egglint` reads `.egglint.yaml` from the working directory. Every key is
optional, and command line flags layer on top.

```yaml
# Rules to switch off entirely (ID or name).
disable:
  - EGG016            # no-errexit
  - unused-variable

# Raise or lower a rule's severity.
severity:
  EGG014: error       # treat unchecked downloads as a hard failure

# Paths to skip. "**" matches any number of directories.
exclude:
  - "**/world_files/**"

# Lowest severity that fails the run: error (default), warning, info.
fail-on: error

# Lint JSON files that are not egg exports. Default false.
skip-non-eggs: true
```

## Rules

Run `egglint explain <rule>` for the full reasoning behind any of these.

### Schema

| ID | Name | Default | Checks |
|---|---|---|---|
| EGG001 | `invalid-json` | error | The file parses as JSON |
| EGG002 | `unknown-schema` | error | `meta.version` is a known PTDL schema |
| EGG003 | `missing-field` | error | `name`, `author` and `startup` are present |
| EGG004 | `author-email` | error | `author` is a valid email — the panel validates this on import |
| EGG005 | `invalid-uuid` | warning | `uuid` is a real UUID, so re-import updates rather than duplicates |
| EGG006 | `no-docker-images` | error | At least one docker image is declared |
| EGG007 | `config-json-string` | error | `config.files`/`startup`/`logs` contain valid JSON |
| EGG008 | `untagged-image` | warning | Images are tagged, so the runtime cannot change silently |
| EGG009 | `wrong-field-type` | error | Fields hold the type the schema requires (`[]`, not `""`) |

### Install script

| ID | Name | Default | Checks |
|---|---|---|---|
| EGG010 | `no-install-script` | warning | An installation script exists |
| EGG011 | `unknown-entrypoint` | error | The entrypoint is a shell the install image has |
| EGG012 | `script-syntax` | error | The script parses under its declared interpreter |
| EGG013 | `crlf-line-endings` | info | The script has no Windows line endings |
| EGG014 | `unchecked-download` | warning | `curl` that writes a file or pipes to a shell uses `--fail` |
| EGG015 | `no-artifact-assertion` | warning | Something confirms a download actually arrived |
| EGG016 | `no-errexit` | info | The script enables `set -e` |
| EGG017 | `unchecked-fetch` | info | Other `curl` calls use `--fail` |

### Variables and startup

| ID | Name | Default | Checks |
|---|---|---|---|
| EGG020 | `invalid-env-name` | error | `env_variable` is a valid shell identifier |
| EGG021 | `duplicate-env` | error | No two variables share an `env_variable` |
| EGG022 | `reserved-env` | error | No variable shadows one wings or the shell provides |
| EGG023 | `missing-rules` | warning | Variables declare validation rules |
| EGG024 | `required-empty-default` | warning | `required` variables have a usable default |
| EGG025 | `unused-variable` | info | Declared variables are actually referenced |
| EGG030 | `undefined-startup-var` | error | Every `{{PLACEHOLDER}}` in `startup` resolves |

## Calibration

Default severities were set by running against **538 published eggs** from the
public Pterodactyl and Pelican egg repositories. On that corpus the defaults
produce **11 errors across 11 files** — each one manually confirmed as a real
defect, including an unterminated quote in an install script, a `file_denylist`
typed as `""`, duplicate environment variables, and startup commands referencing
placeholders that do not exist.

That exercise changed two rules, and both changes are worth knowing about if you
are writing similar tooling:

**CRLF is not fatal, despite appearances.** Carriage returns genuinely break
`bash`, `dash` and `busybox ash` — `then\r` is not the `then` keyword. About 97%
of published eggs contain them. Both facts are true because the panel normalises
line endings before wings ever receives the script, in the remote endpoint that
serves it. So `egglint` normalises before syntax-checking, exactly as the panel
does, and reports CRLF only as advisory. Checking the raw script would have
reported a syntax error against the large majority of eggs, all of which work.

**`bash -n` does not run `shopt`.** An install script that enables `extglob` on
line 2 and uses `!(pattern)` on line 12 is valid — bash parses as it executes —
but `-n` parses the whole file without executing anything, so the option is never
set. `egglint` detects parser-affecting `shopt -s` calls and passes the matching
`-O` flag.

## Library

The engine is importable, so a panel, registry or CI service can run identical
checks:

```go
import "github.com/hostvaultio/egglint/lint"

linter, err := lint.New(lint.Config{Disable: []string{"EGG016"}})
if err != nil {
    return err
}

report := linter.Run([]string{"eggs/paper.json"})
for _, f := range report.All() {
    fmt.Printf("%s:%d %s %s\n", f.Path, f.Line, f.RuleID, f.Message)
}
```

`lint.Discover` expands directories; `lint.LintBytes` lints eggs already in
memory.

## Contributing

New rules are welcome, especially ones that come from a failure you actually hit.
See [CONTRIBUTING.md](CONTRIBUTING.md). A rule needs a real failure mode, a
default severity justified against published eggs, and documentation explaining
the *why* — `egglint explain` is part of the interface, not an afterthought.

## Licence

MIT. See [LICENSE](LICENSE).

Not affiliated with the Pterodactyl or Pelican projects. Built and maintained by
[Hostvault](https://hostvault.io).
