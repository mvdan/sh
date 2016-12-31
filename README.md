# sh

[![GoDoc](https://godoc.org/github.com/mvdan/sh?status.svg)](https://godoc.org/github.com/mvdan/sh)
[![Build Status](https://travis-ci.org/mvdan/sh.svg?branch=master)](https://travis-ci.org/mvdan/sh)

A shell parser and formatter. Supports [POSIX Shell][posix-spec] and
[Bash][bash-site].

For a quick overview, see the [examples][]. Requires Go 1.6 or later.

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

### Fuzzing

This project makes use of [go-fuzz][] to find crashes and hangs in both
the parser and the printer. To get started, run:

	git checkout fuzz
	./fuzz

### Caveats

Supporting some of these could be possible, but they would involve major
drawbacks explained below.

* Associative arrays. Cannot be parsed statically as that depends on
  whether `array` was defined via `declare -A`.

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

* [format-shell][] - Atom plugin for `shfmt`

[posix-spec]: http://pubs.opengroup.org/onlinepubs/9699919799/utilities/V3_chap02.html
[bash-site]: https://www.gnu.org/software/bash/
[examples]: https://godoc.org/github.com/mvdan/sh/syntax#pkg-examples
[go-fuzz]: https://github.com/dvyukov/go-fuzz
[posix-ambiguity]: http://pubs.opengroup.org/onlinepubs/9699919799/utilities/V3_chap02.html#tag_18_06_03
[format-shell]: https://atom.io/packages/format-shell
