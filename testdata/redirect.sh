foo >file
bar > /dev/etc
foo <file >file2

foo >file arg

foo >>file
foo >&2

{ foo; } >file
{ foo; } >>file
{ foo; } >&1
{ foo; } <file
