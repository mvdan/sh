shfmt(1)

# NAME

shfmt - Format shell programs

# SYNOPSIS

*shfmt* [flags] [path...]

# DESCRIPTION

shfmt formats shell programs. If the only argument is a dash (*-*) or no
arguments are given, standard input will be used. If a given path is a
directory, all shell scripts found under that directory will be used.

If any EditorConfig files are found, they will be used to apply formatting
options. If any parser or printer flags are given to the tool, no EditorConfig
files will be used. A default like *-i=0* can be used for this purpose.

shfmt's default shell formatting was chosen to be consistent, common, and
predictable. Some aspects of the format can be configured via printer flags.

# OPTIONS

## Generic flags

*-version*
	Show version and exit.

*-l*
	List files whose formatting differs from shfmt's.

*-w*
	Write result to file instead of stdout.

*-d*
	Error with a diff when the formatting differs.

*-s*
	Simplify the code.

*-mn*
	Minify the code to reduce its size (implies -s).

## Parser flags

*-ln* <str>
	Language variant to parse (bash/posix/mksh/bats, default "bash").

*-p*
	Shorthand for -ln=posix.

*-filename* str
	Provide a name for the standard input file.

## Printer flags

*-i* <uint>
	Indent: 0 for tabs (default), >0 for number of spaces.

*-bn*
	Binary ops like && and | may start a line.

*-ci*
	Switch cases will be indented.

*-sr*
	Redirect operators will be followed by a space.

*-kp*
	Keep column alignment paddings.

*-fn*
	Function opening braces are placed on a separate line.

## Utility flags

*-f*
	Recursively find all shell files and print the paths.

*-tojson*
	Print syntax tree to stdout as a typed JSON.

# EXAMPLES

Format all the scripts under the current directory, printing which are modified:

	shfmt -l -w .

For CI, one can use a variant where formatting changes are just shown as diffs:

	shfmt -d .

The following formatting flags closely resemble Google's shell style defined in
<https://google.github.io/styleguide/shell.xml>:

	shfmt -i 2 -ci

Below is a sample EditorConfig file as defined by <https://editorconfig.org/>,
showing how to set any option:

```
[*.sh]
# like -i=4
indent_style = space
indent_size = 4

shell_variant      = posix # like -ln=posix
binary_next_line   = true  # like -bn
switch_case_indent = true  # like -ci
space_redirects    = true  # like -sr
keep_padding       = true  # like -kp
function_next_line = true  # like -fn

# Ignore the entire "third_party" directory.
[third_party/**]
ignore = true
```

shfmt can also replace *bash -n* to check shell scripts for syntax errors. It is
more exhaustive, as it parses all syntax statically and requires valid UTF-8:

```
$ echo '${foo:1 2}' | bash -n
$ echo '${foo:1 2}' | shfmt >/dev/null
1:9: not a valid arithmetic operator: 2
$ echo 'foo=(1 2)' | bash --posix -n
$ echo 'foo=(1 2)' | shfmt -p >/dev/null
1:5: arrays are a bash feature
```

# AUTHORS

Maintained by Daniel Martí <mvdan@mvdan.cc>, who is assisted by other open
source contributors. For more information and development, see
<https://github.com/mvdan/sh>.
