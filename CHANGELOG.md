# Changelog

## Unreleased

## [3.1.2] - 2020-06-26

- **syntax**
  - Fix brace indentation when using `FunctionNextLine`
  - Support indirect parameter expansions with transformations
  - Stop heredoc bodies only when the entire line matches
- **interp**
  - Make the tests pass on 32-bit platforms

## [3.1.1] - 2020-05-04

- **cmd/shfmt**
  - Recognise `function_next_line` in EditorConfig files
- **syntax**
  - Don't ignore escaped newlines at the end of heredoc bodies
  - Improve support for parsing regexes in test expressions
  - Count columns for `KeepPadding` in bytes, to better support unicode
  - Never let `KeepPadding` add spaces right after indentation
- **interp**
  - Hide unset variables when executing programs

## [3.1.0] - 2020-04-07

- Redesigned Docker images, including buildx and an Alpine variant
- **cmd/shfmt**
  - Replace source files atomically when possible
  - Support `ignore = true` in an EditorConfig to skip directories
  - Add `-fn` to place function opening braces on the next line
  - Improve behavior of `-f` when given non-directories
  - Docker images and `go get` installs now embed good version information
- **syntax**
  - Add support for nested here-documents
  - Allow parsing for loops with braces, present in mksh and Bash
  - Expand `CaseClause` to describe its `in` token
  - Allow empty lines in Bash arrays in the printer
  - Support disabling `KeepPadding`
  - Avoid mis-printing some programs involving `&`
- **interp**
  - Add initial support for Bash process substitutions
  - Add initial support for aliases
  - Fix an edge case where the status code would not be reset
  - The exit status code can now reflect being stopped by a signal
  - `test -t` now uses the interpreter's stdin/stdout/stderr files
- **expand**
  - Improve the interaction of `@` and `*` with quotes and `IFS`

## [3.0.2] - 2020-02-22

- **syntax**
  - Don't indent after escaped newlines in heredocs
  - Don't parse `*[i]=x` as a valid assignment
- **interp**
  - Prevent subshells from defining funcs in the parent shells
- **expand**
  - Parameters to `Fields` no longer get braces expanded in-place

## [3.0.1] - 2020-01-11

- **cmd/shfmt**
  - Fix an edge case where walking directories could panic
- **syntax**
  - Only do a trailing read in `Parser.Stmts` if we have open heredocs
  - Ensure comments are never folded into heredocs
  - Properly tokenize `)` after a `=~` test regexp
  - Stop parsing a comment at an escaped newline
- **expand**
  - `"$@"` now expands to zero fields when there are zero parameters

## [3.0.0] - 2019-12-16

This is the first stable release as a proper module, now under
`mvdan.cc/sh/v3/...`. Go 1.12 or later is supported.

A large number of changes have been done since the last feature release a year
ago. All users are encouraged to update. Below are the major highlights.

