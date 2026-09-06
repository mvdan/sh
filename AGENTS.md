# AGENTS.md

This file provides guidance to AI coding agents when working with code in this repository.

## Project Overview

`mvdan.cc/sh/v3` is a shell parser, formatter, and interpreter written in Go.
Supports POSIX Shell, Bash, mksh, Zsh, and Bats. Requires Go 1.26+.

Per the README, drive-by AI patches are not helpful unless they come from an
active user or contributor; detailed issues are preferred.

## Common Commands

```bash
go test ./...                          # -short skips the slow bash confirmation tests
go test -run TestName ./syntax         # a single test
go test -race ./...                    # race detector (Linux)
REQUIRE_SHELLS=1 go test ./...         # fail rather than skip when external shells are missing
GOOS=windows go test ./interp          # runs under Wine; always do this after changing interp
GOARCH=386 go test ./...               # 32-bit
GOOS=js GOARCH=wasm go test ./...      # needs $(go env GOROOT)/lib/wasm in PATH; no subprocesses
gofmt -s -l . && go vet ./...          # enforced by CI
go test -fuzz=FuzzParsePrint ./syntax  # after parser or printer changes
go generate ./...                      # after modifying types with stringer directives
cd moreinterp && go test ./...         # separate module; tests the released v3, not the working tree
```

## Architecture

The codebase follows a pipeline: **parse → expand → interpret/format**.

- **`syntax/`** — Core package. Lexer (`lexer.go`), parser (`parser.go`), AST nodes (`nodes.go`), printer (`printer.go`), and walker (`walk.go`). All dialects are handled via `LangVariant`. `syntax/typedjson` encodes the AST as JSON.
- **`expand/`** — Parameter, brace, tilde, and arithmetic expansion, plus globbing via `Config.ReadDir2`.
- **`interp/`** — Runner that executes a `*syntax.File`. Builtins in `builtin.go`. Handlers (Exec, Call, Open, ReadDir2, Stat, Access, ProcSubst) let callers customize I/O and process execution.
- **`pattern/`** — Shell pattern matching, including extended globs.
- **`shell/`** — High-level helpers with Bash semantics: `Fields`, `Expand`, `Split`, `Join`, `Match`, `Glob`.
- **`internal/`** — Shared helpers: the extended pattern matcher, sparse arrays, and `TestMainSetup`.
- **`cmd/shfmt/`** — The formatter. Each printer option is exposed three ways: a `syntax` printer option, a shfmt flag, and an EditorConfig property.
- **`cmd/gosh/`** — Proof-of-concept interactive shell.
- **`fileutil/`** — File utilities used by shfmt.
- **`moreinterp/coreutils`** — interp middleware implementing cat, cp, find, ls, xargs, etc. via u-root, mainly for Windows. A separate module because of that dependency.

## API Stability

The v3 exported API is frozen. Add new symbols and deprecate old ones rather
than breaking them, as with `FunctionNextLine` → `BlockNextLine` and
`ReadDirHandler` → `ReadDirHandler2`. Record desired breaking changes as
`TODO(v4)` comments; see issue #630.

## Testing

- Table-driven tests with `t.Parallel()` are the norm; assertions use `github.com/go-quicktest/qt`.
- Real shells are the oracle. `TestRunnerRunConfirm` runs every `runTests` case in `interp/interp_test.go` through bash, so `want` must match bash byte for byte; mark known divergences with ` #IGNORE reason` or ` #JUSTERR` in `want`. The syntax tests confirm parses against bash 5.3, dash, mksh R59, and zsh 5.9. Missing shells skip silently unless `REQUIRE_SHELLS=1`. Before changing behavior in `interp`, `expand`, or `pattern`, run the snippet in the real shell.
- Platform-specific cases go in `runTestsUnix`, `runTestsWindows`, and `runTests64bit`; `skipIfUnsupported` skips by regex on Windows, macOS, and js/wasm. Platform-specific files use build tags such as `unix`, `!windows`, and `windows`.
- Large tables are Go source: `interp/interp_test.go`, `syntax/filetests_test.go`, `syntax/printer_test.go`. `cmd/shfmt` and `cmd/gosh` use testscript txtar files under `testdata/script`; the `-u` test flag regenerates expected output.
- Fuzz targets: `FuzzParsePrint` and `FuzzQuote` in `syntax`, with corpora in `syntax/testdata/fuzz`, and `FuzzDecode` in `syntax/typedjson`.
- `syntax.DebugPrint(os.Stdout, node)` prints the full AST for debugging.

