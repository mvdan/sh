#!/bin/bash

# separate comment

! foo bar >a &

foo() { bar; }

{
	export var1="some long value" # var1 comment
	export var2=short             # var2 comment
}

if foo; then bar; fi

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
	&& (bar) \
	&& (more)

foo 2>&1
foo <<EOF
bar
EOF

((3 + 4))