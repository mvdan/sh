// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package interp

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"mvdan.cc/sh/syntax"
)

func BenchmarkRun(b *testing.B) {
	src := `
echo a b c d
echo ./$foo/etc $(echo foo bar)
foo="bar"
x=y :
fn() {
	local a=b
	for i in 1 2 3; do
		echo $i | cat
	done
}
[[ $foo == bar ]] && fn
echo a{b,c}d *.go
let i=(2 + 3)
`
	file, err := syntax.NewParser().Parse(strings.NewReader(src), "")
	if err != nil {
		b.Fatal(err)
	}
	r := Runner{
		Stdout: ioutil.Discard,
		Stderr: ioutil.Discard,
	}
	for i := 0; i < b.N; i++ {
		r.Reset()
		if err := r.Run(file); err != nil {
			b.Fatal(err)
		}
	}
}

var hasBash44 bool

func TestMain(m *testing.M) {
	os.Setenv("LANGUAGE", "en_US.UTF8")
	os.Setenv("LC_ALL", "en_US.UTF8")
	os.Unsetenv("CDPATH")
	hasBash44 = checkBash()
	os.Setenv("INTERP_GLOBAL", "value")
	exit := m.Run()
	for _, s := range []string{"a", "b", "c", "d", "foo", "bar"} {
		os.Unsetenv(s)
	}
	os.Exit(exit)
}

func checkBash() bool {
	out, err := exec.Command("bash", "-c", "echo -n $BASH_VERSION").Output()
	if err != nil {
		return false
	}
	return strings.HasPrefix(string(out), "4.4")
}

