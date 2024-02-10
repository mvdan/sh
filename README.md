# sh

[![Go Reference](https://pkg.go.dev/badge/mvdan.cc/sh/v3.svg)](https://pkg.go.dev/mvdan.cc/sh/v3)

A shell parser, formatter, and interpreter. Supports [POSIX Shell], [Bash], and
[mksh]. Requires Go 1.21 or later.

### Quick start

To parse shell scripts, inspect them, and print them out, see the [syntax
examples](https://pkg.go.dev/mvdan.cc/sh/v3/syntax#pkg-examples).

For high-level operations like performing shell expansions on strings, see the
[shell examples](https://pkg.go.dev/mvdan.cc/sh/v3/shell#pkg-examples).

### shfmt

	go install mvdan.cc/sh/v3/cmd/shfmt@latest

`shfmt` formats shell programs. See [canonical.sh](syntax/canonical.sh) for a
quick look at its default style. For example:

	shfmt -l -w script.sh

For more information, see [its manpage](cmd/shfmt/shfmt.1.scd), which can be
viewed directly as Markdown or rendered with [scdoc].

Packages are available on [Alpine], [Arch], [Debian], [Docker], [Fedora], [FreeBSD],
[Homebrew], [MacPorts], [NixOS], [OpenSUSE], [Scoop], [Snapcraft], [Void] and [webi].

### gosh

	go install mvdan.cc/sh/v3/cmd/gosh@latest

Proof of concept shell that uses `interp`. Note that it's not meant to replace a
POSIX shell at the moment, and its options are intentionally minimalistic.

### Fuzzing

We use Go's native fuzzing support. For instance:

	cd syntax
	go test -run=- -fuzz=ParsePrint

### Caveats

* When indexing Bash associative arrays, always use quotes. The static parser
  will otherwise have to assume that the index is an arithmetic expression.

```sh
$ echo '${array[spaced string]}' | shfmt
1:16: not a valid arithmetic operator: string
$ echo '${array[dash-string]}' | shfmt
${array[dash - string]}
```

* `$((` and `((` ambiguity is not supported. Backtracking would complicate the
  parser and make streaming support via `io.Reader` impossible. The POSIX spec
  recommends to [space the operands][posix-ambiguity] if `$( (` is meant.

```sh
$ echo '$((foo); (bar))' | shfmt
1:1: reached ) without matching $(( with ))
```

* Some builtins like `export` and `let` are parsed as keywords.
  This allows statically building their syntax tree,
  as opposed to keeping the arguments as a slice of words.
  It is also required to support `declare foo=(bar)`.
  Note that this means expansions like `declare {a,b}=c` are not supported.

### JavaScript

A subset of the Go packages are available as an npm package called [mvdan-sh].
See the [_js](_js) directory for more information.

### Docker

All release tags are published via [Docker], such as `v3.5.1`.
The latest stable release is currently published as `v3`,
and the latest development version as `latest`.
The images only include `shfmt`; `-alpine` variants exist on Alpine Linux.

To build a Docker image, run:

	docker build -t my:tag -f cmd/shfmt/Dockerfile .

To use a Docker image, run:

	docker run --rm -u "$(id -u):$(id -g)" -v "$PWD:/mnt" -w /mnt my:tag <shfmt arguments>

### Related projects

The following editor integrations wrap `shfmt`:

- [BashSupport-Pro] - Bash plugin for JetBrains IDEs
- [intellij-shellcript] - Intellij Jetbrains `shell script` plugin
- [micro] - Editor with a built-in plugin
- [shell-format] - VS Code plugin
- [vscode-shfmt] - VS Code plugin
- [shfmt.el] - Emacs package
- [Sublime-Pretty-Shell] - Sublime Text 3 plugin
- [Trunk] - Universal linter, available as a CLI, VS Code plugin, and GitHub action
- [vim-shfmt] - Vim plugin

Other noteworthy integrations include:

- [modd] - A developer tool that responds to filesystem changes
- [prettier-plugin-sh] - [Prettier] plugin using [mvdan-sh]
- [sh-checker] - A GitHub Action that performs static analysis for shell scripts
- [mdformat-shfmt] - [mdformat] plugin to format shell scripts embedded in Markdown with shfmt
- [pre-commit-shfmt] - [pre-commit] shfmt hook

[alpine]: https://pkgs.alpinelinux.org/packages?name=shfmt
[arch]: https://archlinux.org/packages/extra/x86_64/shfmt/
[bash]: https://www.gnu.org/software/bash/
[BashSupport-Pro]: https://www.bashsupport.com/manual/editor/formatter/
[debian]: https://tracker.debian.org/pkg/golang-mvdan-sh
[docker]: https://hub.docker.com/r/mvdan/shfmt/
[editorconfig]: https://editorconfig.org/
[examples]: https://pkg.go.dev/mvdan.cc/sh/v3/syntax#pkg-examples
[fedora]: https://packages.fedoraproject.org/pkgs/golang-mvdan-sh-3/shfmt/
[freebsd]: https://www.freshports.org/devel/shfmt
[homebrew]: https://formulae.brew.sh/formula/shfmt
[intellij-shellcript]: https://www.jetbrains.com/help/idea/shell-scripts.html
[macports]: https://ports.macports.org/port/shfmt/details/
[mdformat-shfmt]: https://github.com/hukkin/mdformat-shfmt
[mdformat]: https://github.com/executablebooks/mdformat
[micro]: https://micro-editor.github.io/
[mksh]: http://www.mirbsd.org/mksh.htm
[modd]: https://github.com/cortesi/modd
[mvdan-sh]: https://www.npmjs.com/package/mvdan-sh
[nixos]: https://github.com/NixOS/nixpkgs/blob/HEAD/pkgs/tools/text/shfmt/default.nix
[OpenSUSE]: https://build.opensuse.org/package/show/openSUSE:Factory/shfmt
[posix shell]: https://pubs.opengroup.org/onlinepubs/9699919799/utilities/V3_chap02.html
[posix-ambiguity]: https://pubs.opengroup.org/onlinepubs/9699919799/utilities/V3_chap02.html#tag_18_06_03
[pre-commit]: https://pre-commit.com
[pre-commit-shfmt]: https://github.com/scop/pre-commit-shfmt
[prettier-plugin-sh]: https://github.com/un-ts/prettier/tree/master/packages/sh
[prettier]: https://prettier.io
[scdoc]: https://sr.ht/~sircmpwn/scdoc/
[scoop]: https://github.com/ScoopInstaller/Main/blob/HEAD/bucket/shfmt.json
[sh-checker]: https://github.com/luizm/action-sh-checker
[shell-format]: https://marketplace.visualstudio.com/items?itemName=foxundermoon.shell-format
[shfmt.el]: https://github.com/purcell/emacs-shfmt/
[snapcraft]: https://snapcraft.io/shfmt
[sublime-pretty-shell]: https://github.com/aerobounce/Sublime-Pretty-Shell
[trunk]: https://trunk.io/check
[vim-shfmt]: https://github.com/z0mbix/vim-shfmt
[void]: https://github.com/void-linux/void-packages/blob/HEAD/srcpkgs/shfmt/template
[vscode-shfmt]: https://marketplace.visualstudio.com/items?itemName=mkhl.shfmt
[webi]: https://webinstall.dev/shfmt/
