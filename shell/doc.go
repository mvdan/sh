// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

// Package shell provides one-call helpers for common shell-like tasks,
// following Bash semantics:
//
//   - [Split] and [Join] split a command line into arguments and quote arguments
//     back into one, like Python's shlex or the shellquote and shell-words libraries.
//   - [Fields] and [Expand] expand parameters like $HOME, tildes, and braces in strings.
//   - [Match] and [Glob] match names and glob with "**" using shell patterns,
//     as alternatives to [path.Match], [filepath.Glob], and [fs.Glob].
//
// The [syntax], [expand], and [pattern] packages underneath offer more options.
//
// Please note that this package uses Bash syntax. As such, path names on
// Windows need to use double backslashes or be within single quotes when given
// to functions like Fields. For example:
//
//	shell.Fields("echo /foo/bar")     // on Unix-like
//	shell.Fields("echo C:\\foo\\bar") // on Windows
//	shell.Fields("echo 'C:\foo\bar'") // on Windows, with quotes
package shell