var fileCases = []struct {
	in, want string
}{
	// no-op programs
	{"", ""},
	{"true", ""},
	{":", ""},
	{"exit", ""},
	{"exit 0", ""},
	{"{ :; }", ""},
	{"(:)", ""},

	// exit codes
	{"exit 1", "exit status 1"},
	{"exit -1", "exit status 255"},
	{"exit 300", "exit status 44"},
	{"false", "exit status 1"},
	{"false foo", "exit status 1"},
	{"! false", ""},
	{"true foo", ""},
	{": foo", ""},
	{"! true", "exit status 1"},
	{"false; true", ""},
	{"false; exit", "exit status 1"},
	{"exit; echo foo", ""},
	{"exit 0; echo foo", ""},
	{"printf", "usage: printf format [arguments]\nexit status 2 #JUSTERR"},
	{"break", "break is only useful in a loop #JUSTERR"},
	{"continue", "continue is only useful in a loop #JUSTERR"},
	{"cd a b", "usage: cd [dir]\nexit status 2 #JUSTERR"},
	{"shift a", "usage: shift [n]\nexit status 2 #JUSTERR"},
	{
		"shouldnotexist",
		"\"shouldnotexist\": executable file not found in $PATH\nexit status 127 #JUSTERR",
	},
	{
		"for i in 1; do continue a; done",
		"usage: continue [n]\nexit status 2 #JUSTERR",
	},
	{
		"for i in 1; do break a; done",
		"usage: break [n]\nexit status 2 #JUSTERR",
	},

	// we don't need to follow bash error strings
	{"exit a", "invalid exit code: \"a\"\nexit status 2 #JUSTERR"},
	{"exit 1 2", "exit cannot take multiple arguments\nexit status 1 #JUSTERR"},

	// echo
	{"echo", "\n"},
	{"echo a b c", "a b c\n"},
	{"echo -n foo", "foo"},
	{`echo -e '\t'`, "\t\n"},
	{`echo -E '\t'`, "\\t\n"},
	{"echo -x foo", "-x foo\n"},
	{"echo -e -x -e foo", "-x -e foo\n"},

	// printf
	{"printf foo", "foo"},
	{"printf %%", "%"},
	{"printf %", "missing format char\nexit status 1 #JUSTERR"},
	{"printf %; echo foo", "missing format char\nfoo\n #IGNORE"},
	{"printf %1", "missing format char\nexit status 1 #JUSTERR"},
	{"printf %+", "missing format char\nexit status 1 #JUSTERR"},
	{"printf %B foo", "invalid format char: B\nexit status 1 #JUSTERR"},
	{"printf %12-s foo", "invalid format char: -\nexit status 1 #JUSTERR"},
	{"printf ' %s \n' bar", " bar \n"},
	{"printf '\\A'", "\\A"},
	{"printf %s foo", "foo"},
	{"printf %s", ""},
	{"printf %d,%i 3 4", "3,4"},
	{"printf %d", "0"},
	{"printf %d,%d 010 0x10", "8,16"},
	{"printf %i,%u -3 -3", "-3,18446744073709551613"},
	{"printf %o -3", "1777777777777777777775"},
	{"printf %x -3", "fffffffffffffffd"},
	{"printf %c,%c,%c foo àa", "f,\xc3,\x00"}, // TODO: use a rune?
	{"printf %3s a", "  a"},
	{"printf %3i 1", "  1"},
	{"printf %+i%+d 1 -3", "+1-3"},
	{"printf %-5x 10", "a    "},
	{"printf %02x 1", "01"},
	{"printf 'a% 5s' a", "a    a"},
	{"printf 'nofmt' 1 2 3", "nofmt"},
	{"printf '%d_' 1 2 3", "1_2_3_"},
	{"printf '%02d %02d\n' 1 2 3", "01 02\n03 00\n"},

	// words and quotes
	{"echo  foo ", "foo\n"},
	{"echo ' foo '", " foo \n"},
	{`echo " foo "`, " foo \n"},
	{`echo a'b'c"d"e`, "abcde\n"},
	{`a=" b c "; echo $a`, "b c\n"},
	{`a=" b c "; echo "$a"`, " b c \n"},
	{`echo "$(echo ' b c ')"`, " b c \n"},
	{"echo ''", "\n"},
	{`$(echo)`, ""},
	{`echo -n '\\'`, `\\`},
	{`echo -n "\\"`, `\`},
	{`set -- a b c; x="$@"; echo "$x"`, "a b c\n"},
	{`set -- b c; echo a"$@"d`, "ab cd\n"},

	// dollar quotes
	{`echo $'foo\nbar'`, "foo\nbar\n"},
	{`echo $'\r\t\\'`, "\r\t\\\n"},
	{`echo $"foo\nbar"`, "foo\\nbar\n"},
	{`echo $'%s'`, "%s\n"},
	{`a=$'\r\t\\'; echo "$a"`, "\r\t\\\n"},
	{`a=$"foo\nbar"; echo "$a"`, "foo\\nbar\n"},

	// escaped chars
	{"echo a\\b", "ab\n"},
	{"echo a\\ b", "a b\n"},
	{"echo \\$a", "$a\n"},
	{"echo \"a\\b\"", "a\\b\n"},
	{"echo 'a\\b'", "a\\b\n"},
	{"echo \"a\\\nb\"", "ab\n"},
	{"echo 'a\\\nb'", "a\\\nb\n"},

	// vars
	{"foo=bar; echo $foo", "bar\n"},
	{"foo=bar foo=etc; echo $foo", "etc\n"},
	{"foo=bar; foo=etc; echo $foo", "etc\n"},
	{"foo=bar; foo=; echo $foo", "\n"},
	{"unset foo; echo $foo", "\n"},
	{"foo=bar; unset foo; echo $foo", "\n"},
	{"echo $INTERP_GLOBAL", "value\n"},
	{"INTERP_GLOBAL=; echo $INTERP_GLOBAL", "\n"},
	{"unset INTERP_GLOBAL; echo $INTERP_GLOBAL", "\n"},
	{"foo=bar; foo=x true; echo $foo", "bar\n"},
	{"foo=bar; foo=x true; echo $foo", "bar\n"},
	{"foo=bar; env | grep '^foo='", "exit status 1"},
	{"foo=bar env | grep '^foo='", "foo=bar\n"},
	{"foo=a foo=b env | grep '^foo='", "foo=b\n"},
	{"env | grep '^INTERP_GLOBAL='", "INTERP_GLOBAL=value\n"},
	{"a=b; a+=c x+=y; echo $a $x", "bc y\n"},
	{`a=" x  y"; b=$a c="$a"; echo $b; echo $c`, "x y\nx y\n"},
	{`a=" x  y"; b=$a c="$a"; echo "$b"; echo "$c"`, " x  y\n x  y\n"},

	// special vars
	{"echo $?; false; echo $?", "0\n1\n"},
	{"for i in 1 2; do\necho $LINENO\necho $LINENO\ndone", "2\n3\n2\n3\n"},
	{"[[ -n $$ && $$ -gt 0 ]]", ""},
	{"[[ -n $PPID && $PPID -gt 0 ]]", ""},
	{"[[ $$ -eq $PPID ]]", "exit status 1"},

	// var manipulation
	{"echo ${#a} ${#a[@]}", "0 0\n"},
	{"a=bar; echo ${#a} ${#a[@]}", "3 1\n"},
	{"a=世界; echo ${#a}", "2\n"},
	{"a=(a bcd); echo ${#a} ${#a[@]} ${#a[*]} ${#a[1]}", "1 2 2 3\n"},
	{"set -- a bc; echo ${#@} ${#*} $#", "2 2 2\n"},
	{
		"echo ${!a}; a=b; echo ${!a}; b=c; echo ${!a}",
		"\n\nc\n",
	},
	{
		"a=foo; echo ${a:1}; echo ${a: -1}; echo ${a: -10}; echo ${a:5}",
		"oo\no\n\n\n",
	},
	{
		"a=foo; echo ${a::2}; echo ${a::-1}; echo ${a: -10}; echo ${a::5}",
		"fo\nfo\n\nfoo\n",
	},
	{
		"a=abc; echo ${a:1:1}",
		"b\n",
	},
	{
		"a=foo; echo ${a/no/x} ${a/o/i} ${a//o/i} ${a/fo/}",
		"foo fio fii o\n",
	},
	{
		"a=foo; echo ${a/*/xx} ${a//?/na} ${a/o*}",
		"xx nanana f\n",
	},
	{
		"a=12345; echo ${a//[42]} ${a//[^42]} ${a//[!42]}",
		"135 24 24\n",
	},
	{"a=0123456789; echo ${a//[1-35-8]}", "049\n"},
	{"a=]abc]; echo ${a//[]b]}", "ac\n"},
	{"a=-abc-; echo ${a//[-b]}", "ac\n"},
	{`a='x\y'; echo ${a//\\}`, "xy\n"},
	{"a=']'; echo ${a//[}", "]\n"},
	{"a=']'; echo ${a//[]}", "]\n"},
	{"a=']'; echo ${a//[]]}", "\n"},
	{"a='['; echo ${a//[[]}", "\n"},
	{"a=']'; echo ${a//[xy}", "]\n"},
	{"a='abc123'; echo ${a//[[:digit:]]}", "abc\n"},
	{"a='[[:wrong:]]'; echo ${a//[[:wrong:]]}", "[[:wrong:]]\n"},
	{"a='[[:wrong:]]'; echo ${a//[[:}", "[[:wrong:]]\n"},
	{"a='abcx1y'; echo ${a//x[[:digit:]]y}", "abc\n"},
	{`a=xyz; echo "${a/y/a  b}"`, "xa  bz\n"},
	{"a='foo/bar'; echo ${a//o*a/}", "fr\n"},
	{
		"echo ${a:-b}; echo $a; a=; echo ${a:-b}; a=c; echo ${a:-b}",
		"b\n\nb\nc\n",
	},
	{
		"echo ${a-b}; echo $a; a=; echo ${a-b}; a=c; echo ${a-b}",
		"b\n\n\nc\n",
	},
	{
		"echo ${a:=b}; echo $a; a=; echo ${a:=b}; a=c; echo ${a:=b}",
		"b\nb\nb\nc\n",
	},
	{
		"echo ${a=b}; echo $a; a=; echo ${a=b}; a=c; echo ${a=b}",
		"b\nb\n\nc\n",
	},
	{
		"echo ${a:+b}; echo $a; a=; echo ${a:+b}; a=c; echo ${a:+b}",
		"\n\n\nb\n",
	},
	{
		"echo ${a+b}; echo $a; a=; echo ${a+b}; a=c; echo ${a+b}",
		"\n\nb\nb\n",
	},
	{
		"a=b; echo ${a:?err1}; a=; echo ${a:?err2}; unset a; echo ${a:?err3}",
		"b\nerr2\nexit status 1 #JUSTERR",
	},
	{
		"a=b; echo ${a?err1}; a=; echo ${a?err2}; unset a; echo ${a?err3}",
		"b\n\nerr3\nexit status 1 #JUSTERR",
	},
	{
		"echo ${a:?%s}",
		"%s\nexit status 1 #JUSTERR",
	},
	{
		"x=aaabccc; echo ${x#*a}; echo ${x##*a}",
		"aabccc\nbccc\n",
	},
	{
		"x=(__a _b c_); echo ${x[@]#_}",
		"_a b c_\n",
	},
	{
		"x=(a__ b_ _c); echo ${x[@]%%_}",
		"a_ b _c\n",
	},
	{
		"x=aaabccc; echo ${x%c*}; echo ${x%%c*}",
		"aaabcc\naaab\n",
	},
	{
		"x=aaabccc; echo ${x%%[bc}",
		"aaabccc\n",
	},
	{
		"a='àÉñ bAr'; echo ${a^}; echo ${a^^}",
		"ÀÉñ bAr\nÀÉÑ BAR\n",
	},
	{
		"a='àÉñ bAr'; echo ${a,}; echo ${a,,}",
		"àÉñ bAr\nàéñ bar\n",
	},
	{
		"a='àÉñ bAr'; echo ${a^?}; echo ${a^^[br]}",
		"ÀÉñ bAr\nàÉñ BAR\n",
	},
	{
		"a='àÉñ bAr'; echo ${a,?}; echo ${a,,[br]}",
		"àÉñ bAr\nàÉñ bAr\n",
	},
	{
		"a=(àÉñ bAr); echo ${a[@]^}; echo ${a[*],,}",
		"ÀÉñ BAr\nàéñ bar\n",
	},
	{
		"INTERP_X_1=a INTERP_X_2=b; echo ${!INTERP_X_*}",
		"INTERP_X_1 INTERP_X_2\n",
	},
	{
		"INTERP_X_2=b INTERP_X_1=a; echo ${!INTERP_*}",
		"INTERP_GLOBAL INTERP_X_1 INTERP_X_2\n",
	},
	{
		`a='b  c'; eval "echo -n ${a} ${a@Q}"`,
		`b c b  c`,
	},
	{
		`a='"\n'; printf "%s %s" "${a}" "${a@E}"`,
		"\"\\n \"\n",
	},

	// if
	{
		"if true; then echo foo; fi",
		"foo\n",
	},
	{
		"if false; then echo foo; fi",
		"",
	},
	{
		"if false; then echo foo; fi",
		"",
	},
	{
		"if true; then echo foo; else echo bar; fi",
		"foo\n",
	},
	{
		"if false; then echo foo; else echo bar; fi",
		"bar\n",
	},
	{
		"if true; then false; fi",
		"exit status 1",
	},
	{
		"if false; then :; else false; fi",
		"exit status 1",
	},
	{
		"if false; then :; elif true; then echo foo; fi",
		"foo\n",
	},
	{
		"if false; then :; elif false; then :; elif true; then echo foo; fi",
		"foo\n",
	},
	{
		"if false; then :; elif false; then :; else echo foo; fi",
		"foo\n",
	},

	// while
	{
		"while false; do echo foo; done",
		"",
	},
	{
		"while true; do exit 1; done",
		"exit status 1",
	},
	{
		"while true; do break; done",
		"",
	},
	{
		"while true; do while true; do break 2; done; done",
		"",
	},

	// until
	{
		"until true; do echo foo; done",
		"",
	},
	{
		"until false; do exit 1; done",
		"exit status 1",
	},
	{
		"until false; do break; done",
		"",
	},

	// for
	{
		"for i in 1 2 3; do echo $i; done",
		"1\n2\n3\n",
	},
	{
		"for i in 1 2 3; do echo $i; exit; done",
		"1\n",
	},
	{
		"for i in 1 2 3; do echo $i; false; done",
		"1\n2\n3\nexit status 1",
	},
	{
		"for i in 1 2 3; do echo $i; break; done",
		"1\n",
	},
	{
		"for i in 1 2 3; do echo $i; continue; echo foo; done",
		"1\n2\n3\n",
	},
	{
		"for i in 1 2; do for j in a b; do echo $i $j; continue 2; done; done",
		"1 a\n2 a\n",
	},
	{
		"for ((i=0; i<3; i++)); do echo $i; done",
		"0\n1\n2\n",
	},
	{
		"for ((i=5; i>0; i--)); do echo $i; break; done",
		"5\n",
	},
	{
		"for i in 1 2; do for j in a b; do echo $i $j; done; break; done",
		"1 a\n1 b\n",
	},
	{
		"for i in 1 2 3; do :; done; echo $i",
		"3\n",
	},
	{
		"for ((i=0; i<3; i++)); do :; done; echo $i",
		"3\n",
	},

	// block
	{
		"{ echo foo; }",
		"foo\n",
	},
	{
		"{ false; }",
		"exit status 1",
	},

	// subshell
	{
		"(echo foo)",
		"foo\n",
	},
	{
		"(false)",
		"exit status 1",
	},
	{
		"(exit 1)",
		"exit status 1",
	},
	{
		"(foo=bar; echo $foo); echo $foo",
		"bar\n\n",
	},
	{
		"(echo() { printf 'bar\n'; }; echo); echo",
		"bar\n\n",
	},
	{
		"unset INTERP_GLOBAL & echo $INTERP_GLOBAL",
		"value\n",
	},
	{
		"(fn() { :; }) & pwd >/dev/null",
		"",
	},

	// cd/pwd
	{"[[ fo~ == 'fo~' ]]", ""},
	{`[[ 'ab\c' == *\\* ]]`, ""},
	{`[[ foo/bar == foo* ]]`, ""},
	{"[[ a == [ab ]]", "exit status 1"},
	{
		`[[ ~ == "$HOME" ]] && [[ ~/foo == "$HOME/foo" ]]`,
		"",
	},
	{
		"[[ ~noexist == '~noexist' ]]",
		"",
	},
	{
		"[[ ~root == '~root' ]]",
		"exit status 1",
	},
	{
		`w="$HOME"; cd; [[ $PWD == "$w" ]]`,
		"",
	},
	{
		`HOME=/foo; echo $HOME`,
		"/foo\n",
	},
	{
		"cd noexist",
		"exit status 1 #JUSTERR",
	},
	{
		"mkdir -p a/b && cd a && cd b && cd ../..",
		"",
	},
	{
		"touch a && cd a",
		"exit status 1 #JUSTERR",
	},
	{
		`[[ $PWD == "$(pwd)" ]]`,
		"",
	},
	{
		"PWD=changed; [[ $PWD == changed ]]",
		"",
	},
	{
		"PWD=changed; mkdir a; cd a; [[ $PWD == changed ]]",
		"exit status 1",
	},
	{
		`mkdir %s; old="$PWD"; cd %s; [[ $old == "$PWD" ]]`,
		"exit status 1",
	},
	{
		`old="$PWD"; mkdir a; cd a; cd ..; [[ $old == "$PWD" ]]`,
		"",
	},
	{
		`[[ $PWD == "$OLDPWD" ]]`,
		"exit status 1",
	},
	{
		`old="$PWD"; mkdir a; cd a; [[ $old == "$OLDPWD" ]]`,
		"",
	},
	{
		`mkdir a; ln -s a b; [[ $(cd a && pwd) == "$(cd b && pwd)" ]]; echo $?`,
		"1\n",
	},
	{
		`mkdir a; chmod 0000 a; cd a`,
		"exit status 1 #JUSTERR",
	},
	{
		`mkdir a; chmod 0222 a; cd a`,
		"exit status 1 #JUSTERR",
	},
	{
		`mkdir a; chmod 0444 a; cd a`,
		"exit status 1 #JUSTERR",
	},
	{
		`mkdir a; chmod 0100 a; cd a`,
		"",
	},
	{
		`mkdir a; chmod 0010 a; cd a`,
		"exit status 1 #JUSTERR",
	},
	{
		`mkdir a; chmod 0001 a; cd a`,
		"exit status 1 #JUSTERR",
	},

	// dirs/pushd/popd
	{"set -- $(dirs); echo $# ${#DIRSTACK[@]}", "1 1\n"},
	{"pushd", "pushd: no other directory\nexit status 1 #JUSTERR"},
	{"pushd -n", ""},
	{"pushd foo bar", "pushd: too many arguments\nexit status 2 #JUSTERR"},
	{"pushd does-not-exist; set -- $(dirs); echo $#", "1\n #IGNORE"},
	{"mkdir a; pushd a >/dev/null; set -- $(dirs); echo $#", "2\n"},
	{"mkdir a; set -- $(pushd a); echo $#", "2\n"},
	{
		`mkdir a; pushd a >/dev/null; set -- $(dirs); [[ $1 == "$HOME" ]]`,
		"exit status 1",
	},
	{
		`mkdir a; pushd a >/dev/null; [[ ${DIRSTACK[0]} == "$HOME" ]]`,
		"exit status 1",
	},
	{
		`old=$(dirs); mkdir a; pushd a >/dev/null; pushd >/dev/null; set -- $(dirs); [[ $1 == "$old" ]]`,
		"",
	},
	{
		`old=$(dirs); mkdir a; pushd a >/dev/null; pushd -n >/dev/null; set -- $(dirs); [[ $1 == "$old" ]]`,
		"exit status 1",
	},
	{
		"mkdir a; pushd a >/dev/null; pushd >/dev/null; rmdir a; pushd",
		"exit status 1 #JUSTERR",
	},
	{
		`old=$(dirs); mkdir a; pushd -n a >/dev/null; set -- $(dirs); [[ $1 == "$old" ]]`,
		"",
	},
	{
		`old=$(dirs); mkdir a; pushd -n a >/dev/null; pushd >/dev/null; set -- $(dirs); [[ $1 == "$old" ]]`,
		"exit status 1",
	},
	{"popd", "popd: directory stack empty\nexit status 1 #JUSTERR"},
	{"popd -n", "popd: directory stack empty\nexit status 1 #JUSTERR"},
	{"popd foo", "popd: invalid argument\nexit status 2 #JUSTERR"},
	{"old=$(dirs); mkdir a; pushd a >/dev/null; set -- $(popd); echo $#", "1\n"},
	{
		`old=$(dirs); mkdir a; pushd a >/dev/null; popd >/dev/null; [[ $(dirs) == "$old" ]]`,
		"",
	},
	{"old=$(dirs); mkdir a; pushd a >/dev/null; set -- $(popd -n); echo $#", "1\n"},
	{
		`old=$(dirs); mkdir a; pushd a >/dev/null; popd -n >/dev/null; [[ $(dirs) == "$old" ]]`,
		"exit status 1",
	},
	{
		"mkdir a; pushd a >/dev/null; pushd >/dev/null; rmdir a; popd",
		"exit status 1 #JUSTERR",
	},

	// binary cmd
	{
		"true && echo foo || echo bar",
		"foo\n",
	},
	{
		"false && echo foo || echo bar",
		"bar\n",
	},

	// func
	{
		"foo() { echo bar; }; foo",
		"bar\n",
	},
	{
		"foo() { echo $1; }; foo",
		"\n",
	},
	{
		"foo() { echo $1; }; foo a b",
		"a\n",
	},
	{
		"foo() { echo $1; bar c d; echo $2; }; bar() { echo $2; }; foo a b",
		"a\nd\nb\n",
	},
	{
		`foo() { echo $#; }; foo; foo 1 2 3; foo "a b"; echo $#`,
		"0\n3\n1\n0\n",
	},
	{
		`foo() { for a in $*; do echo "$a"; done }; foo 'a  1' 'b  2'`,
		"a\n1\nb\n2\n",
	},
	{
		`foo() { for a in "$*"; do echo "$a"; done }; foo 'a  1' 'b  2'`,
		"a  1 b  2\n",
	},
	{
		`foo() { for a in "foo$*"; do echo "$a"; done }; foo 'a  1' 'b  2'`,
		"fooa  1 b  2\n",
	},
	{
		`foo() { for a in $@; do echo "$a"; done }; foo 'a  1' 'b  2'`,
		"a\n1\nb\n2\n",
	},
	{
		`foo() { for a in "$@"; do echo "$a"; done }; foo 'a  1' 'b  2'`,
		"a  1\nb  2\n",
	},

	// case
	{
		"case b in x) echo foo ;; a|b) echo bar ;; esac",
		"bar\n",
	},
	{
		"case b in x) echo foo ;; y|z) echo bar ;; esac",
		"",
	},
	{
		"case foo in bar) echo foo ;; *) echo bar ;; esac",
		"bar\n",
	},
	{
		"case foo in *o*) echo bar ;; esac",
		"bar\n",
	},
	{
		"case foo in '*') echo x ;; f*) echo y ;; esac",
		"y\n",
	},

	// exec
	{
		"bash -c 'echo foo'",
		"foo\n",
	},
	{
		"bash -c 'echo foo >&2' >/dev/null",
		"foo\n",
	},
	{
		"echo foo | bash -c 'cat >&2' >/dev/null",
		"foo\n",
	},
	{
		"bash -c 'exit 1'",
		"exit status 1",
	},
	{
		"exec >/dev/null; echo foo",
		"",
	},

	// PATH
	{
		"PATH=; bash -c 'echo foo'",
		"\"bash\": executable file not found in $PATH\nexit status 127 #JUSTERR",
	},
	{
		"echo '#!/bin/sh\necho b' >a; chmod a+x a; PATH=; a",
		"b\n",
	},
	{
		"mkdir c; cd c; echo '#!/bin/sh\necho b' >a; chmod a+x a; PATH=; a",
		"b\n",
	},
	{
		"c/a",
		"\"c/a\": executable file not found in $PATH\nexit status 127 #JUSTERR",
	},
	{
		"mkdir c; echo '#!/bin/sh\necho b' >c/a; chmod a+x c/a; c/a",
		"b\n",
	},

	// return
	{"return", "return: can only be done from a func or sourced script\nexit status 1 #JUSTERR"},
	{"f() { return; }; f", ""},
	{"f() { return 2; }; f", "exit status 2"},
	{"f() { echo foo; return; echo bar; }; f", "foo\n"},
	{"f1() { :; }; f2() { f1; return; }; f2", ""},
	{"echo 'return' >a; source a", ""},
	{"echo 'return' >a; source a; return", "return: can only be done from a func or sourced script\nexit status 1 #JUSTERR"},
	{"echo 'return 2' >a; source a", "exit status 2"},
	{"echo 'echo foo; return; echo bar' >a; source a", "foo\n"},

	// command
	{"command", ""},
	{"command -o echo", "command: invalid option -o\nexit status 2 #JUSTERR"},
	{"echo() { :; }; echo foo", ""},
	{"echo() { :; }; command echo foo", "foo\n"},
	{"bash() { :; }; bash -c 'echo foo'", ""},
	{"bash() { :; }; command bash -c 'echo foo'", "foo\n"},
	{"command -v does-not-exist", "exit status 1"},
	{"foo() { :; }; command -v foo", "foo\n"},
	{"foo() { :; }; command -v does-not-exist foo", "foo\n"},
	{"command -v echo", "echo\n"},
	{"[[ $(command -v bash) == bash ]]", "exit status 1"},

	// cmd substitution
	{
		"echo foo $(printf bar)",
		"foo bar\n",
	},
	{
		"echo foo $(echo bar)",
		"foo bar\n",
	},
	{
		"$(echo echo foo bar)",
		"foo bar\n",
	},
	{
		"for i in 1 $(echo 2 3) 4; do echo $i; done",
		"1\n2\n3\n4\n",
	},
	{
		"echo 1$(echo 2 3)4",
		"12 34\n",
	},
	{
		`mkdir d; [[ $(cd d && pwd) == "$(pwd)" ]]`,
		"exit status 1",
	},

	// pipes
	{
		"echo foo | sed 's/o/a/g'",
		"faa\n",
	},
	{
		"echo foo | false | true",
		"",
	},

	// redirects
	{
		"echo foo >&1 | sed 's/o/a/g'",
		"faa\n",
	},
	{
		"echo foo >&2 | sed 's/o/a/g'",
		"foo\n",
	},
	{
		// TODO: why does bash need a block here?
		"{ echo foo >&2; } |& sed 's/o/a/g'",
		"faa\n",
	},
	{
		"echo foo >/dev/null; echo bar",
		"bar\n",
	},
	{
		">a; wc -c <a",
		"0\n",
	},
	{
		"echo foo >a; wc -c <a",
		"4\n",
	},
	{
		"echo foo >>a; echo bar &>>a; wc -c <a",
		"8\n",
	},
	{
		"{ echo a; echo b >&2; } &>/dev/null",
		"",
	},
	{
		"sed 's/o/a/g' <<EOF\nfoo$foo\nEOF",
		"faa\n",
	},
	{
		"sed 's/o/a/g' <<'EOF'\nfoo$foo\nEOF",
		"faa$faa\n",
	},
	{
		"sed 's/o/a/g' <<EOF\n\tfoo\nEOF",
		"\tfaa\n",
	},
	{
		"sed 's/o/a/g' <<EOF\nfoo\nEOF",
		"faa\n",
	},
	{
		"sed 's/o/a/g' <<<foo$foo",
		"faa\n",
	},
	{
		"mkdir a; echo foo >a |& grep -q 'is a directory'",
		" #IGNORE",
	},
	{
		"echo foo 1>&1 | sed 's/o/a/g'",
		"faa\n",
	},
	{
		"echo foo 2>&2 |& sed 's/o/a/g'",
		"faa\n",
	},
	{
		"printf 2>&1 | sed 's/.*usage.*/foo/'",
		"foo\n",
	},
	{
		"mkdir a && cd a && echo foo >b && cd .. && cat a/b",
		"foo\n",
	},

	// background/wait
	{"wait", ""},
	{"{ true; } & wait", ""},
	{"{ exit 1; } & wait", ""},
	{
		"{ echo foo; } & wait; echo bar",
		"foo\nbar\n",
	},
	{
		"{ echo foo & wait; } & wait; echo bar",
		"foo\nbar\n",
	},
	{`mkdir d; old=$PWD; cd d & wait; [[ $old == "$PWD" ]]`, ""},

	// bash test
	{
		"[[ a ]]",
		"",
	},
	{
		"[[ '' ]]",
		"exit status 1",
	},
	{
		"[[ '' ]]; [[ a ]]",
		"",
	},
	{
		"[[ ! (a == b) ]]",
		"",
	},
	{
		"[[ a != b ]]",
		"",
	},
	{
		"[[ a && '' ]]",
		"exit status 1",
	},
	{
		"[[ a || '' ]]",
		"",
	},
	{
		"[[ a > 3 ]]",
		"",
	},
	{
		"[[ a < 3 ]]",
		"exit status 1",
	},
	{
		"[[ 3 == 03 ]]",
		"exit status 1",
	},
	{
		"[[ a -eq b ]]",
		"",
	},
	{
		"[[ 3 -eq 03 ]]",
		"",
	},
	{
		"[[ 3 -ne 4 ]]",
		"",
	},
	{
		"[[ 3 -le 4 ]]",
		"",
	},
	{
		"[[ 3 -ge 4 ]]",
		"exit status 1",
	},
	{
		"[[ 3 -ge 3 ]]",
		"",
	},
	{
		"[[ 3 -lt 4 ]]",
		"",
	},
	{
		"[[ 3 -gt 4 ]]",
		"exit status 1",
	},
	{
		"[[ 3 -gt 3 ]]",
		"exit status 1",
	},
	{
		"[[ a -nt a || a -ot a ]]",
		"exit status 1",
	},
	{
		"touch -d @1 a b; [[ a -nt b || a -ot b ]]",
		"exit status 1",
	},
	{
		"touch -d @1 a; touch -d @2 b; [[ a -nt b ]]",
		"exit status 1",
	},
	{
		"touch -d @1 a; touch -d @2 b; [[ a -ot b ]]",
		"",
	},
	{
		"[[ a -ef b ]]",
		"exit status 1",
	},
	{
		"touch a b; [[ a -ef b ]]",
		"exit status 1",
	},
	{
		"touch a; [[ a -ef a ]]",
		"",
	},
	{
		"touch a; ln a b; [[ a -ef b ]]",
		"",
	},
	{
		"touch a; ln -s a b; [[ a -ef b ]]",
		"",
	},
	{
		"[[ -z 'foo' || -n '' ]]",
		"exit status 1",
	},
	{
		"[[ -z '' && -n 'foo' ]]",
		"",
	},
	{
		"a=x b=''; [[ -v a && -v b && ! -v c ]]",
		"",
	},
	{
		"[[ abc == *b* ]]",
		"",
	},
	{
		"[[ abc != *b* ]]",
		"exit status 1",
	},
	{
		"[[ *b == '*b' ]]",
		"",
	},
	{
		"[[ ab == a. ]]",
		"exit status 1",
	},
	{
		`x='*b*'; [[ abc == $x ]]`,
		"",
	},
	{
		`x='*b*'; [[ abc == "$x" ]]`,
		"exit status 1",
	},
	{
		`[[ abc == \a\bc ]]`,
		"",
	},
	{
		"[[ abc != *b'*' ]]",
		"",
	},
	{
		"[[ a =~ b ]]",
		"exit status 1",
	},
	{
		"[[ foo =~ foo && foo =~ .* && foo =~ f.o ]]",
		"",
	},
	{
		"[[ foo =~ oo ]] && echo foo; [[ foo =~ ^oo$ ]] && echo bar || true",
		"foo\n",
	},
	{
		"[[ a =~ [ ]]",
		"exit status 2",
	},
	{
		"[[ -e a ]] && echo x; touch a; [[ -e a ]] && echo y",
		"y\n",
	},
	{
		"ln -s b a; [[ -e a ]] && echo x; touch b; [[ -e a ]] && echo y",
		"y\n",
	},
	{
		"[[ -f a ]] && echo x; touch a; [[ -f a ]] && echo y",
		"y\n",
	},
	{
		"[[ -e a ]] && echo x; mkdir a; [[ -e a ]] && echo y",
		"y\n",
	},
	{
		"[[ -d a ]] && echo x; mkdir a; [[ -d a ]] && echo y",
		"y\n",
	},
	{
		"[[ -r a ]] && echo x; touch a; [[ -r a ]] && echo y",
		"y\n",
	},
	{
		"[[ -w a ]] && echo x; touch a; [[ -w a ]] && echo y",
		"y\n",
	},
	{
		"[[ -x a ]] && echo x; touch a; chmod +x a; [[ -x a ]] && echo y",
		"y\n",
	},
	{
		"[[ -s a ]] && echo x; echo body >a; [[ -s a ]] && echo y",
		"y\n",
	},
	{
		"[[ -L a ]] && echo x; ln -s b a; [[ -L a ]] && echo y;",
		"y\n",
	},
	{
		"[[ -p a ]] && echo x; mkfifo a; [[ -p a ]] && echo y",
		"y\n",
	},
	{
		"touch a; [[ -k a ]] && echo x; chmod +t a; [[ -k a ]] && echo y",
		"y\n",
	},
	{
		"touch a; [[ -u a ]] && echo x; chmod u+s a; [[ -u a ]] && echo y",
		"y\n",
	},
	{
		"touch a; [[ -g a ]] && echo x; chmod g+s a; [[ -g a ]] && echo y",
		"y\n",
	},
	{
		"mkdir a; cd a; test -f b && echo x; touch b; test -f b && echo y",
		"y\n",
	},
	{
		"touch a; [[ -b a ]] && echo block; [[ -c a ]] && echo char; true",
		"",
	},
	{
		"[[ -e /dev/sda ]] || { echo block; exit; }; [[ -b /dev/sda ]] && echo block; [[ -c /dev/sda ]] && echo char; true",
		"block\n",
	},
	{
		"[[ -e /dev/tty ]] || { echo char; exit; }; [[ -b /dev/tty ]] && echo block; [[ -c /dev/tty ]] && echo char; true",
		"char\n",
	},
	{"[[ -t 1234 ]]", "exit status 1"}, // TODO: reliable way to test a positive?
	{"[[ -o wrong ]]", "exit status 1"},
	{"[[ -o errexit ]]", "exit status 1"},
	{"set -e; [[ -o errexit ]]", ""},
	{"[[ -o noglob ]]", "exit status 1"},
	{"set -f; [[ -o noglob ]]", ""},
	{"[[ -o allexport ]]", "exit status 1"},
	{"set -a; [[ -o allexport ]]", ""},
	{"[[ -o nounset ]]", "exit status 1"},
	{"set -u; [[ -o nounset ]]", ""},
	{"[[ -o noexec ]]", "exit status 1"},
	{"set -n; [[ -o noexec ]]", ""}, // actually does nothing, but oh well
	{"[[ -o pipefail ]]", "exit status 1"},
	{"set -o pipefail; [[ -o pipefail ]]", ""},

	// classic test
	{
		"[",
		"1:1: [: missing matching ]\nexit status 2 #JUSTERR",
	},
	{
		"[ a",
		"1:1: [: missing matching ]\nexit status 2 #JUSTERR",
	},
	{
		"[ a b c ]",
		"1:1: not a valid test operator: b\nexit status 2 #JUSTERR",
	},
	{
		"[ a -a ]",
		"1:1: -a must be followed by an expression\nexit status 2 #JUSTERR",
	},
	{"[ a ]", ""},
	{"[ -n ]", ""},
	{"[ '-n' ]", ""},
	{"[ -z ]", ""},
	{"[ ! ]", ""},
	{"[ a != b ]", ""},
	{"[ ! a '==' a ]", "exit status 1"},
	{"[ a -a 0 -gt 1 ]", "exit status 1"},
	{"[ 0 -gt 1 -o 1 -gt 0 ]", ""},
	{"[ 3 -gt 4 ]", "exit status 1"},
	{"[ 3 -lt 4 ]", ""},
	{
		"[ -e a ] && echo x; touch a; [ -e a ] && echo y",
		"y\n",
	},
	{
		"test 3 -gt 4",
		"exit status 1",
	},
	{
		"test 3 -lt 4",
		"",
	},
	{
		"test 3 -lt",
		"1:1: -lt must be followed by a word\nexit status 2 #JUSTERR",
	},
	{
		"touch -d @1 a; touch -d @2 b; [ a -nt b ]",
		"exit status 1",
	},
	{
		"touch -d @1 a; touch -d @2 b; [ a -ot b ]",
		"",
	},
	{
		"touch a; [ a -ef a ]",
		"",
	},
	{"[ 3 -eq 04 ]", "exit status 1"},
	{"[ 3 -eq 03 ]", ""},
	{"[ 3 -ne 03 ]", "exit status 1"},
	{"[ 3 -le 4 ]", ""},
	{"[ 3 -ge 4 ]", "exit status 1"},
	{
		"[ -d a ] && echo x; mkdir a; [ -d a ] && echo y",
		"y\n",
	},
	{
		"[ -r a ] && echo x; touch a; [ -r a ] && echo y",
		"y\n",
	},
	{
		"[ -w a ] && echo x; touch a; [ -w a ] && echo y",
		"y\n",
	},
	{
		"[ -x a ] && echo x; touch a; chmod +x a; [ -x a ] && echo y",
		"y\n",
	},
	{
		"[ -s a ] && echo x; echo body >a; [ -s a ] && echo y",
		"y\n",
	},
	{
		"[ -L a ] && echo x; ln -s b a; [ -L a ] && echo y;",
		"y\n",
	},
	{
		"[ -p a ] && echo x; mkfifo a; [ -p a ] && echo y",
		"y\n",
	},
	{
		"touch a; [ -k a ] && echo x; chmod +t a; [ -k a ] && echo y",
		"y\n",
	},
	{
		"touch a; [ -u a ] && echo x; chmod u+s a; [ -u a ] && echo y",
		"y\n",
	},
	{
		"touch a; [ -g a ] && echo x; chmod g+s a; [ -g a ] && echo y",
		"y\n",
	},
	{
		"touch a; [ -b a ] && echo block; [ -c a ] && echo char; true",
		"",
	},
	{"[ -t 1234 ]", "exit status 1"}, // TODO: reliable way to test a positive?
	{"[ -o wrong ]", "exit status 1"},
	{"[ -o errexit ]", "exit status 1"},
	{"set -e; [ -o errexit ]", ""},
	{"a=x b=''; [ -v a -a -v b -a ! -v c ]", ""},

	// arithm
	{
		"echo $((1 == +1))",
		"1\n",
	},
	{
		"echo $((!0))",
		"1\n",
	},
	{
		"echo $((!3))",
		"0\n",
	},
	{
		"echo $((1 + 2 - 3))",
		"0\n",
	},
	{
		"echo $((-1 * 6 / 2))",
		"-3\n",
	},
	{
		"a=2; echo $(( a + $a + c ))",
		"4\n",
	},
	{
		"a=b; b=c; c=5; echo $((a % 3))",
		"2\n",
	},
	{
		"echo $((2 > 2 || 2 < 2))",
		"0\n",
	},
	{
		"echo $((2 >= 2 && 2 <= 2))",
		"1\n",
	},
	{
		"echo $(((1 & 2) != (1 | 2)))",
		"1\n",
	},
	{
		"echo $a; echo $((a = 3 ^ 2)); echo $a",
		"\n1\n1\n",
	},
	{
		"echo $((a += 1, a *= 2, a <<= 2, a >> 1))",
		"4\n",
	},
	{
		"echo $((a -= 10, a /= 2, a >>= 1, a << 1))",
		"-6\n",
	},
	{
		"echo $((a |= 3, a &= 1, a ^= 8, a %= 5, a))",
		"4\n",
	},
	{
		"echo $((a = 3, ++a, a--))",
		"4\n",
	},
	{
		"echo $((2 ** 3)) $((1234 ** 4567))",
		"8 0\n",
	},
	{
		"echo $((1 ? 2 : 3)) $((0 ? 2 : 3))",
		"2 3\n",
	},
	{
		"((1))",
		"",
	},
	{
		"((3 == 4))",
		"exit status 1",
	},
	{
		"let i=(3+4); let i++; echo $i; let i--; echo $i",
		"8\n7\n",
	},
	{
		"let 3==4",
		"exit status 1",
	},
	{
		"a=1; let a++; echo $a",
		"2\n",
	},
	{
		"a=$((1 + 2)); echo $a",
		"3\n",
	},

	// set/shift
	{
		"echo $#; set foo bar; echo $#",
		"0\n2\n",
	},
	{
		"shift; set a b c; shift; echo $@",
		"b c\n",
	},
	{
		"shift 2; set a b c; shift 2; echo $@",
		"c\n",
	},
	{
		`echo $#; set '' ""; echo $#`,
		"0\n2\n",
	},
	{
		"set -- a b; echo $#",
		"2\n",
	},
	{
		"set -U",
		"set: invalid option: \"-U\"\nexit status 2 #JUSTERR",
	},
	{
		"set -e; false; echo foo",
		"exit status 1",
	},
	{
		"set -e; set +e; false; echo foo",
		"foo\n",
	},
	{
		"set -e; ! false; echo foo",
		"foo\n",
	},
	{
		"set -e; local; echo foo",
		"local: can only be used in a function\nexit status 1 #JUSTERR",
	},
	{
		"false | :",
		"",
	},
	{
		"set -o pipefail; false | :",
		"exit status 1",
	},
	{
		"set -o pipefail; true | false | true | :",
		"exit status 1",
	},
	{
		"set -o pipefail; set -M 2>/dev/null | false",
		"exit status 1",
	},
	{
		"set -f; touch a.x; echo *.x;",
		"*.x\n",
	},
	{
		"set -f; set +f; touch a.x; echo *.x;",
		"a.x\n",
	},
	{
		"set -a; foo=bar; env | grep ^foo=",
		"foo=bar\n",
	},
	{
		"set -a; foo=(b a r); env | grep ^foo=",
		"exit status 1",
	},
	{
		"foo=bar; set -a; env | grep ^foo=",
		"exit status 1",
	},
	{
		"a=b; echo $a; set -u; echo $a",
		"b\nb\n",
	},
	{
		"echo $a; set -u; echo $a",
		"\na: unbound variable\nexit status 1 #JUSTERR",
	},
	{"set -n; echo foo", ""},
	{"set -n; [ wrong", ""},
	{"set -n; set +n; echo foo", ""},
	{
		"set -o foobar",
		"set: invalid option: \"-o\"\nexit status 2 #JUSTERR",
	},
	{"set -o noexec; echo foo", ""},
	{"set +o noexec; echo foo", "foo\n"},
	{
		"set -e; set -o",
		`allexport:	off
errexit:	on
noexec:	off
noglob:	off
nounset:	off
pipefail:	off
 #IGNORE`,
	},
	{
		"set -a; set +o",
		`set -o allexport
set +o errexit
set +o noexec
set +o noglob
set +o nounset
set +o pipefail
 #IGNORE`,
	},

	// unset
	{
		"a=1; echo $a; unset a; echo $a",
		"1\n\n",
	},
	{
		"a() { echo func; }; a; unset -f a; a",
		"func\n\"a\": executable file not found in $PATH\nexit status 127 #JUSTERR",
	},
	{
		"a=1; a() { echo func; }; unset -f a; echo $a",
		"1\n",
	},
	{
		"a=1; a() { echo func; }; unset -v a; a; echo $a",
		"func\n\n",
	},
	{
		"a=1; a() { echo func; }; a; echo $a; unset a; a; echo $a; unset a; a",
		"func\n1\nfunc\n\n\"a\": executable file not found in $PATH\nexit status 127 #JUSTERR",
	},
	{
		"unset PATH; [[ $PATH == '' ]]",
		"",
	},
	{
		"readonly a=1; echo $a; unset a; echo $a",
		"1\na: readonly variable\n1\n #IGNORE",
	},
	{
		"f() { local a=1; echo $a; unset a; echo $a; }; f",
		"1\n\n",
	},
	{
		`a=b eval 'echo $a; unset a; echo $a'`,
		"b\n\n",
	},

	// IFS
	{`echo -n "$IFS"`, " \t\n"},
	{`a="x:y:z"; IFS=:; echo $a`, "x y z\n"},
	{`a=(x y z); IFS=-; echo "${a[*]}"`, "x-y-z\n"},
	{`a=(x y z); IFS=-; echo "${a[@]}"`, "x y z\n"},
	{`a="  x y z"; IFS=; echo $a`, "  x y z\n"},
	{`a=(x y z); IFS=; echo "${a[*]}"`, "xyz\n"},
	{`a=(x y z); IFS=-; echo "${!a[@]}"`, "0 1 2\n"},

	// builtin
	{"builtin", ""},
	{"builtin noexist", "exit status 1 #JUSTERR"},
	{"builtin echo foo", "foo\n"},
	{
		"echo() { printf 'bar\n'; }; echo foo; builtin echo foo",
		"bar\nfoo\n",
	},

	// type
	{"type", ""},
	{"type echo", "echo is a shell builtin\n"},
	{"echo() { :; }; type echo | sed 1q", "echo is a function\n"},
	{"type bash | grep -q -E 'bash is (/|[A-Z]:).*'", ""},
	{"type noexist", "type: noexist: not found\nexit status 1 #JUSTERR"},

	// eval
	{"eval", ""},
	{"eval ''", ""},
	{"eval echo foo", "foo\n"},
	{"eval 'echo foo'", "foo\n"},
	{"eval 'exit 1'", "exit status 1"},
	{"eval '('", "eval: 1:1: reached EOF without matching ( with )\nexit status 1 #JUSTERR"},
	{"set a b; eval 'echo $@'", "a b\n"},
	{"eval 'a=foo'; echo $a", "foo\n"},
	{`a=b eval "echo $a"`, "\n"},
	{`a=b eval 'echo $a'`, "b\n"},
	{`eval 'echo "\$a"'`, "$a\n"},
	{`a=b eval 'x=y eval "echo \$a \$x"'`, "b y\n"},
	{`a=b eval 'a=y eval "echo $a \$a"'`, "b y\n"},
	{"a=b eval '(echo $a)'", "b\n"},

	// source
	{
		"source",
		"1:1: source: need filename\nexit status 2 #JUSTERR",
	},
	{
		"echo 'echo foo' >a; source a; . a",
		"foo\nfoo\n",
	},
	{
		"echo 'echo $@' >a; source a; source a b c; echo $@",
		"\nb c\n\n",
	},
	{
		"echo 'foo=bar' >a; source a; echo $foo",
		"bar\n",
	},

	// indexed arrays
	{
		"a=foo; echo ${a[0]} ${a[@]} ${a[x]}; echo ${a[1]}",
		"foo foo foo\n\n",
	},
	{
		"a=(); echo ${a[0]} ${a[@]} ${a[x]} ${a[1]}",
		"\n",
	},
	{
		"a=(b c); echo $a; echo ${a[0]}; echo ${a[1]}; echo ${a[x]}",
		"b\nb\nc\nb\n",
	},
	{
		"a=(b c); echo ${a[@]}; echo ${a[*]}",
		"b c\nb c\n",
	},
	{
		"a=(1 2 3); echo ${a[2-1]}; echo $((a[1+1]))",
		"2\n3\n",
	},
	{
		"a=(1 2) x=(); a+=b x+=c; echo ${a[@]}; echo ${x[@]}",
		"1b 2\nc\n",
	},
	{
		"a=(1 2) x=(); a+=(b c) x+=(d e); echo ${a[@]}; echo ${x[@]}",
		"1 2 b c\nd e\n",
	},
	{
		"a=bbb; a+=(c d); echo ${a[@]}",
		"bbb c d\n",
	},
	{
		`a=('a  1' 'b  2'); for e in ${a[@]}; do echo "$e"; done`,
		"a\n1\nb\n2\n",
	},
	{
		`a=('a  1' 'b  2'); for e in "${a[*]}"; do echo "$e"; done`,
		"a  1 b  2\n",
	},
	{
		`a=('a  1' 'b  2'); for e in "${a[@]}"; do echo "$e"; done`,
		"a  1\nb  2\n",
	},
	{
		`a=([1]=y [0]=x); echo ${a[0]}`,
		"x\n",
	},
	{
		`a=(y); a[2]=x; echo ${a[2]}`,
		"x\n",
	},
	{
		`a="y"; a[2]=x; echo ${a[2]}`,
		"x\n",
	},
	{
		`declare -a a=(x y); echo ${a[1]}`,
		"y\n",
	},
	{
		`a=b; echo "${a[@]}"`,
		"b\n",
	},

	// associative arrays
	{
		`a=foo; echo ${a[""]} ${a["x"]}`,
		"foo foo\n",
	},
	{
		`declare -A a=(); echo ${a[0]} ${a[@]} ${a[1]} ${a["x"]}`,
		"\n",
	},
	{
		`declare -A a=([x]=b [y]=c); echo $a; echo ${a[0]}; echo ${a["x"]}; echo ${a["_"]}`,
		"\n\nb\n\n",
	},
	{
		`declare -A a=([x]=b [y]=c); echo ${a[@]}; echo ${a[*]}`,
		"b c\nb c\n",
	},
	{
		`declare -A a=([y]=b [x]=c); echo ${a[@]}; echo ${a[*]}`,
		"c b\nc b\n",
	},
	{
		`declare -A a=([x]=a); a["y"]=d; a["x"]=c; echo ${a[@]}`,
		"c d\n",
	},
	{
		`declare -A a=([x]=a); a[y]=d; a[x]=c; echo ${a[@]}`,
		"c d\n",
	},
	{
		// cheating a little; bash just did a=c
		`a=(["x"]=b ["y"]=c); echo ${a["y"]}`,
		"c\n",
	},
	{
		`declare -A a=(['x']=b); echo ${a['x']} ${a[$'x']} ${a[$"x"]}`,
		"b b b\n",
	},
	{
		`a=(['x']=b); echo ${a['y']}`,
		"\n #IGNORE bash requires -A",
	},

	// weird assignments
	{"a=b; a=(c d); echo ${a[@]}", "c d\n"},
	{"a=(b c); a=d; echo ${a[@]}", "d c\n"},
	{"declare -A a=([x]=b [y]=c); a=d; echo ${a[@]}", "d b c\n"},
	{"i=3; a=b; a[i]=x; echo ${a[@]}", "b x\n"},
	{"i=3; declare a=(b); a[i]=x; echo ${!a[@]}", "0 3\n"},
	{"i=3; declare -A a=(['x']=b); a[i]=x; echo ${!a[@]}", "i x\n"},

	// declare
	{"declare -B foo", "declare: invalid option \"-B\"\nexit status 2 #JUSTERR"},
	{"a=b; declare a; echo $a; declare a=; echo $a", "b\n\n"},
	{"a=b; declare a; echo $a", "b\n"},
	{
		"declare a=b c=(1 2); echo $a; echo ${c[@]}",
		"b\n1 2\n",
	},
	{"a=x; declare $a; echo $a $x", "x\n"},
	{"a=x=y; declare $a; echo $a $x", "x=y y\n"},
	{"a='x=(y)'; declare $a; echo $a $x", "x=(y) (y)\n"},
	{"a='x=b y=c'; declare $a; echo $x $y", "b c\n"},

	// export
	{"declare foo=bar; env | grep '^foo='", "exit status 1"},
	{"declare -x foo=bar; env | grep '^foo='", "foo=bar\n"},
	{"export foo=bar; env | grep '^foo='", "foo=bar\n"},
	{"foo=bar; export foo; env | grep '^foo='", "foo=bar\n"},
	{"export foo=bar; foo=baz; env | grep '^foo='", "foo=baz\n"},
	{"export foo=bar; readonly foo=baz; env | grep '^foo='", "foo=baz\n"},
	{"export foo=(1 2); env | grep '^foo='", "exit status 1"},
	{"declare -A foo=([a]=b); export foo; env | grep '^foo='", "exit status 1"},
	{"export foo=(b c); foo=x; env | grep '^foo='", "exit status 1"},

	// local
	{
		"local a=b",
		"local: can only be used in a function\nexit status 1 #JUSTERR",
	},
	{
		"local a=b 2>/dev/null; echo $a",
		"\n",
	},
	{
		"{ local a=b; }",
		"local: can only be used in a function\nexit status 1 #JUSTERR",
	},
	{
		"echo 'local a=b' >a; source a",
		"local: can only be used in a function\nexit status 1 #JUSTERR",
	},
	{
		"echo 'local a=b' >a; f() { source a; }; f; echo $a",
		"\n",
	},
	{
		"f() { local a=b; }; f; echo $a",
		"\n",
	},
	{
		"a=x; f() { local a=b; }; f; echo $a",
		"x\n",
	},
	{
		"a=x; f() { echo $a; local a=b; echo $a; }; f",
		"x\nb\n",
	},
	{
		"f1() { local a=b; }; f2() { f1; echo $a; }; f2",
		"\n",
	},
	{
		"f() { a=1; declare b=2; export c=3; readonly d=4; declare -g e=5; }; f; echo $a $b $c $d $e",
		"1 3 4 5\n",
	},

	// name references
	{"declare -n foo=bar; bar=etc; [[ -R foo ]]", ""},
	{"declare -n foo=bar; bar=etc; [ -R foo ]", ""},
	{"nameref foo=bar; bar=etc; [[ -R foo ]]", " #IGNORE"},
	{"declare foo=bar; bar=etc; [[ -R foo ]]", "exit status 1"},
	{
		"declare -n foo=bar; bar=etc; echo $foo; bar=zzz; echo $foo",
		"etc\nzzz\n",
	},
	{
		"declare -n foo=bar; bar=(x y); echo ${foo[1]}; bar=(a b); echo ${foo[1]}",
		"y\nb\n",
	},
	{
		"declare -n foo=bar; bar=etc; echo $foo; unset bar; echo $foo",
		"etc\n\n",
	},
	{
		"declare -n a1=a2 a2=a3 a3=a4; a4=x; echo $a1 $a3",
		"x x\n",
	},
	{
		"declare -n foo=bar bar=foo; echo $foo",
		"\n #IGNORE",
	},
	{
		"declare -n foo=bar; echo $foo",
		"\n",
	},
	{
		"declare -n foo=bar; echo ${!foo}",
		"bar\n",
	},
	{
		"declare -n foo=bar; bar=etc; echo $foo; echo ${!foo}",
		"etc\nbar\n",
	},

	// read-only vars
	{"declare -r foo=bar; echo $foo", "bar\n"},
	{"readonly foo=bar; echo $foo", "bar\n"},
	{
		"a=b; a=c; echo $a; readonly a; a=d",
		"c\na: readonly variable\nexit status 1 #JUSTERR",
	},
	{
		"declare -r foo=bar; foo=etc",
		"foo: readonly variable\nexit status 1 #JUSTERR",
	},
	{
		"readonly foo=bar; foo=etc",
		"foo: readonly variable\nexit status 1 #JUSTERR",
	},

	// multiple var modes at once
	{
		"declare -r -x foo=bar; env | grep '^foo='",
		"foo=bar\n",
	},
	{
		"declare -r -x foo=bar; foo=x",
		"foo: readonly variable\nexit status 1 #JUSTERR",
	},

	// glob
	{"echo .", ".\n"},
	{"echo ..", "..\n"},
	{"echo ./.", "./.\n"},
	{
		"touch a.x b.x c.x; echo *.x; rm a.x b.x c.x",
		"a.x b.x c.x\n",
	},
	{
		`touch a.x; echo '*.x' "*.x"; rm a.x`,
		"*.x *.x\n",
	},
	{
		`touch a.x; echo *'.x' "a."* '*'.x; rm a.x`,
		"a.x a.x *.x\n",
	},
	{
		"echo *.x; echo foo *.y bar",
		"*.x\nfoo *.y bar\n",
	},
	{
		"mkdir a; touch a/b.x; echo */*.x | sed 's@\\\\@/@'; cd a; echo *.x",
		"a/b.x\nb.x\n",
	},
	{
		"mkdir -p a/b/c; echo a/* | sed 's@\\\\@/@'",
		"a/b\n",
	},
	{
		"mkdir -p '*/a.z' 'b/a.z'; cd '*'; set -- *.z; echo $#",
		"1\n",
	},
	{
		"touch .hidden a; echo *; echo .h*; rm .hidden a",
		"a\n.hidden\n",
	},
	{
		`mkdir d; touch d/.hidden d/a; set -- "$(echo d/*)" "$(echo d/.h*)"; echo ${#1} ${#2}; rm -r d`,
		"3 9\n",
	},

	// brace expansion; more exhaustive tests in the syntax package
	{"echo a}b", "a}b\n"},
	{"echo {a,b{c,d}", "{a,bc {a,bd\n"},
	{"echo a{b}", "a{b}\n"},
	{"echo a{à,世界}", "aà a世界\n"},
	{"echo a{b,c}d{e,f}g", "abdeg abdfg acdeg acdfg\n"},
	{"echo a{b{x,y},c}d", "abxd abyd acd\n"},
	{"echo a{1..", "a{1..\n"},
	{"echo a{1..2}b{4..5}c", "a1b4c a1b5c a2b4c a2b5c\n"},
	{"echo a{c..f}", "ac ad ae af\n"},
	{"echo a{4..1..1}", "a4 a3 a2 a1\n"},

	// /dev/null
	{"echo foo >/dev/null", ""},
	{"cat </dev/null", ""},

	// time - real would be slow and flaky; see TestElapsedString
	{"{ time; } |& wc", "      4       6      42\n"},
	{"{ time echo -n; } |& wc", "      4       6      42\n"},
	{"{ time -p; } |& wc", "      3       6      29\n"},
	{"{ time -p echo -n; } |& wc", "      3       6      29\n"},

	// exec
	{"exec", ""},
	{
		"exec builtin echo foo",
		"\"builtin\": executable file not found in $PATH\nexit status 127 #JUSTERR",
	},
	{
		"exec echo foo; echo bar",
		"foo\n",
	},

	// read
	{
		"read </dev/null",
		"exit status 1",
	},
	{
		"read -X",
		"read: invalid option \"-X\"\nexit status 2 #JUSTERR",
	},
	{
		"read 0ab",
		"read: invalid identifier \"0ab\"\nexit status 2 #JUSTERR",
	},
	{
		"read <<< foo; echo $REPLY",
		"foo\n",
	},
	{
		"read <<<'  a  b  c  '; echo \"$REPLY\"",
		"  a  b  c  \n",
	},
	{
		"read <<< 'y\nn\n'; echo $REPLY",
		"y\n",
	},
	{
		"read a_0 <<< foo; echo $a_0",
		"foo\n",
	},
	{
		"read a b <<< 'foo  bar  baz  '; echo \"$a\"; echo \"$b\"",
		"foo\nbar  baz\n",
	},
	{
		"while read a; do echo $a; done <<< 'a\nb\nc'",
		"a\nb\nc\n",
	},
	{
		"while read a b; do echo -e \"$a\n$b\"; done <<< '1 2\n3'",
		"1\n2\n3\n\n",
	},
	{
		`read a <<< '\\'; echo "$a"`,
		"\\\n",
	},
	{
		`read a <<< '\a\b\c'; echo "$a"`,
		"abc\n",
	},
	{
		"read -r a b <<< '1\\\t2'; echo $a; echo $b;",
		"1\\\n2\n",
	},
	{
		"echo line\\\ncontinuation | while read a; do echo $a; done",
		"linecontinuation\n",
	},
	{
		`read -r a <<< '\\'; echo "$a"`,
		"\\\\\n",
	},
	{
		"read -r a <<< '\\a\\b\\c'; echo $a",
		"\\a\\b\\c\n",
	},
	{
		"IFS=: read a b c <<< '1:2:3'; echo $a; echo $b; echo $c",
		"1\n2\n3\n",
	},
	{
		"IFS=: read a b c <<< '1\\:2:3'; echo \"$a\"; echo $b; echo $c",
		"1:2\n3\n\n",
	},

	// getopts
	{
		"getopts",
		"getopts: usage: getopts optstring name [arg]\nexit status 2",
	},
	{
		"getopts a a:b",
		"getopts: invalid identifier: \"a:b\"\nexit status 2 #JUSTERR",
	},
	{
		"getopts abc opt -a; echo $opt; $optarg",
		"a\n",
	},
	{
		"getopts abc opt -z",
		"getopts: illegal option -- \"z\"\n #IGNORE",
	},
	{
		"getopts a: opt -a",
		"getopts: option requires an argument -- \"a\"\n #IGNORE",
	},
	{
		"getopts :abc opt -z; echo $opt; echo $OPTARG",
		"?\nz\n",
	},
	{
		"getopts :a: opt -a; echo $opt; echo $OPTARG",
		":\na\n",
	},
	{
		"getopts abc opt foo -a; echo $opt; echo $OPTIND",
		"?\n1\n",
	},
	{
		"getopts abc opt -a foo; echo $opt; echo $OPTIND",
		"a\n2\n",
	},
	{
		"OPTIND=3; getopts abc opt -a -b -c; echo $opt;",
		"c\n",
	},
	{
		"OPTIND=100; getopts abc opt -a -b -c; echo $opt;",
		"?\n",
	},
	{
		"OPTIND=foo; getopts abc opt -a -b -c; echo $opt;",
		"a\n",
	},
	{
		"while getopts ab:c opt -c -b arg -a foo; do echo $opt $OPTARG $OPTIND; done",
		"c 2\nb arg 4\na 5\n",
	},
	{
		"while getopts abc opt -ba -c foo; do echo $opt $OPTARG $OPTIND; done",
		"b 1\na 2\nc 3\n",
	},
	{
		"a() { while getopts abc: opt; do echo $opt $OPTARG; done }; a -a -b -c arg",
		"a\nb\nc arg\n",
	},
}

// concBuffer wraps a bytes.Buffer in a mutex so that concurrent writes
// to it don't upset the race detector.
type concBuffer struct {
	buf bytes.Buffer
	sync.Mutex
}

func (c *concBuffer) Write(p []byte) (int, error) {
	c.Lock()
	n, err := c.buf.Write(p)
	c.Unlock()
	return n, err
}

func (c *concBuffer) WriteString(s string) (int, error) {
	c.Lock()
	n, err := c.buf.WriteString(s)
	c.Unlock()
	return n, err
}

func (c *concBuffer) String() string {
	c.Lock()
	s := c.buf.String()
	c.Unlock()
	return s
}

// wc: leading whitespace padding
// touch -d @: no way to set unix timestamps
var skipOnDarwin = regexp.MustCompile(`\bwc\b|touch -d @`)

// chmod: very different by design
// mkfifo: very different by design
// ln -s: requires linked path to exist, stat does not work well
// ~root: username does not exist
var skipOnWindows = regexp.MustCompile(`chmod|mkfifo|ln -s|~root`)

func skipFileReason(src string) string {
	if runtime.GOOS == "darwin" && skipOnDarwin.MatchString(src) {
		return "skipping linux-only test on darwin"
	}
	if runtime.GOOS == "windows" && skipOnWindows.MatchString(src) {
		return "skipping unix-only test on windows"
	}
	return ""
}

func TestFile(t *testing.T) {
	p := syntax.NewParser()
	for i := range fileCases {
		t.Run(fmt.Sprintf("%03d", i), func(t *testing.T) {
			c := fileCases[i]
			if reason := skipFileReason(c.in); reason != "" {
				t.Skip(reason)
			}
			file, err := p.Parse(strings.NewReader(c.in), "")
			if err != nil {
				t.Fatalf("could not parse: %v", err)
			}
			t.Parallel()
			dir, err := ioutil.TempDir("", "interp-test")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(dir)
			var cb concBuffer
			r := Runner{
				Dir:    dir,
				Stdout: &cb,
				Stderr: &cb,
			}
			r.Open = OpenDevImpls(DefaultOpen)
			r.Reset()
			if err := r.Run(file); err != nil {
				cb.WriteString(err.Error())
			}
			want := c.want
			if i := strings.Index(want, " #JUSTERR"); i >= 0 {
				want = want[:i]
			}
			if i := strings.Index(want, " #IGNORE"); i >= 0 {
				want = want[:i]
			}
			if got := cb.String(); got != want {
				t.Fatalf("wrong output in %q:\nwant: %q\ngot:  %q",
					c.in, want, got)
			}
		})
	}
}

func TestFileConfirm(t *testing.T) {
	if testing.Short() {
		t.Skip("calling bash is slow.")
	}
	if !hasBash44 {
		t.Skip("bash 4.4 required to run")
	}
	for i := range fileCases {
		t.Run(fmt.Sprintf("%03d", i), func(t *testing.T) {
			c := fileCases[i]
			if strings.Contains(c.want, " #IGNORE") {
				return
			}
			if reason := skipFileReason(c.in); reason != "" {
				t.Skip(reason)
			}
			t.Parallel()
			dir, err := ioutil.TempDir("", "interp-test")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(dir)
			cmd := exec.Command("bash")
			cmd.Dir = dir
			cmd.Stdin = strings.NewReader(c.in)
			out, err := cmd.CombinedOutput()
			if strings.Contains(c.want, " #JUSTERR") {
				// bash sometimes exits with code 0 and
				// stderr "bash: ..." for an error
				fauxErr := bytes.HasPrefix(out, []byte("bash:"))
				if err == nil && !fauxErr {
					t.Fatalf("wanted bash to error in %q", c.in)
				}
				return
			}
			got := string(out)
			if err != nil {
				got += err.Error()
			}
			if got != c.want {
				t.Fatalf("wrong bash output in %q:\nwant: %q\ngot:  %q",
					c.in, c.want, got)
			}
		})
	}
}

func TestRunnerOpts(t *testing.T) {
	withPath := func(strs ...string) []string {
		env := []string{"PATH=" + os.Getenv("PATH")}
		return append(env, strs...)
	}
	cases := []struct {
		runner   Runner
		in, want string
	}{
		{
			Runner{},
			"env | grep '^INTERP_GLOBAL='",
			"INTERP_GLOBAL=value\n",
		},
		{
			Runner{Env: withPath()},
			"env | grep '^INTERP_GLOBAL='",
			"exit status 1",
		},
		{
			Runner{Env: withPath("INTERP_GLOBAL=bar")},
			"env | grep '^INTERP_GLOBAL='",
			"INTERP_GLOBAL=bar\n",
		},
		{
			Runner{Env: withPath("a=b")},
			"env | grep '^a='; echo $a",
			"a=b\nb\n",
		},
		{
			// TODO(mvdan): remove tail once we only support
			// Go 1.9 and later, since os/exec doesn't dedup
			// the env in earlier versions.
			Runner{Env: withPath("a=b", "a=c")},
			"env | grep '^a=' | tail -n 1; echo $a",
			"a=c\nc\n",
		},
		{
			Runner{Env: withPath("foo")},
			"",
			`env not in the form key=value: "foo"`,
		},
		{
			Runner{Env: withPath("HOME=")},
			"echo $HOME",
			"\n",
		},
		{
			Runner{Env: withPath("PWD=foo")},
			"[[ $PWD == foo ]]",
			"exit status 1",
		},
	}
	p := syntax.NewParser()
	for i, c := range cases {
		t.Run(fmt.Sprintf("%03d", i), func(t *testing.T) {
			file, err := p.Parse(strings.NewReader(c.in), "")
			if err != nil {
				t.Fatalf("could not parse: %v", err)
			}
			var cb concBuffer
			r := c.runner
			r.Stdout = &cb
			r.Stderr = &cb
			if err := r.Reset(); err != nil {
				cb.WriteString(err.Error())
			}
			if err := r.Run(file); err != nil {
				cb.WriteString(err.Error())
			}
			if got := cb.String(); got != c.want {
				t.Fatalf("wrong output in %q:\nwant: %q\ngot:  %q",
					c.in, c.want, got)
			}
		})
	}
}

func TestRunnerContext(t *testing.T) {
	cases := []string{
		"",
		"while true; do true; done",
		"until false; do true; done",
		"sleep 1000",
		"while true; do true; done & wait",
		"sleep 1000 & wait",
		"(while true; do true; done)",
		"$(while true; do true; done)",
		"while true; do true; done | while true; do true; done",
	}
	p := syntax.NewParser()
	for i, in := range cases {
		t.Run(fmt.Sprintf("%03d", i), func(t *testing.T) {
			file, err := p.Parse(strings.NewReader(in), "")
			if err != nil {
				t.Fatalf("could not parse: %v", err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			r := Runner{Context: ctx}
			errChan := make(chan error)
			go func() {
				errChan <- r.Run(file)
			}()

			select {
			case err := <-errChan:
				if err != nil && err != ctx.Err() {
					t.Fatal("Runner did not use ctx.Err()")
				}
			case <-time.After(time.Millisecond * 100):
				t.Fatal("program was not killed in 0.1s")
			}
		})
	}
}

func TestRunnerAltNodes(t *testing.T) {
	in := "echo foo"
	want := "foo\n"
	file, err := syntax.NewParser().Parse(strings.NewReader(in), "")
	if err != nil {
		t.Fatalf("could not parse: %v", err)
	}
	nodes := []syntax.Node{
		file,
		file.Stmts[0],
		file.Stmts[0].Cmd,
	}
	for _, node := range nodes {
		var cb concBuffer
		r := Runner{
			Stdout: &cb,
			Stderr: &cb,
		}
		r.Reset()
		if err := r.Run(node); err != nil {
			cb.WriteString(err.Error())
		}
		if got := cb.String(); got != want {
			t.Fatalf("wrong output in %q:\nwant: %q\ngot:  %q",
				in, want, got)
		}
	}
}

func TestElapsedString(t *testing.T) {
	tests := []struct {
		in    time.Duration
		posix bool
		want  string
	}{
		{time.Nanosecond, false, "0m0.000s"},
		{time.Millisecond, false, "0m0.001s"},
		{time.Millisecond, true, "0.00"},
		{2500 * time.Millisecond, false, "0m2.500s"},
		{2500 * time.Millisecond, true, "2.50"},
		{
			10*time.Minute + 10*time.Second,
			false,
			"10m10.000s",
		},
		{
			10*time.Minute + 10*time.Second,
			true,
			"610.00",
		},
	}
	for _, tc := range tests {
		t.Run(tc.in.String(), func(t *testing.T) {
			got := elapsedString(tc.in, tc.posix)
			if got != tc.want {
				t.Fatalf("wanted %q, got %q", tc.want, got)
			}
		})
	}
}
