# sh

[![GoDoc](https://godoc.org/github.com/mvdan/sh?status.svg)](https://godoc.org/github.com/mvdan/sh)
[![Build Status](https://travis-ci.org/mvdan/sh.svg?branch=master)](https://travis-ci.org/mvdan/sh)

A shell parser and formatter. Supports POSIX Shell and Bash.

Still in active development, so the API might change a little.

### shfmt

`shfmt` formats shell programs. It can use tabs or any number of spaces
to indent. See [canonical.sh](testdata/canonical.sh) for a quick look at
its style.

	go get github.com/mvdan/sh/cmd/shfmt

You can feed it standard input, any number of files or any number of
directories to recurse into. When recursing, it will operate on `.sh`
and `.bash` files and ignore files starting with a period. It will also
operate on files with no extension and a shell shebang.

	shfmt -l -w script.sh

Use `-i N` to indent with a number of spaces instead of tabs.
