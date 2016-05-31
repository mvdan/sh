! foo bar >a &
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

if a; then A; elif b; then B; else C; fi
if a; then
	A
elif b; then
	B
else
	C
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
a) A;;
b) B;;
esac
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

$(foo bar)
`foo bar`

foo 2>&1
foo <<EOF
bar
EOF

function foo { bar; }
foo <<<"bar"
foo <(bar)

$((3 + 4))
let a=1+2 b=(3 + 4)
