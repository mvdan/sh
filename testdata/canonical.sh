#!/bin/sh

# separate comment

! foo bar >a & # inline comment

{ foo; }
{
	foo
}

foo() { bar; }

if foo; then bar; fi
if foo; then
	bar
fi

for foo in a b c; do bar; done
for foo in a b c; do
	bar
done

case $foo in
	a) A ;;
	b)
		B
		;;
esac

foo | bar
foo \
	&& $(bar) \
	&& `more`

(foo)
(
	foo
)

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
