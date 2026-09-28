# Plan for v4

This document collects the breaking changes we want to make in the next major
version of `mvdan.cc/sh`, and outlines how to get there. It gathers the
`TODO(v4)` comments in the codebase, the open issues tagged `v4:`, and the
`Deprecated:` symbols accumulated since v3.0.0 (December 2019), together with
changes which make sense now that the module requires Go 1.26: `os.Root`,
generics, and range-over-func iterators.

The v3 API is frozen. Everything below is a proposal to discuss, not a
commitment. Items marked with an issue number have more context there.

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
- **Don't use new language features for their own sake.** Iterators earn
  their place below because they replace worse designs. Generics and `fs.FS`
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
   - design changes: handler signatures, options structs, exit status, paths,
     environments;
   - `shfmt` flags and EditorConfig properties;
   - formatter style changes;
   - renames: `Dialect`, `Subscript`, `CaseInsensitive`, `fileutil`, and so on.

   The renames go last, shortly before the first beta. They touch the most
   lines, so landing them early would make every backport to `release-v3`
   conflict while the design work is still ongoing.
4. **Pre-release tags** `v4.0.0-alpha.N` and `-beta.N`, asking the larger
   downstream users listed in the README, such as sh-syntax and the editor
   integrations, to try them.
5. **Docs.** A `doc/migrate-v4.md` guide with an old-to-new table and the
   list of silent behavior changes below, a `CHANGELOG.md` entry with a lead
   paragraph, README updates including the Docker `v4` tag, and an updated
   `shfmt.1.scd`.
6. **Verify** with the same matrix used for backports, plus a churn check:
   run `shfmt` v3 and v4 over a few hundred real scripts and confirm that every
   difference is one of the listed style changes.

## Inventory by package

The identifiers below are v3 names. Each `TODO(v4)` comment in the source
should point back to a bullet here, and be deleted as part of implementing it.

### syntax

Types and naming:

- Rename `LangVariant` to `Dialect`, and `Variant` to `Dialect` or similar
  (#735).
- Make the dialect type an unsigned bitset, `uint32`, since it is already used
  as one internally. Leave the zero value unset rather than an alias for Bash,
  and drop the `langBashLegacy` special case. An unset dialect in parser
  options selects the default, Bash, so that the zero options remain useful;
  anywhere else, such as `Quote`, it is an error.
- Remove `LangAuto`, which is not a dialect: the parser panics on it, and only
  `shfmt` uses it. `shfmt` keeps accepting `auto` via its own flag type.
  Consider exporting its detection by filename and shebang from the package
  replacing `fileutil`.
- An invalid dialect is reported as an error by the parser rather than a
  panic when building the options.
- `LangError.Langs` becomes a single dialect bitset instead of a slice.
- `Pos`: expose `Offset`, `Line`, and `Col` as `int64` rather than `uint`, so
  that the internal bit packing can change later and `NewPos` can report
  overflows to the caller. A plain `int` like in `go/token` would not do, as
  on 32-bit platforms it cannot hold the offsets past 2GiB which `Pos`
  supports today.
- `Indent` takes an `int` rather than a `uint`.
- Rename the arithmetic operators `AndArit` and `OrArit` to use `Bool`
  consistently with the other logical operators. Remove the deprecated
  `RdrAll` in favor of `RdrClob`.
- `IsKeyword` takes a dialect, as it only knows about POSIX and Bash.
- Error types are returned as pointers and have pointer receivers, like
  `*fs.PathError`, so that there is one type to look for with
  `errors.AsType`. v3 returns `ParseError` and `LangError` as values but
  `*QuoteError` as a pointer. The same goes for `UnsetParameterError` and
  `UnexpectedCommandError` in `expand`, and `SyntaxError` in `pattern`.
- Remove `IsIncomplete` in favor of `errors.Is` with `io.ErrUnexpectedEOF`,
  which an incomplete `ParseError` unwraps to (#736). Errors from readers,
  handlers, and callbacks are wrapped rather than flattened into text.

Nodes:

- `FuncDecl`: join `Name` and `Names` into one field, even if it is mildly
  annoying to non-Zsh users.
- `ParamExp`: replace the mutually exclusive booleans `Excl`, `Length`,
  `Width`, and `IsSet` with a single operator token; rename `Index` to
  `Subscript`, matching Bash and Zsh terminology; consider joining `Repl`,
  `Exp`, and `Slice` into a single expansion field or type.
- Subscripts are not always arithmetic. Zsh takes associative array keys
  literally, so parsing `h[foo-bar]` as a subtraction makes the printer
  break the script (#1233). The type of `Subscript` must allow a word, and
  Zsh should parse unflagged subscripts as words by default.
- `ExtGlob`: make extended globs opaque literals, as we did for Zsh glob
  qualifiers. The `expand` package has to stringify the node again anyway, and
  regular glob operators like `*` do not get their own nodes either.
- `Comment` and other nodes with `Pos` fields are fine; nothing to change.

Parser and printer API:

- Drop the callback forms `Parser.Stmts`, `Parser.Interactive`, and
  `Parser.Words`, and rename the `Seq` variants to take their names. Likewise
  `expand.Braces` and `expand.BracesSeq`. Keep `iter.Seq2[T, error]` as the
  error-reporting convention for iterators across the module.
- Swap functional options for `ParserOptions` and `PrinterOptions` structs
  (#801). They are easier to read in docs, cannot run arbitrary code, and are
  cheaper. The same applies to `interp.RunnerOption`; see the section on
  modern Go. Printer fields are named after the long `shfmt` flags, so
  `SwitchCaseIndent` becomes `CaseIndent`.
- Remove `KeepPadding` (#658) and `FunctionNextLine`, superseded by
  `BlockNextLine`. Using the removed flags in `shfmt` becomes an error.
- Replace `SpaceRedirects` with an option to space operators and delimiters
  (#544): `(( x ))`, `<< EOF`, `( stmts )`, `<( stmts )`, `arr=( elems )`.
  Style guides choose per construct, such as Google wanting `(( x ))` but
  `$(cmd)`, and Gentoo wanting only `arr=( elems )`. So rather than one
  boolean, the option is a set of groups, and `shfmt` takes a list such as
  `--space=redirects,arrays`. The set of groups is an open question.
- Deliberate style changes we can only make in a major version: align
  continuation backslashes like comments (#498), and keep the continuation
  lines of a multi-line `case` pattern at the level of its first line rather
  than one level deeper, so that they do not blend with the body (#644).
  Consider a configurable continuation indent (#344) at the same time.
- Keep `Walk` for callers who need pruning; `Preorder` already covers the
  iterator case.

### syntax/typedjson

- Give the derived fields `Type`, `Pos`, and `End` names which cannot collide
  with real node fields, such as `_type`, `_pos`, and `_end`. This changes the
  JSON produced by `shfmt --to-json`, so it must land with the CLI changes.
- Consider encoding recovered positions, as they still carry information.
- A JSON schema (#967) should wait for these changes.

### expand

- `Arithm` and related APIs return `int64` regardless of platform, matching
  Bash, which uses `intmax_t` even on 32-bit systems.
- `Config.ReadDir` is removed; `ReadDir2` takes its name. Globbing switches to
  the path model described under `interp`: slash-separated paths, absolute
  ones keeping their root or volume prefix, with results joined by slashes and
  spelled like the pattern. This fixes the Windows bugs where backslashes are
  treated as separators even when they escape metacharacters.

  We do not adopt `io/fs` paths here. `fs.ValidPath` rejects rooted paths and
  `..` elements, yet `/usr/*` and `../*.go` are everyday patterns, and
  resolving `..` lexically to fit gives the wrong directory when `$PWD` is
  reached via a symlink. For the same reason `ReadDir` stays a function
  rather than an `fs.ReadDirFS`, which also has no room for a
  `context.Context`.
- The `Config` callbacks `CmdSubst`, `ProcSubst`, and `ReadDir` take a
  `context.Context`, as do the expansion methods on `Expander` below.
  The interpreter no longer needs to stash a context in the `Runner` for them.
- `Config.Env` is only written to if it is set via a separate
  `WriteEnviron` field, rather than by asserting the type of a read-only
  field, matching the explicit option in `interp`.
- `Environ.Each` becomes `All() iter.Seq2[string, Variable]`.
- `WriteEnviron.Set` is overloaded: `export foo` changes attributes but not
  the value, and `foo=bar` the reverse. Split it into explicit operations, such
  as setting a value, setting attributes, and unsetting, and drop the
  `KeepValue` kind which exists only to work around this.
- `Variable`: consider hiding the value representation behind methods so
  that sparse indexed arrays (#672, currently `Indexes`) and future
  optimizations do not break users, and offer an iterator over array
  elements. Either way, array
  indices are `int64` rather than `int`, as Bash subscripts are `intmax_t`.
  Remove the deprecated `Unset` alias.
- On Windows, variables are case-sensitive like on any other platform.
  v3 matches every name case-insensitively, so that `for path in *`
  overwrites `PATH` (#772). No other shell on Windows does that: Cygwin,
  MSYS2, and busybox-w32 rename variables once, when taking them from the
  OS, and are case-sensitive from then on.

  Like Cygwin and MSYS2, `ListEnviron` upper-cases a fixed list of names
  on Windows, such as `Path` to `PATH`, along with `TEMP`, `TMP`,
  `COMSPEC`, and `SYSTEMROOT`. Any other name keeps its case (#1232),
  unlike in busybox-w32, which upper-cases them all. Names are not
  restored when running a program; `os/exec` already drops entries which
  differ only in case, keeping the last. A variable then has one name,
  so we do not add a `Name` field to `Variable` as v3 meant to.
- Replace the functions taking a `*Config` with methods on an `Expander` built
  from a `Config`, which holds the scratch state for expanding and is not safe
  for concurrent use, like `syntax.Parser`. v3 keeps that state in a
  `sync.Pool`, as a `Config` is shared and the state escapes to the heap; an
  `Expander` reused by its owner, such as one per `interp.Runner`, needs no
  pool nor allocations. `Config` stays a plain struct of options which is never
  modified.
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
  `HandlerContext` reports what the command resolved to, one of a function,
  a builtin, or an external program, as a middleware which handles every
  command without calling `next` would otherwise start blocking the first
  two. Downstream sandboxes do exactly that to deny external programs.
  The last handler in the chain runs functions and builtins as well as
  external programs, so calling `next` works for all three, and
  `HandlerContext.Builtin` is removed. It resolves the arguments it is
  given, so a middleware which rewrites the command name may turn a program
  into a builtin like `CallHandler` could; the reported kind is that of the
  arguments before any middleware ran.
- The set of builtins stays fixed. A middleware can override a builtin or
  provide a command like `cp` (#93), but `type`, `command -v`, and `builtin`
  do not know about either. Resolving the command kind from the expanded
  arguments is also where `\export foo=bar` should find the builtin rather
  than fall through to an external program (#743).
- `DefaultExecHandler` is removed, as `next` reaches the default handler and
  a standalone function cannot run functions nor builtins. Its kill timeout
  becomes a `Runner` option, whose zero value is the default of two seconds.
- A script which the kernel refuses with `ENOEXEC` is run by the default
  handler in a new shell which keeps the exec chain and `FileSystem` of its
  `Runner`. In v3 that shell uses the default handlers, so such a script
  escapes any sandbox which lets it run.
- Replace `OpenHandler`, `StatHandler`, `ReadDirHandler`, and `AccessHandler`
  with a single `FileSystem` interface holding those four methods, each
  taking a `context.Context` and a `HandlerContext`. Filesystem operations
  are customized together in practice, and an interface composes by embedding
  one implementation in another and overriding some methods, without a
  middleware chain per operation. A virtual filesystem then cannot forget
  `Access`, which is why we meant to fold it into `Stat`. `ProcSubstHandler`
  stays separate. Clean paths before passing them to the interface, and
  export the conversion to an OS path, which an implementation on Windows
  needs before it can use `os`. Paths are always absolute.

  We keep the `HandlerContext` parameter for now, but could choose to
  remove it. Nearly every file handler which uses one, in v3 and
  downstream, only does so to join a relative path with the current
  directory, which absolute paths make unnecessary. Without it, a
  `FileSystem` can be written, wrapped, and tested without an interpreter,
  and its `ReadDir` method can be used in an `expand.Config` as-is.
  The cost would be that it cannot tell which position in the script
  caused an operation, nor write to the shell's standard error, which
  a few downstream handlers do; it would report problems by returning
  errors.

  Permissions are an `fs.FileMode`, and `Stat` keeps its parameter to
  follow symlinks rather than growing the interface with an `Lstat`.

  The interface is our own, as the standard library will not provide one:
  the proposal for writable `io/fs` interfaces, golang/go#45757, was declined
  in 2023. It stays at the four methods which the shell itself needs, using
  the open flags from `os`; utilities like `mkdir` and `rm` are programs.

  As a draft, where the names are up for discussion:

  ```go
  // CommandKind is what the name of a simple command resolved to.
  // The zero value is used when a handler is not running a command.
  type CommandKind uint8

  const (
  	CommandFunction CommandKind = iota + 1
  	CommandBuiltin
  	CommandProgram
  )

  type HandlerContext struct {
  	// Command is only set for exec handlers.
  	Command CommandKind
  	// The other fields remain as in v3.
  }

  type ExecHandlerFunc func(
  	ctx context.Context, hc HandlerContext, args []string,
  ) error

  type ExecMiddleware func(next ExecHandlerFunc) ExecHandlerFunc

  type FileSystem interface {
  	Open(
  		ctx context.Context, hc HandlerContext,
  		path string, flag int, perm fs.FileMode,
  	) (io.ReadWriteCloser, error)
  	Stat(
  		ctx context.Context, hc HandlerContext,
  		path string, followSymlinks bool,
  	) (fs.FileInfo, error)
  	ReadDir(
  		ctx context.Context, hc HandlerContext, path string,
  	) ([]fs.DirEntry, error)
  	Access(
  		ctx context.Context, hc HandlerContext,
  		path string, mode AccessMode,
  	) error
  }
  ```
- The interpreter resolves `/dev/stdin`, `/dev/stdout`, `/dev/stderr`, and
  `/dev/fd/N` in redirections to its own streams, like Bash does, rather
  than passing them to the `FileSystem`. In v3 `echo foo >/dev/stderr`
  writes to the standard error of the process, not the one given via
  `StdIO`, and a virtual filesystem has to use the streams in
  `HandlerContext` to get it right. This can be fixed in v3 already,
  and removes one reason for a `FileSystem` to need a `HandlerContext`.
- Ship two implementations of `FileSystem`: the default, which uses `os`
  directly like v3, and one confined to a directory via an `*os.Root`,
  which maps the shell's absolute paths onto the root.

  `os.Root` cannot be the only option. It is a concrete type, so it rules out
  the virtual filesystems which callers implement via handlers today. It
  rejects every symlink with an absolute target, such as `/dev/stdin` on
  Linux, even when the root is `/`, so it cannot back an unconfined shell.
  It has no counterpart to `access(2)`, and Windows needs a root per volume.
- A `FileSystem` of any kind, confined or virtual, only covers the file
  access done by the shell itself: redirections, globbing, `test`, `cd`,
  `source`, and so on. External programs are not confined by it, so a
  sandbox must restrict the exec handlers as well. Document this on the
  interface and on the `os.Root` implementation.
- `LookPathDir` takes the name of the deprecated `LookPath`, and finds files
  via the `FileSystem` rather than `os.Stat`.
- `HandlerContext.Stdin` becomes an `*os.File` outside js/wasm; decide how to
  represent the wasm case, where there are no OS pipes, without making the
  common case worse.
- Treat handler errors as non-fatal by default: the error is printed to
  standard error, the command fails with status 1, and the shell carries on.
  A handler chooses another status or stops the shell via `ExitError` below.
  Context cancellation remains fatal.
- We tried an `io/fs`-based handler set in #799 and gave up: `io/fs` has no
  writes and no notion of a root with Windows volumes. It remains a poor fit
  for the reasons given under `expand`, so we do not ship a `FileSystem`
  backed by an `fs.FS` either.

Exit status and control flow:

- Replace `ExitStatus` with the `ExitError` struct which v3 added for
  handlers, together with `Exit` and `Fatal`. `Run` returns it for any
  non-zero result, so callers recover the code with `errors.AsType` and its
  `Status` method, as with `exec.ExitError`. Unlike a `uint8`, the struct
  can grow to report the cause of a fatal exit. Remove the deprecated
  `NewExitStatus` and `IsExitStatus` along with it.
- Keep `Runner.Exited`. It cannot move to the error, as `Run` returns nil
  after `exit 0`, which is when an interactive caller needs it.
- In v3 an `ExitError` always exits the shell, and a handler fails a command
  by returning an `ExitStatus`. With one type, there are three constructors:
  a new one to fail the command with a status, `Exit` to exit the shell like
  the builtin, and `Fatal` to halt it with a cause.

Runner state and options:

- Remove the `Vars` and `Funcs` maps. Provide read-only access after `Run`
  via something like `Runner.Env() expand.Environ` and an iterator over
  declared functions (#822).
- Unexport `Env`, `Dir`, and `Params` too. They are documented as read-only
  and only set via options, which a field cannot enforce; the current
  directory and positional parameters get accessor methods.
- Let the caller supply the environment which global variables are written
  to, much like `source`, so that a script's effects can be recorded or
  persisted without copying them out after `Run`. Today the global scope
  always lives in an internal overlay, and only that overlay knows how to
  apply `KeepValue`, read-only attributes, and unsetting. This requires a
  redesign of how variables are stored and modified, together with the
  `WriteEnviron.Set` split above, so that outside implementations are
  feasible. It should be an explicit option rather than inferred from the
  `Env` value's methods, and `Reset` cannot undo writes made to it.
- With options structs, options are no longer applied to an existing
  `Runner`, which removes the trap of `Env` and `Dir` being silently held
  back until the next reset. What callers do change between runs, such as
  the standard streams, needs methods; see open questions.
- Split `Params` into `PosixOpts`, mirroring `BashOpts`, and positional
  parameters, so that user-supplied arguments cannot set options by accident.
- Rename `Subshell` to `Clone`, making its deep copy and concurrency safety
  explicit. That leaves room for exposing the cheaper internal variant which
  shares the parent's state, under a name of its own, if a use case appears.
  The accessor methods must work on a clone, whose `Env` and `Vars` are
  empty in v3 (#1007).
- Path model: on Windows, `Dir`, `PWD`, tilde expansion, and glob results
  currently mix backslashes and slashes, and some paths are split on
  backslashes even though a backslash is the shell's escape character. Like
  busybox-w32, always use slash-separated paths with a volume prefix such as
  `C:/foo` or `//srv/share`, treat backslashes only as escapes, and convert at
  the OS boundary. Environment values imported from the OS stay as-is.
  `expand` and the `FileSystem` methods use the same model, so there is one
  kind of path across the module. On Unix these are the native paths.

### pattern

- Flip `NoGlobStar` to an opt-in `GlobStar`, matching Bash.
- Flip `EntireString` to an opt-out `PartialMatch`; forgetting `EntireString`
  has caused subtle bugs repeatedly.
- Rename `NoGlobCase` to `CaseInsensitive`.
- Drop the unused `Mode` parameter from `HasMeta` and `QuoteMeta`.
- Make `Mode` a `uint32` rather than a `uint`, like the dialect bitset.

### shell

- Drop the `filepath.ToSlash` adapters in `Glob` once `expand` globs with
  slash-separated paths. `Glob` keeps mapping those paths onto its `fs.FS`,
  treating anything outside of it as an empty directory. Otherwise the
  package stays as the simple Bash-flavored entry point.

### fileutil

- Rename the package (#734); `shebang` or `script` describe what it does.
- Remove `HasShebang`, redundant with `Shebang`, and the deprecated
  `CouldBeScript`, letting `CouldBeScript2` take the name.
- `Shebang` reports the command of any shebang line with its arguments,
  such as `bash -x` or `python`, rather than the name of a known shell
  (#846). Matching a shell moves to the dialect detection, which may also
  learn the `# shellcheck shell=` directive (#1151).

### cmd/shfmt

- Remove the `-tojson` spelling in favor of `--to-json` only.
- Make `--to-json` compact by default, with `--to-json=indent` or similar for
  the current output.
- Remove `-kp` and `-fn`; replace `-sr` with the new spacing option, where
  `--space=redirects` is its equivalent.
- Drop the older EditorConfig spellings `switch_case_indent` and
  `shell_variant`; v3 already accepts `case_indent` and `language_dialect`,
  so shared `.editorconfig` files can switch ahead of time. Drop
  `keep_padding` and `function_next_line` along with their printer options.
- Consider including hidden files when `--detect` is not the default mode.
- Promote `--exp.recover` to a stable flag, also as an EditorConfig property.
- Publish the `v4` Docker tag; `v3` stays pinned to the maintenance branch.

### x

A separate module, `mvdan.cc/sh/x`, which depends on the released `v3`.
It is a useful early tester for the handler changes, so it moves to the v4
pre-releases as soon as the handler signatures land, rather than waiting for
`v4.0.0`.

## Silent behavior changes

Most of the changes above break the build of a v3 user, who then looks at the
migration guide. These do not: the code still compiles, or the script still
runs, but behaves differently. The guide lists them first.

- `pattern`: a zero `Mode` matches the entire string rather than part of it,
  and no longer supports `**`.
- `syntax`: a zero dialect is unset rather than Bash.
- `syntax`: Zsh subscripts without flags are words rather than arithmetic
  expressions.
- `fileutil`: `Shebang` returns the whole command, and for any program.
- `interp`, `expand`: on Windows, variable names are case-sensitive, and
  only a few names like `Path` are upper-cased.
- `interp`: exec middlewares run for functions and builtins too.
- `interp`: an error returned by a handler fails the command rather than
  halting the shell.
- `interp`: a script without a shebang line runs with the handlers of its
  `Runner` rather than the default ones.
- `interp`: positional parameters starting with `-` or `+` no longer set
  shell options.
- `syntax`, `expand`: errors are pointers, so looking for a value type such
  as `syntax.ParseError` via `errors.As` compiles but never matches.
- `interp`, `expand`: on Windows, paths use slashes, including `PWD` and
  glob results.
- `shfmt`: `--to-json` output is compact and uses different field names.

## Modern Go

Beyond the items above, these are the language and library changes since v3
was designed which should shape the new API.

- **`io/fs`** (Go 1.16, #630). We use its types, `fs.DirEntry`, `fs.FileInfo`,
  and `fs.FileMode`, and remove the older APIs listing directories as
  `fs.FileInfo`. We do not use `fs.FS` nor its path rules in `expand` and
  `interp`: it is read-only, takes no context, and cannot express the rooted
  and `..` paths a shell works with. Only `shell.Glob` takes an `fs.FS`,
  as a convenience which documents those limits.
- **`os.Root`** (Go 1.24). The basis for a confined `FileSystem`, which is a
  common request from users sandboxing scripts, but not a replacement for
  the interface; see the handlers section.
- **Iterators** (Go 1.23). Every callback-with-bool API becomes an
  `iter.Seq` or `iter.Seq2`: `Environ.Each`, the parser's `Stmts`,
  `Interactive`, and `Words`, `expand.Braces`, and the new read-only accessors
  on `Runner`. Keep `Walk` for pruning and `Preorder` for the flat case.
- **Generics** (Go 1.18). Little of the public API benefits; the AST is an
  interface hierarchy and stays one. Internally they already serve small
  helpers in the lexer, which is about the right scale. Do not add generic
  option or node types.
- **`errors.AsType` and error wrapping** (Go 1.26). `ExitError` implements
  `error` and is found with `errors.AsType`. Handlers may return wrapped
  `*fs.PathError` values, and the interpreter should unwrap them.
- **`encoding.TextMarshaler`.** Operators gained `TextUnmarshaler` in v3.14;
  add the marshaler side, and both sides for `Dialect` next to its existing
  `flag.Value` methods, so that `shfmt` and `typedjson` share one
  representation.
- **Integer types.** Arithmetic results, array indices, and positions use
  `int64` in public APIs, as their range must not depend on the platform.
  Lengths and counts of what is held in memory, such as an indent width or a
  number of fields, stay `int`. Unsigned types are only used with a fixed
  size and where no arithmetic happens: `uint32` for the bitsets `Dialect`,
  `pattern.Mode`, and `AccessMode`, and `uint8` for the enumerations
  `OptState` and `ValueKind`. The platform-dependent `uint` is gone from
  `Pos`, `Indent`, and `pattern.Mode`, its three uses in v3.
- **Options structs.** `context.Context` stays as the first parameter of every
  handler. Configuration moves to structs with exported fields (#801), the same
  shape as `expand.Config` and `typedjson.EncodeOptions` already have, so the
  module has a single convention. The zero value of every field is its
  default, so a default other than zero needs a way to ask for zero, such
  as a negative kill timeout to kill right away.
- **Room for additions.** The features wanted by open issues fit the new
  API without another break. More dialects such as ksh (#614) and
  pre-POSIX (#757) are bits in the `uint32`, of which five are in use.
  A cursor to replace nodes while walking (#1165) and parsing without
  building a tree (#621) are a new function and method. A limit on
  ambiguous parsing (#686) and a single process mode (#171) are options
  fields, where the former must treat zero as its default.

## Open questions

- How options structs are passed to `syntax.Parser`, `syntax.Printer`, and
  `interp.Runner`, so that the default case stays close to the terse
  `NewParser()`. The interpreter's handlers and `FileSystem` are fields too.
- Which groups the spacing option has (#544). Redirects, arrays, arithmetic,
  subshells, and heredocs were suggested, but one user wants `( foo )`
  without `$( foo )`, which share a group.
- Which `Runner` settings can change between runs, and via which methods.
  Downstream users apply `StdIO` to an existing runner to swap its output;
  the positional parameters and directory are other candidates, the latter
  being wanted on a clone (#1125).
- Whether `builtin` and `command` run their command through the exec chain
  again, or only appear in it as themselves.
- Whether to prefix our EditorConfig properties (#1094) while we are
  dropping the older spellings. EditorConfig has no convention for it yet.
- The name of the constructor for an `ExitError` which fails a command
  without exiting the shell.
- How much of `expand.Variable` to hide behind methods. Fields are convenient;
  methods let sparse arrays and future storage changes stay invisible.
- Whether to keep js/wasm support for the interpreter, which currently forces
  `Stdin` to be an `io.Reader` and adds in-process pipes.
- Which of the many formatter feature requests without a v4 dependency should
  still be batched here, so that users see one round of diffs. Those which
  were put off as breaking in v3 are keeping `do` on its own line in a
  multi-line `for` (#582), not printing a multi-line command inline (#720),
  and rejecting `[[` in POSIX mode (#1092). #1064 suggests a function style
  option taking the place of `FunctionNextLine`.

## TODO

What remains unsettled beyond the open questions above. Each item is removed
once it is decided and folded into the sections it affects.

Disagreements and decisions which need confirming:

- TODO: options structs (#801) go against the preference of a downstream
  maintainer in that issue, who likes functional options for public APIs.
  Confirm with the larger `interp` users before the first alpha.
- TODO: case-sensitive variables on Windows reverse the direction of the
  v3 changes made for #1232, which made every variable case-insensitive.
  Confirm with go-task, the main user on Windows.
- TODO: a `Shebang` which works for any program is only floated in #846;
  decide whether it belongs in v4.
- TODO: Zsh subscripts as words are proposed as an opt-in parser option for
  v3. Decide whether v4 makes them the default, and whether flagged
  subscripts like `${a[(r)b]}` and `FlagsArithm` change as well.
- TODO: decide between removing `FunctionNextLine` outright and turning it
  into a function style option (#1064), as both cannot happen.
- TODO: settle each "consider" in the inventory one way or the other:
  exporting dialect detection, joining the `ParamExp` expansion fields,
  a continuation indent (#344), encoding recovered positions, and hidden
  files in `shfmt`.

Gaps in the design:

- TODO: decide whether the `FileSystem` methods keep their
  `HandlerContext` parameter; see the `interp` section.
- TODO: a middleware which overrides a builtin cannot change the shell's
  state, as `HandlerContext` is read-only. Replacing `cd` or `export` is
  then not possible; decide whether that is a goal (#93).
- TODO: the last command of a pipeline runs in the current shell, so
  `echo a | read x` sets `x`, unlike in Bash, and pipes are closed late
  (#1142). Fixing it changes what scripts observe, so it would be a silent
  behavior change.
- TODO: builtins like `history` need the caller's help (#1401), as the
  history belongs to the line editor. Decide whether that is another
  handler, or left to a middleware which overrides the builtin.
- TODO: document which node fields may be nil (#374), at least for the
  nodes which change shape: `FuncDecl` and `ParamExp`.

Housekeeping:

- TODO: there is no tracking issue for v4 as a whole.
