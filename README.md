# sh

[![GoDoc](https://godoc.org/github.com/mvdan/sh?status.svg)](https://godoc.org/github.com/mvdan/sh)
[![Build Status](https://travis-ci.org/mvdan/sh.svg?branch=master)](https://travis-ci.org/mvdan/sh)

A shell parser and formatter. Supports POSIX Shell and Bash.

Still in active development, so the API might change a little.

Have a look at [canonical.sh](testdata/canonical.sh) for a quick look at
what style it enforces.

### shfmt

`shfmt` formats shell programs. It uses tabs for indentation and blanks
for alignment.

	go get github.com/mvdan/sh/cmd/shfmt

You can feed it standard input, any number of files or any number of
directories to recurse into. When recursing, it will operate on .sh and
.bash files and ignore files starting with a period.

	shfmt -l -w script.sh dir

### FAQ

* Why tabs?

The shell language is geared towards them. See `<<-` for example, which
allows indenting heredoc bodies with tabs but not spaces. Also because
this tool was heavily influenced by `gofmt`.

* Why not make [X] configurable in the style?

Allowing changes in the style would [defeat the
purpose](https://twitter.com/davecheney/status/720410297027076096).
