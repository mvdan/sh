#!/bin/sh

# separate comment

! foo bar >a & # inline comment
'foo bar' "foo bar"

{ foo; }
{
	foo
}

foo() { bar; }
foo() {
	bar
}

if foo; then bar; fi
if foo; then
	bar
fi

while foo; do bar; done
while foo; do
	bar
done

for foo in a b c; do bar; done
for foo in a b c; do
	bar
done

case $foo in
a)
	A
	;;
esac

$foo ${foo} ${#foo} ${foo:-bar}

foo | bar
foo && bar

(foo)
(
	foo
)

$(foo bar) `foo bar`

some really long line starts here \
	continues here \
	and ends here

foo 2>&1
foo <<EOF
bar
EOF

$((3 + 4))

# bash-only
function foo() { bar; }
foo <<<"bar"
foo <(bar)
let a=1+2 b=(3 + 4)
