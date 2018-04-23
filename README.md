# sh

[![GoDoc](https://godoc.org/mvdan.cc/sh?status.svg)](https://godoc.org/mvdan.cc/sh)
[![Linux & OSX build](https://travis-ci.org/mvdan/sh.svg?branch=master)](https://travis-ci.org/mvdan/sh)
[![Windows build](https://ci.appveyor.com/api/projects/status/rxxs08v65aj2fqof?svg=true)](https://ci.appveyor.com/project/mvdan/sh)
[![Coverage Status](https://coveralls.io/repos/github/mvdan/sh/badge.svg?branch=master)](https://coveralls.io/github/mvdan/sh)

A shell parser, formatter and interpreter. Supports [POSIX Shell], [Bash] and
[mksh]. Requires Go 1.9 or later.

### shfmt

	go get -u mvdan.cc/sh/cmd/shfmt

`shfmt` formats shell programs. It can use tabs or any number of spaces to
indent. See [canonical.sh](syntax/canonical.sh) for a quick look at its default
style.

You can feed it standard input, any number of files or any number of directories
to recurse into. When recursing, it will operate on `.sh` and `.bash` files and
ignore files starting with a period. It will also operate on files with no
extension and a shell shebang.

	shfmt -l -w script.sh

Use `-i N` to indent with a number of spaces instead of tabs. There are other
formatting options - see `shfmt -h`. For example, to get the formatting
appropriate for [Google's Style][google-style] guide, use `shfmt -i 2 -ci`.

Packages are available for [Arch], [CRUX], [Homebrew], [NixOS] and [Void].

#### Advantages over `bash -n`

`bash -n` can be useful to check for syntax errors in shell scripts. However,
`shfmt >/dev/null` can do a better job as it checks for invalid UTF-8 and does
all parsing statically, including checking POSIX Shell validity:

```
 $ echo '${foo:1 2}' | bash -n
 $ echo '${foo:1 2}' | shfmt
1:9: not a valid arithmetic operator: 2
 $ echo 'foo=(1 2)' | bash --posix -n
 $ echo 'foo=(1 2)' | shfmt -p
1:5: arrays are a bash feature
```

### gosh

	go get -u mvdan.cc/sh/cmd/gosh

Experimental shell that uses `interp`. Work in progress, so don't expect
stability just yet.

### Fuzzing

This project makes use of [go-fuzz] to find crashes and hangs in both the parser
and the printer. To get started, run:

	git checkout fuzz
	./fuzz

### Caveats

* When indexing Bash associative arrays, always use quotes. The static parser
  will otherwise have to assume that the index is an arithmetic expression.

```
$ echo '${array[spaced string]}' | shfmt
1:16: not a valid arithmetic operator: string
$ echo '${array[dash-string]}' | shfmt
${array[dash - string]}
```

* `$((` and `((` ambiguity is not suported. Backtracking would complicate the
  parser and make streaming support via `io.Reader` impossible. The POSIX spec
  recommends to [space the operands][posix-ambiguity] if `$( (` is meant.

```
$ echo '$((foo); (bar))' | shfmt
1:1: reached ) without matching $(( with ))
```

* Some builtins like `export` and `let` are parsed as keywords. This is to allow
  statically parsing them and building their AST, as opposed to just keeping the
  arguments as a slice of arguments.

### Related projects

* [dockerised-shfmt] - A docker image of `shfmt`
* [format-shell] - Atom plugin for `shfmt`
* [micro] - Editor with a built-in plugin for `shfmt`
* [shell-format] - VS Code plugin for `shfmt`
* [vim-shfmt] - Vim plugin for `shfmt`

[arch]: https://aur.archlinux.org/packages/shfmt/
[bash]: https://www.gnu.org/software/bash/
[crux]: https://github.com/6c37/crux-ports-git/tree/3.3/shfmt
[dockerised-shfmt]: https://hub.docker.com/r/jamesmstone/shfmt/
[examples]: https://godoc.org/mvdan.cc/sh/syntax#pkg-examples
[format-shell]: https://atom.io/packages/format-shell
[go-fuzz]: https://github.com/dvyukov/go-fuzz
[google-style]: https://google.github.io/styleguide/shell.xml
[homebrew]: https://github.com/Homebrew/homebrew-core/blob/HEAD/Formula/shfmt.rb
[micro]: https://micro-editor.github.io/
[mksh]: https://www.mirbsd.org/mksh.htm
[nixos]: https://github.com/NixOS/nixpkgs/blob/HEAD/pkgs/tools/text/shfmt/default.nix
[posix shell]: http://pubs.opengroup.org/onlinepubs/9699919799/utilities/V3_chap02.html
[posix-ambiguity]: http://pubs.opengroup.org/onlinepubs/9699919799/utilities/V3_chap02.html#tag_18_06_03
[shell-format]: https://marketplace.visualstudio.com/items?itemName=foxundermoon.shell-format
[vim-shfmt]: https://github.com/z0mbix/vim-shfmt
[void]: https://github.com/voidlinux/void-packages/blob/HEAD/srcpkgs/shfmt/template
