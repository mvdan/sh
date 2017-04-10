# sh

[![GoDoc](https://godoc.org/github.com/mvdan/sh?status.svg)](https://godoc.org/github.com/mvdan/sh)
[![Build Status](https://travis-ci.org/mvdan/sh.svg?branch=master)](https://travis-ci.org/mvdan/sh)
[![Coverage Status](https://coveralls.io/repos/github/mvdan/sh/badge.svg?branch=master)](https://coveralls.io/github/mvdan/sh)

A shell parser and formatter. Supports [POSIX Shell] and [Bash].

For a quick overview, see the [examples]. Requires Go 1.7 or later.

### shfmt

	go get -u github.com/mvdan/sh/cmd/shfmt

`shfmt` formats shell programs. It can use tabs or any number of spaces
to indent. See [canonical.sh](syntax/canonical.sh) for a quick look at
its style.

You can feed it standard input, any number of files or any number of
directories to recurse into. When recursing, it will operate on `.sh`
and `.bash` files and ignore files starting with a period. It will also
operate on files with no extension and a shell shebang.

	shfmt -l -w script.sh

Use `-i N` to indent with a number of spaces instead of tabs.

Packages are available for [Arch], [Homebrew], [NixOS] and [Void].

#### Advantages over `bash -n`

`bash -n` can be useful to check for syntax errors in shell scripts.
However, `shfmt >/dev/null` can do a better job as it checks for invalid
UTF-8 and does all parsing statically, including checking POSIX Shell
validity:

```
 $ echo '${foo:1 2}' | bash -n
 $ echo '${foo:1 2}' | shfmt
1:9: not a valid arithmetic operator: 2
 $ echo 'foo=(1 2)' | bash --posix -n
 $ echo 'foo=(1 2)' | shfmt -p
1:5: arrays are a bash feature
```

### gosh

	go get -u github.com/mvdan/sh/cmd/gosh

Experimental shell executable using `interp`. Work in progress and
unstable - the name and package path might change in the future too.

### Fuzzing

This project makes use of [go-fuzz] to find crashes and hangs in both
the parser and the printer. To get started, run:

	git checkout fuzz
	./fuzz

### Caveats

Supporting some of these could be possible, but they would involve major
drawbacks explained below.

* Bash associative arrays. Cannot be parsed statically as that depends
  on whether `array` was defined via `declare -A`.

```
 $ echo '${array[spaced string]}' | shfmt
1:16: not a valid arithmetic operator: string
```

* `$((` and `((` ambiguity. This means backtracking, which would greatly
  complicate the parser. In practice, the POSIX spec recommends to
  [space the operands][posix-ambiguity] if `$( (` is meant.

```
 $ echo '$((foo); (bar))' | shfmt
1:1: reached ) without matching $(( with ))
```

### Related projects

* [format-shell] - Atom plugin for `shfmt`
* [shell-format] - VS Code plugin for `shfmt`

[posix shell]: http://pubs.opengroup.org/onlinepubs/9699919799/utilities/V3_chap02.html
[bash]: https://www.gnu.org/software/bash/
[examples]: https://godoc.org/github.com/mvdan/sh/syntax#pkg-examples
[arch]: https://aur.archlinux.org/packages/shfmt/
[homebrew]: https://github.com/Homebrew/homebrew-core/blob/HEAD/Formula/shfmt.rb
[nixos]: https://github.com/NixOS/nixpkgs/blob/HEAD/pkgs/tools/text/shfmt/default.nix
[void]: https://github.com/voidlinux/void-packages/blob/HEAD/srcpkgs/shfmt/template
[go-fuzz]: https://github.com/dvyukov/go-fuzz
[posix-ambiguity]: http://pubs.opengroup.org/onlinepubs/9699919799/utilities/V3_chap02.html#tag_18_06_03
[format-shell]: https://atom.io/packages/format-shell
[shell-format]: https://marketplace.visualstudio.com/items?itemName=foxundermoon.shell-format
