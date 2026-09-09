# Plan for v4

This document collects the breaking changes we want to make in the next major
version of `mvdan.cc/sh`, and outlines how to get there. It gathers the
`TODO(v4)` comments in the codebase, the open issues tagged `v4:`, and the
`Deprecated:` symbols accumulated since v3.0.0 (December 2019), together with
changes which make sense now that the module requires Go 1.26: `io/fs`,
generics, and range-over-func iterators.

The v3 API is frozen; see #630. Everything below is a proposal to discuss,
not a commitment. Items marked with an issue number have more context there.

## Principles

- **One breaking round.** Users should migrate once. Anything we are unsure
  about is better left as-is than changed twice.
- **A written transition guide, not tooling.** Every rename and removal is
  listed in `doc/migrate-v4.md` as an old-to-new table with a short note on
  what changed. We do not add `//go:fix` directives for the transition; the
  changes to handlers, options, and paths are not mechanical anyway, and the
  guide has to exist regardless.
- **Behavior stays.** The interpreter keeps being confirmed against Bash byte
  for byte. Formatter output changes only where listed below, and every listed
  change is deliberate and documented in the changelog.
- **No rewrite.** The parser, printer, and interpreter internals are not being
  redesigned; v4 is about the API surface and a handful of long-standing
  design mistakes.
- **Don't use new language features for their own sake.** Iterators and
  `io/fs` earn their place below because they replace worse designs. Generics
  mostly do not; see the section on modern Go.

## Process

1. **Prepare in v3.** Land any last deprecations, so that the final v3
   release already points at the v4 names where they exist. Land new
   formatting behaviors that do not need a v4, such as #678, so that the v4
   formatting diff is as small as possible.
2. **Cut `release-v3`.** Master becomes v4 with `module mvdan.cc/sh/v4`. v3
   keeps receiving backports for panics, build failures, and conformance fixes
   per the existing backport policy, for at least a year after v4.0.0.
3. **Land the changes in phases**, roughly:
   - remove deprecated symbols and finish `ReadDir2`-style replacements;
   - renames: `Dialect`, `Subscript`, `CaseInsensitive`, `fileutil`, and so on;
   - design changes: handler signatures, options structs, exit status, paths,
     environments;
   - `shfmt` flags and EditorConfig properties;
   - formatter style changes.
4. **Pre-release tags** `v4.0.0-alpha.N` and `-beta.N`, asking the larger
   downstream users listed in the README, such as sh-syntax and the editor
   integrations, to try them.
5. **Docs.** A `doc/migrate-v4.md` guide with an old-to-new table, a
   `CHANGELOG.md` entry with a lead paragraph, README updates including the
   Docker `v4` tag, and an updated `shfmt.1.scd`.
6. **Verify** with the same matrix used for backports, plus a churn check:
   run `shfmt` v3 and v4 over a few hundred real scripts and confirm that every
   difference is one of the listed style changes.

## Inventory by package

The identifiers below are v3 names. Each `TODO(v4)` comment in the source
should point back to a bullet here, and be deleted as part of implementing it.

### syntax

Types and naming:

- Rename `LangVariant` to `Dialect`, and `Variant` to `Dialect` or similar
  (#735). Rename the EditorConfig property `shell_variant` to
  `language_dialect` to match the `--language-dialect` flag.
- Make the dialect type an unsigned bitset, `uint32`, since it is already used
  as one internally. Leave the zero value unset and invalid rather than an
  alias for Bash; drop the `langBashLegacy` special case.
- `LangError.Langs` becomes a single dialect bitset instead of a slice.
- `Pos`: expose `Offset`, `Line`, and `Col` as `int64` rather than `uint`, so
  that the internal bit packing can change later and `NewPos` can report
  overflows to the caller. Positions are the only place we use `uint` in
  public APIs.
- Rename the arithmetic operators `AndArit` and `OrArit` to use `Bool`
  consistently with the other logical operators. Remove the deprecated
  `RdrAll` in favor of `RdrClob`.

Nodes:

- `FuncDecl`: join `Name` and `Names` into one field, even if it is mildly
  annoying to non-Zsh users.
- `ParamExp`: replace the mutually exclusive booleans `Excl`, `Length`,
  `Width`, and `IsSet` with a single operator token; rename `Index` to
  `Subscript`, matching Bash and Zsh terminology; consider joining `Repl`,
  `Exp`, and `Slice` into a single expansion field or type.
- `ExtGlob`: make extended globs opaque literals, as we did for Zsh glob
  qualifiers. The `expand` package has to stringify the node again anyway, and
  regular glob operators like `*` do not get their own nodes either.
- `Comment` and other nodes with `Pos` fields are fine; nothing to change.

Parser and printer API:

- Drop the callback forms `Parser.Stmts`, `Parser.Interactive`, and
  `Parser.Words`, and rename the `Seq` variants to take their names. Likewise
  `expand.Braces` and `expand.BracesSeq`. Keep `iter.Seq2[T, error]` as the
  error-reporting convention for iterators across the module.
- Consider swapping functional options for `ParserOptions` and
  `PrinterOptions` structs (#801). They are easier to read in docs, cannot run
  arbitrary code, and are cheaper. The same question applies to
  `interp.RunnerOption`; decide once for the whole module. See open questions.
- Remove `KeepPadding` (#658) and `FunctionNextLine`, superseded by
  `BlockNextLine`. Using the removed flags in `shfmt` becomes an error.
- Replace `SpaceRedirects` with an option to space all operators and delimiters
  (#544): `(( x ))`, `<< EOF`, `( stmts )`, `<( stmts )`, `arr=( elems )`.
  The issue notes that not every style guide wants all of these; decide whether
  one boolean is enough before implementing.
- Deliberate style changes we can only make in a major version: align
  continuation backslashes like comments (#498), and indent the body of a
  multi-line `case` pattern one level deeper than its continuation (#644).
  Consider a configurable continuation indent (#344) at the same time.
- Keep `Walk` for callers who need pruning; `Preorder` already covers the
  iterator case.

### syntax/typedjson

- Give the derived fields `Type`, `Pos`, and `End` names which cannot collide
  with real node fields, such as `_type`, `_pos`, and `_end`. This changes the
  JSON produced by `shfmt --to-json`, so it must land with the CLI changes.
- Consider encoding recovered positions, as they still carry information.

### expand

- `Arithm` and related APIs return `int64` regardless of platform, matching
  Bash, which uses `intmax_t` even on 32-bit systems.
- `Config.ReadDir` is removed; `ReadDir2` takes its name. Globbing switches to
  `io/fs` semantics: slash-separated paths relative to a root, validated with
  `fs.ValidPath`, with results joined by slashes. The caller maps volumes and
  absolute paths onto that root. This fixes the Windows bugs where backslashes
  are treated as separators even when they escape metacharacters, and lets
  `shell.Glob` pass its `fs.FS` straight through without adapters. Consider
  accepting an `fs.ReadDirFS` directly instead of a function.
- `Environ.Each` becomes `All() iter.Seq2[string, Variable]`.
- `WriteEnviron.Set` is overloaded: `export foo` changes attributes but not
  the value, and `foo=bar` the reverse. Split it into explicit operations, such
  as setting a value, setting attributes, and unsetting, and drop the
  `KeepValue` kind which exists only to work around this.
- `Variable`: add a `Name` field so that a variable can report its original
  name, which is needed on Windows where lookups are case-insensitive. The
  field is output-only: `Get` and `All` report it, and `Set` ignores it in
  favor of its name parameter, so hand-built variables need not fill it in.
  Consider
  hiding the value representation behind methods so that sparse indexed arrays
  (#672, currently `Indexes`) and future optimizations do not break users, and
  offer an iterator over array elements. Remove the deprecated `Unset` alias.
- `Config` should not be modified in place when preparing it; either document
  that a `Config` is not safe for concurrent use, or copy it.
- `Fields` and `FieldsSeq`: keep both, since the slice form is the common case,
  but name them consistently with the parser iterators.

### interp

Handlers:

- Pass `HandlerContext` as an explicit parameter instead of hiding it in the
  `context.Context` (#440), and remove `HandlerCtx`. Keep the operation
  parameters separate from the interpreter state, so signatures look like
  `func(ctx context.Context, hc HandlerContext, args []string) error`.
- Join `CallHandler` and `ExecHandlers`: now that middlewares can call `next`
  with changed arguments, the only thing `CallHandler` adds is seeing builtins
  and functions. Make the exec middleware chain see those too, and remove
  `CallHandler` and the deprecated non-middleware `ExecHandler`.
- Port the middleware pattern to `OpenHandler`, `StatHandler`,
  `ReadDirHandler`, and `AccessHandler`, so that they compose the same way.
- Remove `ReadDirHandler` and `ReadDirHandlerFunc`; the `2` variants take
  their names. Clean paths before passing them to handlers.
- `HandlerContext.Stdin` becomes an `*os.File` outside js/wasm; decide how to
  represent the wasm case, where there are no OS pipes, without making the
  common case worse.
- Treat handler errors as non-fatal by default, with an explicit way to return
  a fatal error carrying an exit status.
- We tried an `io/fs`-based handler set in #799 and gave up: `io/fs` has no
  writes and no notion of a root with Windows volumes. Revisit once the
  path model below is in place, and evaluate `os.Root` as a way to confine the
  default handlers to a directory.

Exit status and control flow:

- Replace `ExitStatus` with the `ExitError` struct which v3 added for
  handlers, together with `Exit` and `Fatal`. `Run` returns it for any
  non-zero result, so callers recover the code with `errors.As` and its
  `Status` method, as with `exec.ExitError`. Unlike a `uint8`, the struct
  can grow to report whether the shell exited and the cause of a fatal
  exit, which lets `Runner.Exited` become a method on the error. Remove the
  deprecated `NewExitStatus` and `IsExitStatus` along with it.

Runner state and options:

- Remove the `Vars` and `Funcs` maps. Provide read-only access after `Run`
  via something like `Runner.Env() expand.Environ` and an iterator over
  declared functions (#822).
- Let the caller supply the environment which global variables are written
  to, much like `source`, so that a script's effects can be recorded or
  persisted without copying them out after `Run`. Today the global scope
  always lives in an internal overlay, and only that overlay knows how to
  apply `KeepValue`, read-only attributes, and unsetting. This requires a
  redesign of how variables are stored and modified, together with the
  `WriteEnviron.Set` split above, so that outside implementations are
  feasible. It should be an explicit option rather than inferred from the
  `Env` value's methods, and `Reset` cannot undo writes made to it.
- Make `Env` and `Dir` fail when applied after a reset, instead of being
  silently held back until the next one.
- Split `Params` into `PosixOpts`, mirroring `BashOpts`, and positional
  parameters, so that user-supplied arguments cannot set options by accident.
- Rename `Subshell` to `Clone`, making its deep copy and concurrency safety
  explicit. That leaves room for exposing the cheaper internal variant which
  shares the parent's state, under a name of its own, if a use case appears.
- Path model: on Windows, `Dir`, `PWD`, tilde expansion, and glob results
  currently mix backslashes and slashes, and some paths are split on
  backslashes even though a backslash is the shell's escape character. Like
  busybox-w32, always use slash-separated paths with a volume prefix such as
  `C:/foo` or `//srv/share`, treat backslashes only as escapes, and convert at
  the OS boundary. Environment values imported from the OS stay as-is.

### pattern

- Flip `NoGlobStar` to an opt-in `GlobStar`, matching Bash.
- Flip `EntireString` to an opt-out `PartialMatch`; forgetting `EntireString`
  has caused subtle bugs repeatedly.
- Rename `NoGlobCase` to `CaseInsensitive`.
- Drop the unused `Mode` parameter from `HasMeta` and `QuoteMeta`.
- Make `Mode` a `uint32` like the dialect bitset.

### shell

- Drop the `filepath.ToSlash` adapters in `Glob` once `expand` globs with
  `io/fs` semantics. Otherwise the package stays as the simple Bash-flavored
  entry point.

### fileutil

- Rename the package (#734); `shebang` or `script` describe what it does.
- Remove `HasShebang`, redundant with `Shebang`, and the deprecated
  `CouldBeScript`, letting `CouldBeScript2` take the name.

### cmd/shfmt

- Remove the `-tojson` spelling in favor of `--to-json` only.
- Make `--to-json` compact by default, with `--to-json=indent` or similar for
  the current output.
- Remove `-kp` and `-fn`; replace `-sr` with the new spacing option.
- Drop the older EditorConfig spellings `switch_case_indent` and
  `shell_variant`; v3 already accepts `case_indent` and `language_dialect`,
  so shared `.editorconfig` files can switch ahead of time. Drop
  `keep_padding` and `function_next_line` along with their printer options.
- Consider including hidden files when `--detect` is not the default mode.
- Promote `--exp.recover` to a stable flag, also as an EditorConfig property.
- Publish the `v4` Docker tag; `v3` stays pinned to the maintenance branch.

### moreinterp

A separate module which depends on the released `v3`; it gets a `v4` bump
once `v4.0.0` is out, with its middleware adapted to the new handler
signatures. It is a useful early tester for the handler changes.

## Modern Go

Beyond the items above, these are the language and library changes since v3
was designed which should shape the new API.

- **`io/fs`** (Go 1.16). Directory listing already moved to `fs.DirEntry`;
  v4 finishes the job by adopting slash-separated, root-relative paths in
  globbing, and by removing the pre-`io/fs` handlers. `fs.FS` remains
  read-only, so the interpreter keeps its handler functions for opening and
  writing files, but with the same path model.
- **`os.Root`** (Go 1.24). Worth evaluating as the basis for a confined
  default `OpenHandler`, `StatHandler`, and `ReadDirHandler`, which is a common
  request from users sandboxing scripts.
- **Iterators** (Go 1.23). Every callback-with-bool API becomes an
  `iter.Seq` or `iter.Seq2`: `Environ.Each`, the parser's `Stmts`,
  `Interactive`, and `Words`, `expand.Braces`, and the new read-only accessors
  on `Runner`. Keep `Walk` for pruning and `Preorder` for the flat case.
- **Generics** (Go 1.18). Little of the public API benefits; the AST is an
  interface hierarchy and stays one. Internally they already serve small
  helpers in the lexer, which is about the right scale. Do not add generic
  option or node types.
- **`errors.As` and error wrapping.** `ExitError` implements `error` and
  is found with `errors.As`. Handlers may return wrapped
  `*fs.PathError` values, and the interpreter should unwrap them.
- **`encoding.TextMarshaler`.** Operators gained `TextUnmarshaler` in v3.14;
  add the marshaler side and implement `flag.Value` for `Dialect`, so that
  `shfmt` and `typedjson` share one representation.
- **Sizes as `int64`.** Arithmetic results, positions, and counts use `int64`
  in public APIs, avoiding `int` and `uint` whose width changes on 32-bit
  platforms.
- **Options structs.** `context.Context` stays as the first parameter of every
  handler. Configuration moves to structs with exported fields (#801), the same
  shape as `expand.Config` and `typedjson.EncodeOptions` already have, so the
  module has a single convention.

## Open questions

- Functional options versus options structs for `syntax.Parser`,
  `syntax.Printer`, and `interp.Runner`. Structs are the recommendation; the
  main cost is losing the terse `NewParser()` call for the default case.
- Whether one "space all operators" boolean is enough, given that style guides
  disagree per construct (#544), or whether `shfmt` needs a small set.
- How to represent the Windows path model in `expand` without penalizing
  Unix callers, and whether `interp` should ship an `fs.FS`-backed read-only
  handler set alongside the function handlers.
- How much of `expand.Variable` to hide behind methods. Fields are convenient;
  methods let sparse arrays and future storage changes stay invisible.
- Whether to keep js/wasm support for the interpreter, which currently forces
  `Stdin` to be an `io.Reader` and adds in-process pipes.
- Which of the many formatter feature requests without a v4 dependency should
  still be batched here, so that users see one round of diffs.