## Reference Specifications

When checking shell behavior or conformance, consult the upstream sources:

- **POSIX Shell** — The Open Group Base Specifications, "Shell Command Language": <https://pubs.opengroup.org/onlinepubs/9799919799/utilities/V3_chap02.html> (Issue 8, 2024). Builtins and utilities are under `.../utilities/`.
- **Bash** — GNU Bash Reference Manual: <https://www.gnu.org/software/bash/manual/bash.html>.
- **Zsh** — the `zshall` manual (all zsh man pages combined): <https://zsh.sourceforge.io/Doc/Release/zshall.html>.

## Issue Tracking

- Issues are tracked on GitHub. Use the `gh` CLI to interact with them.
- When investigating an issue, always load its comments too (e.g. `gh issue view <n> --comments`), not just the description.

## Commits

- Subject line: `package: lowercase summary`, no trailing period. Examples: `interp: make -nt and -ot conform to POSIX when a file is missing`, `syntax: fix a crash with LangZsh and Simplify(true)`.
- The prefix is the affected package/path. Use a comma-separated list for several (`expand,syntax: ...`) or `all:` for repo-wide changes.
- Any behavior change gets a body, wrapped at ~72 columns, explaining the symptom, the cause, and the fix, citing what Bash or POSIX does. `Fixes #NNNN.` closes an issue; `For #NNNN.` references one without closing it.
- Bug fixes are two commits: the first adds test cases asserting the current wrong behavior and says so in its body (`For #NNNN.`); the second fixes the bug and flips them (`Fixes #NNNN.`). Panics and hangs are fixed in a single commit.

## Release Notes

New entries go at the top of `CHANGELOG.md`, as `## [X.Y.Z] - YYYY-MM-DD`.

- Read `git log <last tag>..HEAD` with full bodies; the bodies hold the issue
  references and the user-visible rationale.
- Fold each test/fix commit pair into one bullet describing the final behavior.
- Group bullets under `- **package**` headers, roughly in dependency order:
  `cmd/shfmt`, `syntax`, `syntax/typedjson`, `interp`, `expand`, `pattern`,
  `fileutil`. Within a section, additions first, then fixes.
- Bullets are short imperative phrases with identifiers and shell snippets in
  backticks, ending in ` - #NNNN` for the issues they close (comma-separated
  for several). Add a security advisory ID the same way.
- Only mention what an importer or `shfmt` user would notice. Omit doc, test,
  README, CI, and dependency commits, as well as `moreinterp`.
- Omit changes which are both narrow and lack an issue; keep issue-less ones
  only for API additions, panics, and new platform support. Collapse several
  small conformance fixes in one package into a single bullet.
- Add a lead paragraph only for things which don't fit a bullet: dropped Go
  versions, breaking changes, or a theme for the release.

## Backport Releases

A patch release like `v3.14.1` is a `release-vX.Y` branch cut from the
`vX.Y.0` tag, not a linear cut from master. Cherry-pick with `-x`, in master's
chronological order, keeping each test/fix pair together and in order.
Leave the picked commits otherwise untouched, even when a subject line does
not follow the convention above; only resolve genuine conflicts.

Work in `git worktree add` rather than checking the branch out, so that master
stays in place.

The `CHANGELOG.md` entry is written and committed on master like any other,
then cherry-picked onto the branch as the final commit, so that both histories
describe the release identically. It gets no lead paragraph; the bullets are
the whole entry.

Backport build failures, panics, silent corruption of the user's script by the
printer, and interpreter conformance fixes. Test-only fixes qualify when they
unbreak packagers, such as a test which fails on 32-bit. Do not backport new
API, features, or printer style changes: they reformat working code, and users
take a patch release expecting no diff.

Verify the branch with the full matrix in Common Commands, plus `go build` for
freebsd and netbsd on `386` and `arm`, and a fuzz run after printer changes.
Then check for churn: build `shfmt` from the tag and from the branch, run both
over a few hundred real scripts (`/usr/share`, `/etc`, `/usr/bin`), and diff
the output. It should be identical for every file. Any difference is a style
change which does not belong in a patch release.
