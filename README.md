# sh

[![GoDoc](https://godoc.org/github.com/mvdan/xurls?status.svg)](https://godoc.org/github.com/mvdan/xurls)
[![Build Status](https://travis-ci.org/mvdan/sh.svg?branch=master)](https://travis-ci.org/mvdan/sh)

A shell parser and formatter. Supports POSIX Shell and Bash.

Still in active development, so the API might change a little.

#### shfmt

`shfmt` formats shell programs. It uses tabs for indentation and blanks
for alignment.

	go get github.com/mvdan/sh/cmd/shfmt

You can feed it standard input, any number of files or any number of
directories to recurse into. When recursing, it will operate on .sh and
.bash files and ignore files starting with a period.

	shfmt -l -w script.sh dir