- **cmd/shfmt**
  - Support for [EditorConfig](https://editorconfig.org/) files
  - Drop the dependency on `diff` for the `-d` flag, now using pure Go
- **syntax**
  - Overhaul escaped newlines, now represented as `WordPart` positions
  - Improve some operator type names, to consistently convey meaning
  - Completely remove `StmtList`
  - Redesign `IfClause`, making its "else" another `IfClause` node
  - Redesign `DeclClause` to remove its broken `Opts` field
  - Brace expression parsing is now done with a `BraceExp` word part
  - Improve comment alignment in `Printer` via a post-process step
  - Add support for the `~` bitwise negation operator
  - Record the use of deprecated tokens in the syntax tree
- **interp**
  - Improve the module API as "handlers", to reduce confusion with Go modules
  - Split `LookPath` out of `ExecHandler` to allow custom behavior
  - `Run` now returns `nil` instead of `ShellExitStatus(0)`
  - `OpenDevImpls` is removed; see `ExampleOpenHandler` for an alternative
- **expand**
  - Redesign `Variable` to reduce allocations
  - Add support for more escape sequences
  - Make `Config` a bit more powerful via `func` fields
  - Rework brace expansion via the new `BraceExp` word part
- **pattern**
  - New package for shell pattern matching, extracted from `syntax`
  - Add support for multiple modes, including filenames and braces

Special thanks to Konstantin Kulikov for his contribution to this release.

## [2.6.4] - 2019-03-10

- **syntax**
  - Support array elements without values, like `declare -A x=([index]=)`
  - Parse `for i; do ...` uniquely, as it's short for `for i in "$@"`
  - Add missing error on unclosed nested backquotes
- **expand**
  - Don't expand tildes twice, fixing `echo ~` on Windows
- **interp**
  - Fix the use of `Params` as an option to `New`
  - Support lowercase Windows volume names in `$PATH`

## [2.6.3] - 2019-01-19

- **expand**
  - Support globs with path prefixes and suffixes, like `./foo/*/`
  - Don't error when skipping non-directories in glob walks

## [2.6.2] - 2018-12-08

- **syntax**
  - Avoid premature reads in `Parser.Interactive` when parsing Unicode bytes
  - Fix parsing of certain Bash test expression involving newlines
  - `Redirect.End` now takes the `Hdoc` field into account
  - `ValidName` now returns `false` for an empty string
- **expand**
  - Environment variables on Windows are case insensitive again
- **interp**
  - Don't crash on `declare $unset=foo`
  - Fix a regression where executed programs would receive a broken environment

Note that the published Docker image was changed to set `shfmt` as the
entrypoint, so previous uses with arguments like `docker run mvdan/shfmt:v2.6.1
shfmt --version` should now be `docker run mvdan/shfmt:v2.6.2 --version`.

## [2.6.1] - 2018-11-17

- **syntax**
  - Fix `Parser.Incomplete` with some incomplete literals
  - Fix parsing of Bash regex tests in some edge cases
- **interp**
  - Add support for `$(<file)` special command substitutions

## [2.6.0] - 2018-11-10

This is the biggest v2 release to date. It's now possible to write an
interactive shell, and it's easier and safer to perform shell expansions.

This will be the last major v2 version, to allow converting the project to a Go
module in v3.

- Go 1.10 or later required to build
- **syntax**
  - Add `Parser.Interactive` to implement an interactive shell
  - Add `Parser.Document` to parse a single here-document body
  - Add `Parser.Words` to incrementally parse separate words
  - Add the `Word.Lit` helper method
  - Support custom indentation in `<<-` heredoc bodies
- **interp**
  - Stabilize API and add some examples
  - Introduce a constructor, and redesign `Runner.Reset`
  - Move the context from a field to function parameters
  - Remove `Runner.Stmt` in favor of `Run` with `ShellExitStatus`
- **shell**
  - Stabilize API and add some examples
  - Add `Expand`, as a more powerful `os.Expand`
  - Add `Fields`, similar to the old `Runner.Fields`
  - `Source*` functions now take a context
  - `Source*` functions no longer try to sandbox
- **expand**
  - New package, split from `interp`
  - Allows performing shell expansions in a controlled way
  - Redesigned `Environ` and `Variable` moved from `interp`

## [2.5.1] - 2018-08-03

- **syntax**
  - Fix a regression where semicolons would disappear within switch cases

## [2.5.0] - 2018-07-13

- **syntax**
  - Add support for Bash's `{varname}<` redirects
  - Add `SpaceRedirects` to format redirects like `> word`
  - Parse `$\"` correctly within double quotes
  - A few fixes where minification would break programs
  - Printing of heredocs within `<()` no longer breaks them
  - Printing of single statements no longer adds empty lines
  - Error on invalid parameter names like `${1a}`
- **interp**
  - `Runner.Dir` is now always an absolute path
- **shell**
  - `Expand` now supports expanding a lone `~`
  - `Expand` and `SourceNode` now have default timeouts
- **cmd/shfmt**
  - Add `-sr` to print spaces after redirect operators
  - Don't skip empty string values in `-tojson`
  - Include comment positions in `-tojson`

## [2.4.0] - 2018-05-16

- Publish as a JS package, [mvdan-sh](https://www.npmjs.com/package/mvdan-sh)
- **syntax**
  - Add `DebugPrint` to pretty-print a syntax tree
  - Fix comment parsing and printing in some edge cases
  - Indent `<<-` heredoc bodies if indenting with tabs
  - Add support for nested backquotes
  - Relax parser to allow quotes in arithmetic expressions
  - Don't rewrite `declare foo=` into `declare foo`
- **interp**
  - Add support for `shopt -s globstar`
  - Replace `Runner.Env` with an interface
- **shell**
  - Add `Expand` as a fully featured version of `os.Expand`
- **cmd/shfmt**
  - Set appropriate exit status when `-d` is used

## [2.3.0] - 2018-03-07

- **syntax**
  - Case clause patterns are no longer forced on a single line
  - Add `ExpandBraces`, to perform Bash brace expansion on words
  - Improve the handling of backslashes within backquotes
  - Improve the parsing of Bash test regexes
- **interp**
  - Support `$DIRSTACK`, `${param[@]#word}`, and `${param,word}`
- **cmd/shfmt**
  - Add `-d`, to display diffs when formatting differs
  - Promote `-exp.tojson` to `-tojson`
  - Add `Pos` and `End` fields to nodes in `-tojson`
  - Inline `StmtList` fields to simplify the `-tojson` output
  - Support `-l` on standard input

## [2.2.1] - 2018-01-25

- **syntax**
  - Don't error on `${1:-default}`
  - Allow single quotes in `${x['str key']}` as well as double quotes
  - Add support for `${!foo[@]}`
  - Don't simplify `foo[$x]` to `foo[x]`, to not break string indexes
  - Fix `Stmt.End` when the end token is the background operator `&`
  - Never apply the negation operator `!` to `&&` and `||` lists
  - Apply the background operator `&` to entire `&&` and `||` lists
  - Fix `StopAt` when the stop string is at the beginning of the source
  - In `N>word`, check that `N` is a valid numeric literal
  - Fix a couple of crashers found via fuzzing
- **cmd/shfmt**
  - Don't error if non-bash files can't be written to

## [2.2.0] - 2018-01-18

- Tests on Mac and Windows are now ran as part of CI
- **syntax**
  - Add `StopAt` to stop lexing at a custom arbitrary token
  - Add `TranslatePattern` and `QuotePattern` for pattern matching
  - Minification support added to the printer - see `Minify`
  - Add ParamExp.Names to represent `${!prefix*}`
  - Add TimeClause.PosixFormat for its `-p` flag
  - Fix parsing of assignment values containing `=`
  - Fix parsing of parameter expansions followed by a backslash
  - Fix quotes in parameter expansion operators like `${v:-'def'}`
  - Fix parsing of negated declare attributes like `declare +x name`
  - Fix parsing of `${#@}`
  - Reject bad parameter expansion operators like `${v@WRONG}`
  - Reject inline array variables like `a=(b c) prog`
  - Reject indexing of special vars like `${1[3]}`
  - Reject `${!name}` when in POSIX mode
  - Reject multiple parameter expansion actions like `${#v:-def}`
- **interp**
  - Add Bash brace expansion support, including `{a,b}` and `{x..y}`
  - Pattern matching actions are more correct and precise
  - Exported some Runner internals, including `Vars` and `Funcs`
  - Use the interpreter's `$PATH` to find binaries
  - Roll our own globbing to use our own pattern matching code
  - Support the `getopts` sh builtin
  - Support the `read` bash builtin
  - Numerous changes to improve Windows support
- **shell**
  - New experimental package with high-level utility functions
  - Add `SourceFile` to get the variables declared in a script
  - Add `SourceNode` as a lower-level version of the above
- **cmd/shfmt**
  - Add `-mn`, which minifies programs via `syntax.Minify`

## [2.1.0] - 2017-11-25

- **syntax**
  - Add `Stmts`, to parse one statement at a time
  - Walk no longer ignores comments
  - Parameter expansion end fixes, such as `$foo.bar`
  - Whitespace alignment can now be kept - see `KeepPadding`
  - Introduce an internal newline token to simplify the parser
  - Fix `Block.Pos` to actually return the start position
  - Fix mishandling of inline comments in two edge cases
- **interp**
  - Expose `Fields` to expand words into strings
  - First configurable modules - cmds and files
  - Add support for the new `TimeClause`
  - Add support for namerefs and readonly vars
  - Add support for associative arrays (maps)
  - More sh builtins: `exec return`
  - More bash builtins: `command pushd popd dirs`
  - More `test` operators: `-b -c -t -o`
  - Configurable kill handling - see `KillTimeout`
- **cmd/shfmt**
  - Add `-f` to just list all the shell files found
  - Add `-kp` to keep the column offsets in place
- **cmd/gosh**
  - Now supports a basic interactive mode

## [2.0.0] - 2017-08-30

- The package import paths were moved to `mvdan.cc/sh/...`
- **syntax**
  - Parser and Printer structs introduced with functional options
  - Node positions are now independent - `Position` merged into `Pos`
  - All comments are now attached to nodes
  - Support `mksh` - MirBSD's Korn Shell, used in Android
  - Various changes to the AST:
    - `EvalClause` removed; `eval` is no longer parsed as a keyword
    - Add support for Bash's `time` and `select`
    - Merge `UntilClause` into `WhileClause`
    - Moved `Stmt.Assigns` to `CallExpr.Assigns`
    - Remove `Elif` - chain `IfClause` nodes instead
  - Support for indexed assignments like `a[i]=b`
  - Allow expansions in arithmetic expressions again
  - Unclosed heredocs now produce an error
  - Binary ops are kept in the same line - see `BinaryNextLine`
  - Switch cases are not indented by default - see `SwitchCaseIndent`
- **cmd/shfmt**
  - Add `-s`, which simplifies programs via `syntax.Simplify`
  - Add `-ln <lang>`, like `-ln mksh`
  - Add `-bn` to put binary ops in the next line, like in v1
  - Add `-ci` to indent switch cases, like in v1
- **interp**
  - Some progress made, though still experimental
  - Most of POSIX done - some builtins remain to be done

## [1.3.1] - 2017-05-26

- **syntax**
  - Fix parsing of `${foo[$bar]}`
  - Fix printer regression where `> >(foo)` would be turned into `>>(foo)`
  - Break comment alignment on any line without a comment, fixing formatting issues
  - Error on keywords like `fi` and `done` used as commands

## [1.3.0] - 2017-04-24

- **syntax**
  - Fix backslashes in backquote command substitutions
  - Disallow some test expressions like `[[ a == ! b ]]`
  - Disallow some parameter expansions like `${$foo}`
  - Disallow some arithmetic expressions like `((1=3))` and `(($(echo 1 + 2)))`
  - Binary commands like `&&`, `||` and pipes are now left-associative
- **fileutil**
  - `CouldBeScript` may now return true on non-regular files such as symlinks
- **interp**
  - New experimental package to interpret a `syntax.File` in pure Go

## [1.2.0] - 2017-02-22

- **syntax**
  - Add support for escaped characters in bash regular expressions
- **fileutil**
  - New package with some code moved from `cmd/shfmt`, now importable
  - New funcs `HasShebang` and `CouldBeScript`
  - Require shebangs to end with whitespace to reject `#!/bin/shfoo`

## [1.1.0] - 2017-01-05

- **syntax**
  - Parse `[[ a = b ]]` like `[[ a == b ]]`, deprecating `TsAssgn` in favour of `TsEqual`
  - Add support for the `-k`, `-G`, `-O` and `-N` unary operators inside `[[ ]]`
  - Add proper support for `!` in parameter expansions, like `${!foo}`
  - Fix a couple of crashes found via fuzzing
- **cmd/shfmt**
  - Rewrite `[[ a = b ]]` into the saner `[[ a == b ]]` (see above)

## [1.0.0] - 2016-12-13

- **syntax**
  - Stable release, API now frozen
  - `Parse` now reads input in chunks of 1KiB
- **cmd/shfmt**
  - Add `-version` flag

## [0.6.0] - 2016-12-05

- **syntax**
  - `Parse` now takes an `io.Reader` instead of `[]byte`
  - Invalid UTF-8 is now reported as an error
  - Remove backtracking for `$((` and `((`
  - `Walk` now takes a func literal to simplify its use

## [0.5.0] - 2016-11-24

- **cmd/shfmt**
  - Remove `-cpuprofile`
  - Don't read entire files into memory to check for a shebang
- **syntax**
  - Use `uint32` for tokens and positions in nodes
  - Use `Word` and `Lit` pointers consistently instead of values
  - Ensure `Word.Parts` is never empty
  - Add support for expressions in array indexing and parameter expansion slicing

## [0.4.0] - 2016-11-08

- Merge `parser`, `ast`, `token` and `printer` into a single package `syntax`
- Use separate operator types in nodes rather than `Token`
- Use operator value names that express their function
- Keep `;` if on a separate line when formatting
- **cmd/shfmt**
  - Allow whitespace after `#!` in a shebang
- **syntax**
  - Implement operator precedence for `[[ ]]`
  - Parse `$(foo)` and ``foo`` as the same (`shfmt` then converts the latter to the former)
  - Rename `Quoted` to `DblQuoted` for clarity
  - Split `((foo))` nodes as their own type, `ArithmCmd`
  - Add support for bash parameter expansion slicing

## [0.3.0] - 2016-10-26

- Add support for bash's `coproc` and extended globbing like `@(foo)`
- Improve test coverage, adding tests to `cmd/shfmt` and bringing `parser` and `printer` close to 100%
- Support empty C-style for loops like `for ((;;)) ...`
- Support for the `>|` redirect operand
- **cmd/shfmt**
  - Fix issue where `.sh` and `.bash` files might not be walked if running on a directory
  - Fix issue where `-p` was not obeyed when formatting stdin
- **parser**
  - `$''` now generates an `ast.SglQuoted`, not an `ast.Quoted`
  - Support for ambiguous `((` like with `$((`
  - Improve special parameter expansions like `$@` or `$!`
  - Improve bash's `export` `typeset`, `nameref` and `readonly`
  - `<>`, `>&` and `<&` are valid POSIX
  - Support for bash's `^`, `^^`, `,` and `,,` operands inside `${}`

## [0.2.0] - 2016-10-13

- Optimizations all around, making `shfmt` ~15% faster
- **cmd/shfmt**
  - Add `-p` flag to only accept POSIX Shell programs (`parser.PosixConformant`)
- **parser**
  - Add support for ambiguous `$((` as in `$((foo) | bar)`
  - Limit more bash features to `PosixConformant` being false
  - Don't parse heredoc bodies in nested expansions and contexts
  - Run tests through `bash` to confirm the presence of a parse error
- **ast**
  - Add `Walk(Visitor, Node)` function

## [0.1.0] - 2016-09-20

Initial release.

[3.1.1]: https://github.com/mvdan/sh/releases/tag/v3.1.1
[3.1.0]: https://github.com/mvdan/sh/releases/tag/v3.1.0
[3.0.2]: https://github.com/mvdan/sh/releases/tag/v3.0.2
[3.0.1]: https://github.com/mvdan/sh/releases/tag/v3.0.1
[3.0.0]: https://github.com/mvdan/sh/releases/tag/v3.0.0
[2.6.4]: https://github.com/mvdan/sh/releases/tag/v2.6.4
[2.6.3]: https://github.com/mvdan/sh/releases/tag/v2.6.3
[2.6.2]: https://github.com/mvdan/sh/releases/tag/v2.6.2
[2.6.1]: https://github.com/mvdan/sh/releases/tag/v2.6.1
[2.6.0]: https://github.com/mvdan/sh/releases/tag/v2.6.0
[2.5.1]: https://github.com/mvdan/sh/releases/tag/v2.5.1
[2.5.0]: https://github.com/mvdan/sh/releases/tag/v2.5.0
[2.4.0]: https://github.com/mvdan/sh/releases/tag/v2.4.0
[2.3.0]: https://github.com/mvdan/sh/releases/tag/v2.3.0
[2.2.1]: https://github.com/mvdan/sh/releases/tag/v2.2.1
[2.2.0]: https://github.com/mvdan/sh/releases/tag/v2.2.0
[2.1.0]: https://github.com/mvdan/sh/releases/tag/v2.1.0
[2.0.0]: https://github.com/mvdan/sh/releases/tag/v2.0.0
[1.3.1]: https://github.com/mvdan/sh/releases/tag/v1.3.1
[1.3.0]: https://github.com/mvdan/sh/releases/tag/v1.3.0
[1.2.0]: https://github.com/mvdan/sh/releases/tag/v1.2.0
[1.1.0]: https://github.com/mvdan/sh/releases/tag/v1.1.0
[1.0.0]: https://github.com/mvdan/sh/releases/tag/v1.0.0
[0.6.0]: https://github.com/mvdan/sh/releases/tag/v0.6.0
[0.5.0]: https://github.com/mvdan/sh/releases/tag/v0.5.0
[0.4.0]: https://github.com/mvdan/sh/releases/tag/v0.4.0
[0.3.0]: https://github.com/mvdan/sh/releases/tag/v0.3.0
[0.2.0]: https://github.com/mvdan/sh/releases/tag/v0.2.0
[0.1.0]: https://github.com/mvdan/sh/releases/tag/v0.1.0
