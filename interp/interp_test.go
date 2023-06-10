// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package interp_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math/bits"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/interp"
	"mvdan.cc/sh/v3/syntax"
)

// runnerRunTimeout is the context timeout used by any tests calling Runner.Run.
// The timeout saves us from hangs or burning too much CPU if there are bugs.
// All the test cases are designed to be inexpensive and stop in a very short
// amount of time, so 5s should be plenty even for busy machines.
const runnerRunTimeout = 5 * time.Second

// Some program which should be in $PATH. Needs to run before runTests is
// initialized (so an init function wouldn't work), because runTest uses it.
var pathProg = func() string {
	if runtime.GOOS == "windows" {
		return "cmd"
	}
	return "sh"
}()

func parse(tb testing.TB, parser *syntax.Parser, src string) *syntax.File {
	if parser == nil {
		parser = syntax.NewParser()
	}
	file, err := parser.Parse(strings.NewReader(src), "")
	if err != nil {
		tb.Fatal(err)
	}
	return file
}

func BenchmarkRun(b *testing.B) {
	b.ReportAllocs()
	b.StopTimer()
	src := `
echo a b c d
echo ./$foo_interp_missing/etc $(echo foo_interp_missing bar_interp_missing)
foo_interp_missing="bar_interp_missing"
x=y :
fn() {
	local a=b
	for i in 1 2 3; do
		echo $i | cat
	done
}
[[ $foo_interp_missing == bar_interp_missing ]] && fn
echo a{b,c}d *.go
let i=(2 + 3)
`
	file := parse(b, nil, src)
	r, _ := interp.New()
	ctx := context.Background()
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		r.Reset()
		if err := r.Run(ctx, file); err != nil {
			b.Fatal(err)
		}
	}
}

var hasBash50 bool

func TestMain(m *testing.M) {
	if os.Getenv("GOSH_PROG") != "" {
		switch os.Getenv("GOSH_CMD") {
		case "pid_and_hang":
			fmt.Println(os.Getpid())
			time.Sleep(time.Hour)
		case "foo_interp_missing_null_bar_interp_missing":
			fmt.Println("foo_interp_missing\x00bar_interp_missing")
			os.Exit(1)
		case "lookpath":
			_, err := exec.LookPath(pathProg)
			if err != nil {
				fmt.Println(err)
				os.Exit(1)
			}
			fmt.Printf("%s found\n", pathProg)
			os.Exit(0)
		}
		r := strings.NewReader(os.Args[1])
		file, err := syntax.NewParser().Parse(r, "")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		runner, _ := interp.New(
			interp.StdIO(os.Stdin, os.Stdout, os.Stderr),
			interp.OpenHandler(testOpenHandler),
			interp.ExecHandlers(testExecHandler),
		)
		ctx := context.Background()
		if err := runner.Run(ctx, file); err != nil {
			if status, ok := interp.IsExitStatus(err); ok {
				os.Exit(int(status))
			}

			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}

		os.Exit(0)
	}
	prog, err := os.Executable()
	if err != nil {
		panic(err)
	}
	os.Setenv("GOSH_PROG", prog)

	// Mimic syntax/parser_test.go's TestMain.
	os.Setenv("LANGUAGE", "C.UTF-8")
	os.Setenv("LC_ALL", "C.UTF-8")

	os.Unsetenv("CDPATH")
	hasBash50 = checkBash()

	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	os.Setenv("GO_TEST_DIR", wd)

	os.Setenv("INTERP_GLOBAL", "value")
	os.Setenv("MULTILINE_INTERP_GLOBAL", "\nwith\nnewlines\n\n")

	// Double check that env vars on Windows are case insensitive.
	if runtime.GOOS == "windows" {
		os.Setenv("mixedCase_INTERP_GLOBAL", "value")
	} else {
		os.Setenv("MIXEDCASE_INTERP_GLOBAL", "value")
	}

	os.Setenv("PATH_PROG", pathProg)

	// To print env vars. Only a builtin on Windows.
	if runtime.GOOS == "windows" {
		os.Setenv("ENV_PROG", "cmd /c set")
	} else {
		os.Setenv("ENV_PROG", "env")
	}

	for _, s := range []string{"a", "b", "c", "d", "foo_interp_missing", "bar_interp_missing"} {
		os.Unsetenv(s)
	}
	exit := m.Run()
	os.Exit(exit)
}

func checkBash() bool {
	out, err := exec.Command("bash", "-c", "echo -n $BASH_VERSION").Output()
	if err != nil {
		return false
	}
	return strings.HasPrefix(string(out), "5.1")
}

// concBuffer wraps a [bytes.Buffer] in a mutex so that concurrent writes
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

func (c *concBuffer) Reset() {
	c.Lock()
	c.buf.Reset()
	c.Unlock()
}

type runTest struct {
	in, want string
}

var runTests = []runTest{
	// no-op programs
	{"", ""},
	{"true", ""},
	{":", ""},
	{"exit", ""},
	{"exit 0", ""},
	{"{ :; }", ""},
	{"(:)", ""},

	// exit status codes
	{"exit 1", "exit status 1"},
	{"exit -1", "exit status 255"},
	{"exit 300", "exit status 44"},
	{"false", "exit status 1"},
	{"false foo_interp_missing", "exit status 1"},
	{"! false", ""},
	{"true foo_interp_missing", ""},
	{": foo_interp_missing", ""},
	{"! true", "exit status 1"},
	{"false; true", ""},
	{"false; exit", "exit status 1"},
	{"exit; echo foo_interp_missing", ""},
	{"exit 0; echo foo_interp_missing", ""},
	{"printf", "usage: printf format [arguments]\nexit status 2 #JUSTERR"},
	{"break", "break is only useful in a loop\n #JUSTERR"},
	{"continue", "continue is only useful in a loop\n #JUSTERR"},
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
	{"false; a=b", ""},
	{"false; false &", ""},

	// we don't need to follow bash error strings
	{"exit a", "invalid exit status code: \"a\"\nexit status 2 #JUSTERR"},
	{"exit 1 2", "exit cannot take multiple arguments\nexit status 1 #JUSTERR"},

	// echo
	{"echo", "\n"},
	{"echo a b c", "a b c\n"},
	{"echo -n foo_interp_missing", "foo_interp_missing"},
	{`echo -e '\t'`, "\t\n"},
	{`echo -E '\t'`, "\\t\n"},
	{"echo -x foo_interp_missing", "-x foo_interp_missing\n"},
	{"echo -e -x -e foo_interp_missing", "-x -e foo_interp_missing\n"},

	// printf
	{"printf foo_interp_missing", "foo_interp_missing"},
	{"printf %%", "%"},
	{"printf %", "missing format char\nexit status 1 #JUSTERR"},
	{"printf %; echo foo_interp_missing", "missing format char\nfoo_interp_missing\n #IGNORE"},
	{"printf %1", "missing format char\nexit status 1 #JUSTERR"},
	{"printf %+", "missing format char\nexit status 1 #JUSTERR"},
	{"printf %B foo_interp_missing", "invalid format char: B\nexit status 1 #JUSTERR"},
	{"printf %12-s foo_interp_missing", "invalid format char: -\nexit status 1 #JUSTERR"},
	{"printf ' %s \n' bar_interp_missing", " bar_interp_missing \n"},
	{"printf '\\A'", "\\A"},
	{"printf %s foo_interp_missing", "foo_interp_missing"},
	{"printf %s", ""},
	{"printf %d,%i 3 4", "3,4"},
	{"printf %d", "0"},
	{"printf %d,%d 010 0x10", "8,16"},
	{"printf %c,%c,%c foo_interp_missing àa", "f,\xc3,\x00"}, // TODO: use a rune?
	{"printf %3s a", "  a"},
	{"printf %3i 1", "  1"},
	{"printf %+i%+d 1 -3", "+1-3"},
	{"printf %-5x 10", "a    "},
	{"printf %02x 1", "01"},
	{"printf 'a% 5s' a", "a    a"},
	{"printf 'nofmt' 1 2 3", "nofmt"},
	{"printf '%d_' 1 2 3", "1_2_3_"},
	{"printf '%02d %02d\n' 1 2 3", "01 02\n03 00\n"},
	{`printf '0%s1' 'a\bc'`, `0a\bc1`},
	{`printf '0%b1' 'a\bc'`, "0a\bc1"},
	{"printf 'a%bc'", "ac"},

	// words and quotes
	{"echo  foo_interp_missing ", "foo_interp_missing\n"},
	{"echo ' foo_interp_missing '", " foo_interp_missing \n"},
	{`echo " foo_interp_missing "`, " foo_interp_missing \n"},
	{`echo a'b'c"d"e`, "abcde\n"},
	{`a=" b c "; echo $a`, "b c\n"},
	{`a=" b c "; echo "$a"`, " b c \n"},
	{`a=" b c "; echo foo${a}bar`, "foo b c bar\n"},
	{`a="b    c"; echo foo${a}bar`, "foob cbar\n"},
	{`echo "$(echo ' b c ')"`, " b c \n"},
	{"echo ''", "\n"},
	{`$(echo)`, ""},
	{`echo -n '\\'`, `\\`},
	{`echo -n "\\"`, `\`},
	{`set -- a b c; x="$@"; echo "$x"`, "a b c\n"},
	{`set -- b c; echo a"$@"d`, "ab cd\n"},
	{`count() { echo $#; }; set --; count "$@"`, "0\n"},
	{`count() { echo $#; }; set -- ""; count "$@"`, "1\n"},
	{`count() { echo $#; }; set -- ""; shift; count "$@"`, "0\n"},
	{`count() { echo $#; }; a=(); count "${a[@]}"`, "0\n"},
	{`count() { echo $#; }; a=(""); count "${a[@]}"`, "1\n"},
	{`echo $1 $3; set -- a b c; echo $1 $3`, "\na c\n"},
	{`[[ $0 == "bash" || $0 == "gosh" ]]`, ""},

	// dollar quotes
	{`echo $'foo_interp_missing\nbar_interp_missing'`, "foo_interp_missing\nbar_interp_missing\n"},
	{`echo $'\r\t\\'`, "\r\t\\\n"},
	{`echo $"foo_interp_missing\nbar_interp_missing"`, "foo_interp_missing\\nbar_interp_missing\n"},
	{`echo $'%s'`, "%s\n"},
	{`a=$'\r\t\\'; echo "$a"`, "\r\t\\\n"},
	{`a=$"foo_interp_missing\nbar_interp_missing"; echo "$a"`, "foo_interp_missing\\nbar_interp_missing\n"},
	{`echo $'\a\b\e\E\f\v'`, "\a\b\x1b\x1b\f\v\n"},
	{`echo $'\\\'\"\?'`, "\\'\"?\n"},
	{`echo $'\1\45\12345\777\9'`, "\x01%S45\xff\\9\n"},
	{`echo $'\x\xf\x09\xAB'`, "\\x\x0f\x09\xab\n"},
	{`echo $'\u\uf\u09\uABCD\u00051234'`, "\\u\u000f\u0009\uabcd\u00051234\n"},
	{`echo $'\U\Uf\U09\UABCD\U00051234'`, "\\U\u000f\u0009\uabcd\U00051234\n"},
	{
		"echo 'foo_interp_missing\x00bar_interp_missing'",
		"foo_interp_missingbar_interp_missing\n",
	},
	{
		"echo \"foo_interp_missing\x00bar_interp_missing\"",
		"foo_interp_missingbar_interp_missing\n",
	},
	{
		"echo $'foo_interp_missing\x00bar_interp_missing'",
		"foo_interp_missingbar_interp_missing\n",
	},
	{
		"echo $'foo_interp_missing\\x00bar_interp_missing'",
		"foo_interp_missing\n",
	},
	{
		"echo $'foo_interp_missing\\xbar_interp_missing'",
		"foo_interp_missing\xbar_interp_missing\n",
	},
	{
		"a='foo_interp_missing\x00bar_interp_missing'; eval \"echo -n ${a} ${a@Q}\";",
		"foo_interp_missingbar_interp_missing foo_interp_missingbar_interp_missing",
	},
	{
		"a=$'foo_interp_missing\\x00bar_interp_missing'; eval \"echo -n ${a} ${a@Q}\";",
		"foo_interp_missing foo_interp_missing",
	},
	{
		"i\x00f true; then echo foo_interp_missing\x00; \x00fi",
		"foo_interp_missing\n",
	},
	{
		"echo $(GOSH_CMD=foo_interp_missing_null_bar_interp_missing $GOSH_PROG)",
		"foo_interp_missingbar_interp_missing\n #IGNORE",
	},
	// See the TODO where FOO_INTERP_MISSING_NULL_BAR_INTERP_MISSING is set.
	// {
	// 	"echo $FOO_INTERP_MISSING_NULL_BAR_INTERP_MISSING \"${FOO_INTERP_MISSING_NULL_BAR_INTERP_MISSING}\"",
	// 	"foo_interp_missing\n",
	// },

	// escaped chars
	{"echo a\\b", "ab\n"},
	{"echo a\\ b", "a b\n"},
	{"echo \\$a", "$a\n"},
	{"echo \"a\\b\"", "a\\b\n"},
	{"echo 'a\\b'", "a\\b\n"},
	{"echo \"a\\\nb\"", "ab\n"},
	{"echo 'a\\\nb'", "a\\\nb\n"},
	{`echo "\""`, "\"\n"},
	{`echo \\`, "\\\n"},
	{`echo \\\\`, "\\\\\n"},
	{`echo \`, "\n"},

	// vars
	{"foo_interp_missing=bar_interp_missing; echo $foo_interp_missing", "bar_interp_missing\n"},
	{"foo_interp_missing=bar_interp_missing foo_interp_missing=etc; echo $foo_interp_missing", "etc\n"},
	{"foo_interp_missing=bar_interp_missing; foo_interp_missing=etc; echo $foo_interp_missing", "etc\n"},
	{"foo_interp_missing=bar_interp_missing; foo_interp_missing=; echo $foo_interp_missing", "\n"},
	{"unset foo_interp_missing; echo $foo_interp_missing", "\n"},
	{"foo_interp_missing=bar_interp_missing; unset foo_interp_missing; echo $foo_interp_missing", "\n"},
	{"echo $INTERP_GLOBAL", "value\n"},
	{"INTERP_GLOBAL=; echo $INTERP_GLOBAL", "\n"},
	{"unset INTERP_GLOBAL; echo $INTERP_GLOBAL", "\n"},
	{"echo $MIXEDCASE_INTERP_GLOBAL", "value\n"},
	{"foo_interp_missing=bar_interp_missing; foo_interp_missing=x true; echo $foo_interp_missing", "bar_interp_missing\n"},
	{"foo_interp_missing=bar_interp_missing; foo_interp_missing=x true; echo $foo_interp_missing", "bar_interp_missing\n"},
	{"foo_interp_missing=bar_interp_missing; $ENV_PROG | grep '^foo_interp_missing='", "exit status 1"},
	{"foo_interp_missing=bar_interp_missing $ENV_PROG | grep '^foo_interp_missing='", "foo_interp_missing=bar_interp_missing\n"},
	{"foo_interp_missing=a foo_interp_missing=b $ENV_PROG | grep '^foo_interp_missing='", "foo_interp_missing=b\n"},
	{"$ENV_PROG | grep '^INTERP_GLOBAL='", "INTERP_GLOBAL=value\n"},
	{"INTERP_GLOBAL=new; $ENV_PROG | grep '^INTERP_GLOBAL='", "INTERP_GLOBAL=new\n"},
	{"INTERP_GLOBAL=; $ENV_PROG | grep '^INTERP_GLOBAL='", "INTERP_GLOBAL=\n"},
	{"unset INTERP_GLOBAL; $ENV_PROG | grep '^INTERP_GLOBAL='", "exit status 1"},
	{"a=b; a+=c x+=y; echo $a $x", "bc y\n"},
	{`a=" x  y"; b=$a c="$a"; echo $b; echo $c`, "x y\nx y\n"},
	{`a=" x  y"; b=$a c="$a"; echo "$b"; echo "$c"`, " x  y\n x  y\n"},
	{`arr=("foo_interp_missing" "bar_interp_missing" "lala" "foo_interp_missingbar_interp_missing"); echo ${arr[@]:2}; echo ${arr[*]:2}`, "lala foo_interp_missingbar_interp_missing\nlala foo_interp_missingbar_interp_missing\n"},
	{`arr=("foo_interp_missing" "bar_interp_missing" "lala" "foo_interp_missingbar_interp_missing"); echo ${arr[@]:2:4}; echo ${arr[*]:1:4}`, "lala foo_interp_missingbar_interp_missing\nbar_interp_missing lala foo_interp_missingbar_interp_missing\n"},
	{`arr=("foo_interp_missing" "bar_interp_missing"); echo ${arr[@]}; echo ${arr[*]}`, "foo_interp_missing bar_interp_missing\nfoo_interp_missing bar_interp_missing\n"},
	{`arr=("foo_interp_missing"); echo ${arr[@]:99}`, "\n"},
	{`echo ${arr[@]:1:99}; echo ${arr[*]:1:99}`, "\n\n"},
	{`arr=(0 1 2 3 4 5 6 7 8 9 0 a b c d e f g h); echo ${arr[@]:3:4}`, "3 4 5 6\n"},
	{`echo ${foo_interp_missing[@]}; echo ${foo_interp_missing[*]}`, "\n\n"},
	// TODO: reenable once we figure out the broken pipe error
	//{`$ENV_PROG | while read line; do if test -z "$line"; then echo empty; fi; break; done`, ""}, // never begin with an empty element

	// inline variables have special scoping
	{
		"f() { echo $inline; inline=bar_interp_missing true; echo $inline; }; inline=foo_interp_missing f",
		"foo_interp_missing\nfoo_interp_missing\n",
	},
	{"v=x; read v <<< 'y'; echo $v", "y\n"},
	{"v=x; v=inline read v <<< 'y'; echo $v", "x\n"},
	{"v=x; v=inline unset v; echo $v", "x\n"},
	{"v=x; echo 'v=y' >f; v=inline source f; echo $v", "x\n"},
	{"declare -n v=v2; v=inline true; echo $v $v2", "\n"},
	{"f() { echo $v; }; v=x; v=y f; f", "y\nx\n"},
	{"f() { echo $v; }; v=x; v+=y f; f", "xy\nx\n"},
	{"f() { echo $v; }; declare -n v=v2; v2=x; v=y f; f", "y\nx\n"},
	{"f() { echo ${v[@]}; }; v=(e1 e2); v=y f; f", "y\ne1 e2\n"},

	// special vars
	{"echo $?; false; echo $?", "0\n1\n"},
	{"for i in 1 2; do\necho $LINENO\necho $LINENO\ndone", "2\n3\n2\n3\n"},
	{"[[ -n $$ && $$ -gt 0 ]]", ""},
	{"[[ $$ -eq $PPID ]]", "exit status 1"},

	// var manipulation
	{"echo ${#a} ${#a[@]}", "0 0\n"},
	{"a=bar_interp_missing; echo ${#a} ${#a[@]}", "18 1\n"},
	{"a=世界; echo ${#a}", "2\n"},
	{"a=(a bcd); echo ${#a} ${#a[@]} ${#a[*]} ${#a[1]}", "1 2 2 3\n"},
	{
		"a=($(echo a bcd)); echo ${#a} ${#a[@]} ${#a[*]} ${#a[1]}",
		"1 2 2 3\n",
	},
	{
		"a=([0]=$(echo a b) $(echo c d)); echo ${#a} ${#a[@]} ${#a[*]} ${#a[0]}",
		"3 3 3 3\n",
	},
	{"set -- a bc; echo ${#@} ${#*} $#", "2 2 2\n"},
	{
		"echo ${!a}; echo more",
		"invalid indirect expansion\nexit status 1 #JUSTERR",
	},
	{
		"a=b; echo ${!a}; b=c; echo ${!a}",
		"\nc\n",
	},
	{
		"a=foo_interp_missing; echo ${a:1}; echo ${a: -1}; echo ${a: -10}; echo ${a:5}",
		"oo_interp_missing\ng\nrp_missing\nnterp_missing\n",
	},
	{
		"a=foo_interp_missing; echo ${a::2}; echo ${a::-1}; echo ${a: -10}; echo ${a::5}",
		"fo\nfoo_interp_missin\nrp_missing\nfoo_i\n",
	},
	{
		"a=abc; echo ${a:1:1}",
		"b\n",
	},
	{
		"a=foo_interp_missing; echo ${a/no/x} ${a/o/i} ${a//o/i} ${a/fo/}",
		"foo_interp_missing fio_interp_missing fii_interp_missing o_interp_missing\n",
	},
	{
		"a=foo_interp_missing; echo ${a/*/xx} ${a//?/na} ${a/o*}",
		"xx nananananananananananananananananana f\n",
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
	{"a='foo_interp_missing/bar_interp_missing'; echo ${a//o*a/}", "fr_interp_missing\n"},
	{
		"echo ${a:-b}; echo $a; a=; echo ${a:-b}; a=c; echo ${a:-b}",
		"b\n\nb\nc\n",
	},
	{
		"echo ${#:-never} ${?:-never} ${LINENO:-never}",
		"0 0 1\n",
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
		"b\na: err2\nexit status 1 #JUSTERR",
	},
	{
		"a=b; echo ${a?err1}; a=; echo ${a?err2}; unset a; echo ${a?err3}",
		"b\n\na: err3\nexit status 1 #JUSTERR",
	},
	{
		"echo ${a:?%s}",
		"a: %s\nexit status 1 #JUSTERR",
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
		"a='àÉñ bAr_interp_missing'; echo ${a^}; echo ${a^^}",
		"ÀÉñ bAr_interp_missing\nÀÉÑ BAR_INTERP_MISSING\n",
	},
	{
		"a='àÉñ bAr_interp_missing'; echo ${a,}; echo ${a,,}",
		"àÉñ bAr_interp_missing\nàéñ bar_interp_missing\n",
	},
	{
		"a='àÉñ bAr_interp_missing'; echo ${a^?}; echo ${a^^[br]}",
		"ÀÉñ bAr_interp_missing\nàÉñ BAR_inteRp_missing\n",
	},
	{
		"a='àÉñ bAr_interp_missing'; echo ${a,?}; echo ${a,,[br]}",
		"àÉñ bAr_interp_missing\nàÉñ bAr_interp_missing\n",
	},
	{
		"a=(àÉñ bAr_interp_missing); echo ${a[@]^}; echo ${a[*],,}",
		"ÀÉñ BAr_interp_missing\nàéñ bar_interp_missing\n",
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
		`INTERP_X_2=b INTERP_X_1=a; set -- ${!INTERP_*}; echo $#`,
		"3\n",
	},
	{
		`INTERP_X_2=b INTERP_X_1=a; set -- "${!INTERP_*}"; echo $#`,
		"1\n",
	},
	{
		`INTERP_X_2=b INTERP_X_1=a; set -- ${!INTERP_@}; echo $#`,
		"3\n",
	},
	{
		`INTERP_X_2=b INTERP_X_1=a; set -- "${!INTERP_@}"; echo $#`,
		"3\n",
	},
	{
		`a='b  c'; eval "echo -n ${a} ${a@Q}"`,
		`b c b  c`,
	},
	{
		`a='"\n'; printf "%s %s" "${a}" "${a@E}"`,
		"\"\\n \"\n",
	},
	{
		"declare a; a+=(b); echo ${a[@]} ${#a[@]}",
		"b 1\n",
	},
	{
		`a=""; a+=(b); echo ${a[@]} ${#a[@]}`,
		"b 2\n",
	},
	{
		"f() { local a; a=bad; a=good; echo $a; }; f",
		"good\n",
	},

	// if
	{
		"if true; then echo foo_interp_missing; fi",
		"foo_interp_missing\n",
	},
	{
		"if false; then echo foo_interp_missing; fi",
		"",
	},
	{
		"if false; then echo foo_interp_missing; fi",
		"",
	},
	{
		"if true; then echo foo_interp_missing; else echo bar_interp_missing; fi",
		"foo_interp_missing\n",
	},
	{
		"if false; then echo foo_interp_missing; else echo bar_interp_missing; fi",
		"bar_interp_missing\n",
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
		"if false; then :; elif true; then echo foo_interp_missing; fi",
		"foo_interp_missing\n",
	},
	{
		"if false; then :; elif false; then :; elif true; then echo foo_interp_missing; fi",
		"foo_interp_missing\n",
	},
	{
		"if false; then :; elif false; then :; else echo foo_interp_missing; fi",
		"foo_interp_missing\n",
	},

	// while
	{
		"while false; do echo foo_interp_missing; done",
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
		"until true; do echo foo_interp_missing; done",
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
		"for i in 1 2 3; do echo $i; continue; echo foo_interp_missing; done",
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
	// for, with missing Init, Cond, Post
	{
		"i=0; for ((; i<3; i++)); do echo $i; done",
		"0\n1\n2\n",
	},
	{
		"for ((i=0;; i++)); do if [ $i -ge 3 ]; then break; fi; echo $i; done",
		"0\n1\n2\n",
	},
	{
		"for ((i=0; i<3;)); do echo $i; i=$((i+1)); done",
		"0\n1\n2\n",
	},
	{
		"i=0; for ((;;)); do if [ $i -ge 3 ]; then break; fi; echo $i; i=$((i+1)); done",
		"0\n1\n2\n",
	},
	// TODO: uncomment once expandEnv.Set starts returning errors
	// {
	// 	"readonly i; for ((i=0; i<3; i++)); do echo $i; done",
	// 	"0\n1\n2\n",
	// },
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
	{
		"set -- a 'b c'; for i in; do echo $i; done",
		"",
	},
	{
		"set -- a 'b c'; for i; do echo $i; done",
		"a\nb c\n",
	},

	// block
	{
		"{ echo foo_interp_missing; }",
		"foo_interp_missing\n",
	},
	{
		"{ false; }",
		"exit status 1",
	},

	// subshell
	{
		"(echo foo_interp_missing)",
		"foo_interp_missing\n",
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
		"(foo_interp_missing=bar_interp_missing; echo $foo_interp_missing); echo $foo_interp_missing",
		"bar_interp_missing\n\n",
	},
	{
		"(echo() { printf 'bar_interp_missing\n'; }; echo); echo",
		"bar_interp_missing\n\n",
	},
	{
		"unset INTERP_GLOBAL & echo $INTERP_GLOBAL",
		"value\n",
	},
	{
		"(fn() { :; }) & pwd >/dev/null",
		"",
	},
	{
		"x[0]=x; (echo ${x[0]}; x[0]=y; echo ${x[0]}); echo ${x[0]}",
		"x\ny\nx\n",
	},
	{
		`x[3]=x; (x[3]=y); echo ${x[3]}`,
		"x\n",
	},
	{
		"shopt -s expand_aliases; alias f='echo x'\nf\n(f\nalias f='echo y'\neval f\n)\nf\n",
		"x\nx\ny\nx\n",
	},
	{
		"set -- a; echo $1; (echo $1; set -- b; echo $1); echo $1",
		"a\na\nb\na\n",
	},
	{"false; ( echo $? )", "1\n"},

	// cd/pwd
	{"[[ fo~ == 'fo~' ]]", ""},
	{`[[ 'ab\c' == *\\* ]]`, ""},
	{`[[ foo_interp_missing/bar_interp_missing == foo_interp_missing* ]]`, ""},
	{"[[ a == [ab ]]", "exit status 1"},
	{`HOME='/*'; echo ~; echo "$HOME"`, "/*\n/*\n"},
	{`test -d ~`, ""},
	{
		`for flag in b c d e f g h k L p r s S u w x; do test -$flag ""; echo -n "$flag$? "; done`,
		`b1 c1 d1 e1 f1 g1 h1 k1 L1 p1 r1 s1 S1 u1 w1 x1 `,
	},
	{`foo_interp_missing=~; test -d $foo_interp_missing`, ""},
	{`foo_interp_missing=~; test -d "$foo_interp_missing"`, ""},
	{`foo_interp_missing='~'; test -d $foo_interp_missing`, "exit status 1"},
	{`foo_interp_missing='~'; [ $foo_interp_missing == '~' ]`, ""},
	{
		`[[ ~ == "$HOME" ]] && [[ ~/foo_interp_missing == "$HOME/foo_interp_missing" ]]`,
		"",
	},
	{
		`HOME=$PWD/home; mkdir home; touch home/f; [[ -e ~/f ]]`,
		"",
	},
	{
		`HOME=$PWD/home; mkdir home; touch home/f; [[ ~/f -ef $HOME/f ]]`,
		"",
	},
	{
		"[[ ~noexist == '~noexist' ]]",
		"",
	},
	{
		`w="$HOME"; cd; [[ $PWD == "$w" ]]`,
		"",
	},
	{
		`mkdir test.cd; cd test.cd; cd ''; [[ "$PWD" == "$OLDPWD" ]]`,
		"",
	},
	{
		`HOME=/foo_interp_missing; echo $HOME`,
		"/foo_interp_missing\n",
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
		">a && cd a",
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
		`pwd -a`,
		"invalid option: \"-a\"\nexit status 2 #JUSTERR",
	},
	{
		`pwd -L -P -a`,
		"invalid option: \"-a\"\nexit status 2 #JUSTERR",
	},
	{
		`mkdir a; ln -s a b; [[ "$(cd a && pwd -P)" == "$(cd b && pwd -P)" ]]`,
		"",
	},
	{
		`mkdir a; ln -s a b; [[ "$(cd a && pwd -P)" == "$(cd b && pwd -L)" ]]; echo $?`,
		"1\n",
	},
	{
		`orig="$PWD"; mkdir a; cd a; cd - >/dev/null; [[ "$PWD" == "$orig" ]]`,
		"",
	},
	{
		`orig="$PWD"; mkdir a; cd a; [[ $(cd -) == "$orig" ]]`,
		"",
	},

	// dirs/pushd/popd
	{"set -- $(dirs); echo $# ${#DIRSTACK[@]}", "1 1\n"},
	{"pushd", "pushd: no other directory\nexit status 1 #JUSTERR"},
	{"pushd -n", ""},
	{"pushd foo_interp_missing bar_interp_missing", "pushd: too many arguments\nexit status 2 #JUSTERR"},
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
		"mkdir a; pushd a >/dev/null; pushd >/dev/null; rm -r a; pushd",
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
	{"popd foo_interp_missing", "popd: invalid argument\nexit status 2 #JUSTERR"},
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
		"mkdir a; pushd a >/dev/null; pushd >/dev/null; rm -r a; popd",
		"exit status 1 #JUSTERR",
	},

	// binary cmd
	{
		"true && echo foo_interp_missing || echo bar_interp_missing",
		"foo_interp_missing\n",
	},
	{
		"false && echo foo_interp_missing || echo bar_interp_missing",
		"bar_interp_missing\n",
	},

	// func
	{
		"foo_interp_missing() { echo bar_interp_missing; }; foo_interp_missing",
		"bar_interp_missing\n",
	},
	{
		"foo_interp_missing() { echo $1; }; foo_interp_missing",
		"\n",
	},
	{
		"foo_interp_missing() { echo $1; }; foo_interp_missing a b",
		"a\n",
	},
	{
		"foo_interp_missing() { echo $1; bar_interp_missing c d; echo $2; }; bar_interp_missing() { echo $2; }; foo_interp_missing a b",
		"a\nd\nb\n",
	},
	{
		`foo_interp_missing() { echo $#; }; foo_interp_missing; foo_interp_missing 1 2 3; foo_interp_missing "a b"; echo $#`,
		"0\n3\n1\n0\n",
	},
	{
		`foo_interp_missing() { for a in $*; do echo "$a"; done }; foo_interp_missing 'a  1' 'b  2'`,
		"a\n1\nb\n2\n",
	},
	{
		`foo_interp_missing() { for a in "$*"; do echo "$a"; done }; foo_interp_missing 'a  1' 'b  2'`,
		"a  1 b  2\n",
	},
	{
		`foo_interp_missing() { for a in "foo_interp_missing$*"; do echo "$a"; done }; foo_interp_missing 'a  1' 'b  2'`,
		"foo_interp_missinga  1 b  2\n",
	},
	{
		`foo_interp_missing() { for a in $@; do echo "$a"; done }; foo_interp_missing 'a  1' 'b  2'`,
		"a\n1\nb\n2\n",
	},
	{
		`foo_interp_missing() { for a in "$@"; do echo "$a"; done }; foo_interp_missing 'a  1' 'b  2'`,
		"a  1\nb  2\n",
	},

	// alias (note the input newlines)
	{
		"alias foo_interp_missing; alias foo_interp_missing=echo; alias foo_interp_missing; alias foo_interp_missing=; alias foo_interp_missing",
		"alias: \"foo_interp_missing\" not found\nalias foo_interp_missing='echo'\nalias foo_interp_missing=''\n #IGNORE",
	},
	{
		"shopt -s expand_aliases; alias foo_interp_missing=echo\nfoo_interp_missing foo_interp_missing; foo_interp_missing bar_interp_missing",
		"foo_interp_missing\nbar_interp_missing\n",
	},
	{
		"shopt -s expand_aliases; alias true=echo\ntrue foo_interp_missing; unalias true\ntrue bar_interp_missing",
		"foo_interp_missing\n",
	},
	{
		"shopt -s expand_aliases; alias echo='echo a'\necho b c",
		"a b c\n",
	},
	{
		"shopt -s expand_aliases; alias foo_interp_missing='echo '\nfoo_interp_missing foo_interp_missing; foo_interp_missing bar_interp_missing",
		"echo\nbar_interp_missing\n",
	},

	// case
	{
		"case b in x) echo foo_interp_missing ;; a|b) echo bar_interp_missing ;; esac",
		"bar_interp_missing\n",
	},
	{
		"case b in x) echo foo_interp_missing ;; y|z) echo bar_interp_missing ;; esac",
		"",
	},
	{
		"case foo_interp_missing in bar_interp_missing) echo foo_interp_missing ;; *) echo bar_interp_missing ;; esac",
		"bar_interp_missing\n",
	},
	{
		"case foo_interp_missing in *o*) echo bar_interp_missing ;; esac",
		"bar_interp_missing\n",
	},
	{
		"case foo_interp_missing in '*') echo x ;; f*) echo y ;; esac",
		"y\n",
	},

	// exec
	{
		"$GOSH_PROG 'echo foo_interp_missing'",
		"foo_interp_missing\n",
	},
	{
		"$GOSH_PROG 'echo foo_interp_missing >&2' >/dev/null",
		"foo_interp_missing\n",
	},
	{
		"echo foo_interp_missing | $GOSH_PROG 'cat >&2' >/dev/null",
		"foo_interp_missing\n",
	},
	{
		"$GOSH_PROG 'exit 1'",
		"exit status 1",
	},
	{
		"exec >/dev/null; echo foo_interp_missing",
		"",
	},

	// return
	{"return", "return: can only be done from a func or sourced script\nexit status 1 #JUSTERR"},
	{"f() { return; }; f", ""},
	{"f() { return 2; }; f", "exit status 2"},
	{"f() { echo foo_interp_missing; return; echo bar_interp_missing; }; f", "foo_interp_missing\n"},
	{"f1() { :; }; f2() { f1; return; }; f2", ""},
	{"echo 'return' >a; source a", ""},
	{"echo 'return' >a; source a; return", "return: can only be done from a func or sourced script\nexit status 1 #JUSTERR"},
	{"echo 'return 2' >a; source a", "exit status 2"},
	{"echo 'echo foo_interp_missing; return; echo bar_interp_missing' >a; source a", "foo_interp_missing\n"},

	// command
	{"command", ""},
	{"command -o echo", "command: invalid option \"-o\"\nexit status 2 #JUSTERR"},
	{"command -vo echo", "command: invalid option \"-o\"\nexit status 2 #JUSTERR"},
	{"echo() { :; }; echo foo_interp_missing", ""},
	{"echo() { :; }; command echo foo_interp_missing", "foo_interp_missing\n"},
	{"command -v does-not-exist", "exit status 1"},
	{"foo_interp_missing() { :; }; command -v foo_interp_missing", "foo_interp_missing\n"},
	{"foo_interp_missing() { :; }; command -v does-not-exist foo_interp_missing", "foo_interp_missing\n"},
	{"command -v echo", "echo\n"},
	{"[[ $(command -v $PATH_PROG) == $PATH_PROG ]]", "exit status 1"},

	// cmd substitution
	{
		"echo foo_interp_missing $(printf bar_interp_missing)",
		"foo_interp_missing bar_interp_missing\n",
	},
	{
		"echo foo_interp_missing $(echo bar_interp_missing)",
		"foo_interp_missing bar_interp_missing\n",
	},
	{
		"$(echo echo foo_interp_missing bar_interp_missing)",
		"foo_interp_missing bar_interp_missing\n",
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
	{
		"a=sub true & { a=main $ENV_PROG | grep '^a='; }",
		"a=main\n",
	},
	{
		"echo foo_interp_missing >f; echo $(cat f); echo $(<f)",
		"foo_interp_missing\nfoo_interp_missing\n",
	},
	{
		"echo foo_interp_missing >f; echo $(<f; echo bar_interp_missing)",
		"bar_interp_missing\n",
	},
	{
		"$(false); echo $?; $(exit 3); echo $?; $(true); echo $?",
		"1\n3\n0\n",
	},
	{
		"foo=$(false); echo $?; echo foo $(false); echo $?",
		"1\nfoo\n0\n",
	},
	{
		"$(false) $(true); echo $?; $(true) $(false); echo $?",
		"0\n1\n",
	},
	{
		"foo=$(false) $(true); echo $?; foo=$(true) $(false); echo $?",
		"1\n0\n",
	},

	// pipes
	{
		"echo foo_interp_missing | sed 's/o/a/g'",
		"faa_interp_missing\n",
	},
	{
		"echo foo_interp_missing | false | true",
		"",
	},
	{
		"true $(true) | true", // used to panic
		"",
	},
	{
		// The first command in the block used to consume stdin, even
		// though it shouldn't be. We just want to run any arbitrary
		// non-builtin program that doesn't consume stdin.
		"echo foo_interp_missing | { $ENV_PROG >/dev/null; cat; }",
		"foo_interp_missing\n",
	},

	// redirects
	{
		"echo foo_interp_missing >&1 | sed 's/o/a/g'",
		"faa_interp_missing\n",
	},
	{
		"echo foo_interp_missing >&2 | sed 's/o/a/g'",
		"foo_interp_missing\n",
	},
	{
		// TODO: why does bash need a block here?
		"{ echo foo_interp_missing >&2; } |& sed 's/o/a/g'",
		"faa_interp_missing\n",
	},
	{
		"echo foo_interp_missing >/dev/null; echo bar_interp_missing",
		"bar_interp_missing\n",
	},
	{
		">a; echo foo_interp_missing >>b; wc -c <a >>b; cat b | tr -d ' '",
		"foo_interp_missing\n0\n",
	},
	{
		"echo foo_interp_missing >a; <a",
		"",
	},
	{
		"echo foo_interp_missing >a; wc -c <a | tr -d ' '",
		"19\n",
	},
	{
		"echo foo_interp_missing >>a; echo bar_interp_missing &>>a; wc -c <a | tr -d ' '",
		"38\n",
	},
	{
		"{ echo a; echo b >&2; } &>/dev/null",
		"",
	},
	{
		"sed 's/o/a/g' <<EOF\nfoo_interp_missing$foo_interp_missing\nEOF",
		"faa_interp_missing\n",
	},
	{
		"sed 's/o/a/g' <<'EOF'\nfoo_interp_missing$foo_interp_missing\nEOF",
		"faa_interp_missing$faa_interp_missing\n",
	},
	{
		"sed 's/o/a/g' <<EOF\n\tfoo_interp_missing\nEOF",
		"\tfaa_interp_missing\n",
	},
	{
		"sed 's/o/a/g' <<EOF\nfoo_interp_missing\nEOF",
		"faa_interp_missing\n",
	},
	{
		"cat <<EOF\n~/foo_interp_missing\nEOF",
		"~/foo_interp_missing\n",
	},
	{
		"sed 's/o/a/g' <<<foo_interp_missing$foo_interp_missing",
		"faa_interp_missing\n",
	},
	{
		"cat <<-EOF\n\tfoo_interp_missing\nEOF",
		"foo_interp_missing\n",
	},
	{
		"cat <<-EOF\n\tfoo_interp_missing\n\nEOF",
		"foo_interp_missing\n\n",
	},
	{
		"cat <<EOF\nfoo_interp_missing\\\nbar_interp_missing\nEOF",
		"foo_interp_missingbar_interp_missing\n",
	},
	{
		"cat <<'EOF'\nfoo_interp_missing\\\nbar_interp_missing\nEOF",
		"foo_interp_missing\\\nbar_interp_missing\n",
	},
	{
		"mkdir a; echo foo_interp_missing >a |& grep -q 'is a directory'",
		" #IGNORE bash prints a warning",
	},
	{
		"echo foo_interp_missing 1>&1 | sed 's/o/a/g'",
		"faa_interp_missing\n",
	},
	{
		"echo foo_interp_missing 2>&2 |& sed 's/o/a/g'",
		"faa_interp_missing\n",
	},
	{
		"printf 2>&1 | sed 's/.*usage.*/foo_interp_missing/'",
		"foo_interp_missing\n",
	},
	{
		"mkdir a && cd a && echo foo_interp_missing >b && cd .. && cat a/b",
		"foo_interp_missing\n",
	},

	// background/wait
	{"wait", ""},
	{"{ true; } & wait", ""},
	{"{ exit 1; } & wait", ""},
	{
		"{ echo foo_interp_missing; } & wait; echo bar_interp_missing",
		"foo_interp_missing\nbar_interp_missing\n",
	},
	{
		"{ echo foo_interp_missing & wait; } & wait; echo bar_interp_missing",
		"foo_interp_missing\nbar_interp_missing\n",
	},
	{`mkdir d; old=$PWD; cd d & wait; [[ $old == "$PWD" ]]`, ""},
	{
		"f() { echo 1; }; { sleep 0.01; f; } & f() { echo 2; }; wait",
		"1\n",
	},

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
		"[[ ' 3' -lt '4 ' ]]",
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
		"touch -t 202111050000.30 a b; [[ a -nt b || a -ot b ]]",
		"exit status 1",
	},
	{
		"touch -t 202111050200.00 a; touch -t 202111060100.00 b; [[ a -nt b ]]",
		"exit status 1",
	},
	{
		"touch -t 202111050000.00 a; touch -t 202111060000.00 b; [[ a -ot b ]]",
		"",
	},
	{
		"[[ a -ef b ]]",
		"exit status 1",
	},
	{
		">a >b; [[ a -ef b ]]",
		"exit status 1",
	},
	{
		">a; [[ a -ef a ]]",
		"",
	},
	{
		">a; ln a b; [[ a -ef b ]]",
		"",
	},
	{
		">a; ln -s a b; [[ a -ef b ]]",
		"",
	},
	{
		"[[ -z 'foo_interp_missing' || -n '' ]]",
		"exit status 1",
	},
	{
		"[[ -z '' && -n 'foo_interp_missing' ]]",
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
		"[[ *b = '*b' ]]",
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
		"[[ foo_interp_missing =~ foo_interp_missing && foo_interp_missing =~ .* && foo_interp_missing =~ f.o ]]",
		"",
	},
	{
		"[[ foo_interp_missing =~ oo ]] && echo foo_interp_missing; [[ foo_interp_missing =~ ^oo$ ]] && echo bar_interp_missing || true",
		"foo_interp_missing\n",
	},
	{
		"[[ a =~ [ ]]",
		"exit status 2",
	},
	{
		"[[ -e a ]] && echo x; >a; [[ -e a ]] && echo y",
		"y\n",
	},
	{
		"ln -s b a; [[ -e a ]] && echo x; >b; [[ -e a ]] && echo y",
		"y\n",
	},
	{
		"[[ -f a ]] && echo x; >a; [[ -f a ]] && echo y",
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
		"[[ -r a ]] && echo x; >a; [[ -r a ]] && echo y",
		"y\n",
	},
	{
		"[[ -w a ]] && echo x; >a; [[ -w a ]] && echo y",
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
		"[[ \"multiline\ntext\" == *text* ]] && echo x; [[ \"multiline\ntext\" == *multiline* ]] && echo y",
		"x\ny\n",
	},
	// * should match a newline
	{
		"[[ \"multiline\ntext\" == multiline*text ]] && echo x",
		"x\n",
	},
	{
		"[[ \"multiline\ntext\" == text ]]",
		"exit status 1",
	},
	{
		`case $'a\nb' in a*b) echo match ;; esac`,
		"match\n",
	},
	{
		`a=$'a\nb'; echo "${a/a*b/sub}"`,
		"sub\n",
	},
	{
		"mkdir a; cd a; test -f b && echo x; >b; test -f b && echo y",
		"y\n",
	},
	{
		">a; [[ -b a ]] && echo block; [[ -c a ]] && echo char; true",
		"",
	},
	{
		"[[ -e /dev/sda ]] || { echo block; exit; }; [[ -b /dev/sda ]] && echo block; [[ -c /dev/sda ]] && echo char; true",
		"block\n",
	},
	{
		"[[ -e /dev/nvme0n1 ]] || { echo block; exit; }; [[ -b /dev/nvme0n1 ]] && echo block; [[ -c /dev/nvme0n1 ]] && echo char; true",
		"block\n",
	},
	{
		"[[ -e /dev/tty ]] || { echo char; exit; }; [[ -b /dev/tty ]] && echo block; [[ -c /dev/tty ]] && echo char; true",
		"char\n",
	},
	{"[[ -t 1 ]]", "exit status 1"},
	{"[[ -t 1234 ]]", "exit status 1"},
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
	{"[ ' 3' -lt '4 ' ]", ""},
	{
		"[ -e a ] && echo x; >a; [ -e a ] && echo y",
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
		"touch -t 202111050000.00 a; touch -t 202111060000.00 b; [ a -nt b ]",
		"exit status 1",
	},
	{
		"touch -t 202111050000.00 a; touch -t 202111060000.00 b; [ a -ot b ]",
		"",
	},
	{
		">a; [ a -ef a ]",
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
		"[ -r a ] && echo x; >a; [ -r a ] && echo y",
		"y\n",
	},
	{
		"[ -w a ] && echo x; >a; [ -w a ] && echo y",
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
		">a; [ -b a ] && echo block; [ -c a ] && echo char; true",
		"",
	},
	{"[ -t 1 ]", "exit status 1"},
	{"[ -t 1234 ]", "exit status 1"},
	{"[ -o wrong ]", "exit status 1"},
	{"[ -o errexit ]", "exit status 1"},
	{"set -e; [ -o errexit ]", ""},
	{"a=x b=''; [ -v a -a -v b -a ! -v c ]", ""},
	{"[ a = a ]", ""},
	{"[ a != a ]", "exit status 1"},
	{"[ abc = ab* ]", "exit status 1"},
	{"[ abc != ab* ]", ""},

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
		"echo $((~0))",
		"-1\n",
	},
	{
		"echo $((~3))",
		"-4\n",
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
	{
		"x=3; echo $(($x)) $((x))",
		"3 3\n",
	},
	{
		"set -- 1; echo $(($@))",
		"1\n",
	},
	{
		"a=b b=a; echo $(($a))",
		"0\n #IGNORE bash prints a warning",
	},
	{
		"let x=3; let 3/0; ((3/0)); echo $((x/y)); let x/=0",
		"division by zero\ndivision by zero\ndivision by zero\ndivision by zero\nexit status 1 #JUSTERR",
	},
	{
		"let x=3; let 3%0; ((3%0)); echo $((x%y)); let x%=0",
		"division by zero\ndivision by zero\ndivision by zero\ndivision by zero\nexit status 1 #JUSTERR",
	},
	{
		"let x=' 3'; echo $x",
		"3\n",
	},
	{
		"x=' 3'; let x++; echo \"$x\"",
		"4\n",
	},

	// set/shift
	{
		"echo $#; set foo_interp_missing bar_interp_missing; echo $#",
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
		"set -e; false; echo foo_interp_missing",
		"exit status 1",
	},
	{
		"set -e; shouldnotexist; echo foo_interp_missing",
		"\"shouldnotexist\": executable file not found in $PATH\nexit status 127 #JUSTERR",
	},
	{
		"set -e; set +e; false; echo foo_interp_missing",
		"foo_interp_missing\n",
	},
	{
		"set -e; ! false; echo foo_interp_missing",
		"foo_interp_missing\n",
	},
	{
		"set -e; ! true; echo foo_interp_missing",
		"foo_interp_missing\n",
	},
	{
		"set -e; if false; then echo foo_interp_missing; fi",
		"",
	},
	{
		"set -e; while false; do echo foo_interp_missing; done",
		"",
	},
	{
		"set -e; false || true",
		"",
	},
	{
		"set -e; false && true; true",
		"",
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
		"set -o pipefail; false | :; echo next",
		"next\n",
	},
	{
		"set -e -o pipefail; false | :; echo next",
		"exit status 1",
	},
	{
		"set -f; >a.x; echo *.x;",
		"*.x\n",
	},
	{
		"set -f; set +f; >a.x; echo *.x;",
		"a.x\n",
	},
	{
		"set -a; foo_interp_missing=bar_interp_missing; $ENV_PROG | grep ^foo_interp_missing=",
		"foo_interp_missing=bar_interp_missing\n",
	},
	{
		"set -a; foo_interp_missing=(b a r); $ENV_PROG | grep ^foo_interp_missing=",
		"exit status 1",
	},
	{
		"foo_interp_missing=bar_interp_missing; set -a; $ENV_PROG | grep ^foo_interp_missing=",
		"exit status 1",
	},
	{
		"a=b; echo $a; set -u; echo $a",
		"b\nb\n",
	},
	{
		"echo $a; set -u; echo $a; echo extra",
		"\na: unbound variable\nexit status 1 #JUSTERR",
	},
	{
		"foo_interp_missing=bar_interp_missing; set -u; echo ${foo_interp_missing/bar_interp_missing/}",
		"\n",
	},
	{
		"foo_interp_missing=bar_interp_missing; set -u; echo ${foo_interp_missing#bar_interp_missing}",
		"\n",
	},
	{
		"set -u; echo ${foo_interp_missing/bar_interp_missing/}",
		"foo_interp_missing: unbound variable\nexit status 1 #JUSTERR",
	},
	{
		"set -u; echo ${foo_interp_missing#bar_interp_missing}",
		"foo_interp_missing: unbound variable\nexit status 1 #JUSTERR",
	},
	// TODO: detect this case as unset
	// {
	// 	"set -u; foo_interp_missing=(bar_interp_missing); echo $foo_interp_missing; echo ${foo_interp_missing[3]}",
	// 	"bar_interp_missing\nfoo_interp_missing: unbound variable\nexit status 1 #JUSTERR",
	// },
	{
		"set -u; foo_interp_missing=(''); echo ${foo_interp_missing[0]}",
		"\n",
	},
	{
		"set -u; echo ${#foo_interp_missing}",
		"foo_interp_missing: unbound variable\nexit status 1 #JUSTERR",
	},
	{
		"set -u; echo ${foo_interp_missing+bar_interp_missing}",
		"\n",
	},
	{
		"set -u; echo ${foo_interp_missing:+bar_interp_missing}",
		"\n",
	},
	{
		"set -u; echo ${foo_interp_missing-bar_interp_missing}",
		"bar_interp_missing\n",
	},
	{
		"set -u; echo ${foo_interp_missing:-bar_interp_missing}",
		"bar_interp_missing\n",
	},
	{
		"set -u; echo ${foo_interp_missing=bar_interp_missing}",
		"bar_interp_missing\n",
	},
	{
		"set -u; echo ${foo_interp_missing:=bar_interp_missing}",
		"bar_interp_missing\n",
	},
	{
		"set -u; echo ${foo_interp_missing?bar_interp_missing}",
		"foo_interp_missing: bar_interp_missing\nexit status 1 #JUSTERR",
	},
	{
		"set -u; echo ${foo_interp_missing:?bar_interp_missing}",
		"foo_interp_missing: bar_interp_missing\nexit status 1 #JUSTERR",
	},
	{
		"set -ue; set -ueo pipefail",
		"",
	},
	{"set -n; echo foo_interp_missing", ""},
	{"set -n; [ wrong", ""},
	{"set -n; set +n; echo foo_interp_missing", ""},
	{
		"set -o foo_interp_missingbar_interp_missing",
		"set: invalid option: \"foo_interp_missingbar_interp_missing\"\nexit status 2 #JUSTERR",
	},
	{"set -o noexec; echo foo_interp_missing", ""},
	{"set +o noexec; echo foo_interp_missing", "foo_interp_missing\n"},
	{"set -e; set -o | grep -E 'errexit|noexec' | wc -l | tr -d ' '", "2\n"},
	{"set -e; set -o | grep -E 'errexit|noexec' | grep 'on$' | wc -l | tr -d ' '", "1\n"},
	{
		"set -a; set +o",
		`set -o allexport
set +o errexit
set +o noexec
set +o noglob
set +o nounset
set +o xtrace
set +o pipefail
 #IGNORE`,
	},
	{`set - foobar; echo $@; set -; echo $@`, "foobar\nfoobar\n"},

	// unset
	{
		"a=1; echo $a; unset a; echo $a",
		"1\n\n",
	},
	{
		"notinpath() { echo func; }; notinpath; unset -f notinpath; notinpath",
		"func\n\"notinpath\": executable file not found in $PATH\nexit status 127 #JUSTERR",
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
		"notinpath=1; notinpath() { echo func; }; notinpath; echo $notinpath; unset notinpath; notinpath; echo $notinpath; unset notinpath; notinpath",
		"func\n1\nfunc\n\n\"notinpath\": executable file not found in $PATH\nexit status 127 #JUSTERR",
	},
	{
		"unset PATH; [[ $PATH == '' ]]",
		"",
	},
	{
		"readonly a=1; echo $a; unset a; echo $a",
		"1\na: readonly variable\n1\n #IGNORE bash prints a warning",
	},
	{
		"f() { local a=1; echo $a; unset a; echo $a; }; f",
		"1\n\n",
	},
	{
		`a=b eval 'echo $a; unset a; echo $a'`,
		"b\n\n",
	},
	{
		`$(unset INTERP_GLOBAL); echo $INTERP_GLOBAL; unset INTERP_GLOBAL; echo $INTERP_GLOBAL`,
		"value\n\n",
	},
	{
		`x=orig; f() { local x=local; unset x; x=still_local; }; f; echo $x`,
		"orig\n",
	},
	{
		`x=orig; f() { local x=local; unset x; [[ -v x ]] && echo set || echo unset; }; f`,
		"unset\n",
	},
	{
		`PS3="pick one: "; select opt in foo bar baz; do echo "Selected $opt"; break; done <<< 3`,
		"1) foo\n2) bar\n3) baz\npick one: Selected baz\n",
	},
	{
		`opts=(foo bar baz); select opt in ${opts[@]}; do echo "Selected $opt"; break; done <<< 99`,
		"1) foo\n2) bar\n3) baz\n#? Selected \n",
	},
	{
		`select opt in foo; do
	case $opt in
	foo) echo "option 1"; break;;
	*) echo "invalid option $REPLY"; break;;
	esac
done <<< 2`,
		"1) foo\n#? invalid option 2\n",
	},

	// shopt
	{"set -e; shopt -o | grep -E 'errexit|noexec' | wc -l | tr -d ' '", "2\n"},
	{"set -e; shopt -o | grep -E 'errexit|noexec' | grep 'on$' | wc -l | tr -d ' '", "1\n"},
	{"shopt -s -o noexec; echo foo_interp_missing", ""},
	{"shopt -so noexec; echo foo_interp_missing", ""},
	{"shopt -u -o noexec; echo foo_interp_missing", "foo_interp_missing\n"},
	{"shopt -u globstar; shopt globstar | grep 'off$' | wc -l | tr -d ' '", "1\n"},
	{"shopt -s globstar; shopt globstar | grep 'off$' | wc -l | tr -d ' '", "0\n"},
	{"shopt extglob | grep 'off' | wc -l | tr -d ' '", "1\n"},
	{
		"shopt inherit_errexit",
		"inherit_errexit\ton\t(\"off\" not supported)\n #JUSTERR",
	},
	{
		"shopt -s extglob",
		"shopt: invalid option name \"extglob\" \"off\" (\"on\" not supported)\nexit status 1 #IGNORE",
	},
	{
		"shopt -s interactive_comments",
		"shopt: invalid option name \"interactive_comments\" \"on\" (\"off\" not supported)\nexit status 1 #IGNORE",
	},
	{
		"shopt -s foo",
		"shopt: invalid option name \"foo\"\nexit status 1 #JUSTERR",
	},

	// IFS
	{`echo -n "$IFS"`, " \t\n"},
	{`a="x:y:z"; IFS=:; echo $a`, "x y z\n"},
	{`a=(x y z); IFS=-; echo ${a[*]}`, "x y z\n"},
	{`a=(x y z); IFS=-; echo ${a[@]}`, "x y z\n"},
	{`a=(x y z); IFS=-; echo "${a[*]}"`, "x-y-z\n"},
	{`a=(x y z); IFS=-; echo "${a[@]}"`, "x y z\n"},
	{`a="  x y z"; IFS=; echo $a`, "  x y z\n"},
	{`a=(x y z); IFS=; echo "${a[*]}"`, "xyz\n"},
	{`a=(x y z); IFS=-; echo "${!a[@]}"`, "0 1 2\n"},
	{`set -- x y z; IFS=-; echo $*`, "x y z\n"},
	{`set -- x y z; IFS=-; echo "$*"`, "x-y-z\n"},
	{`set -- x y z; IFS=; echo $*`, "x y z\n"},
	{`set -- x y z; IFS=; echo "$*"`, "xyz\n"},

	// builtin
	{"builtin", ""},
	{"builtin noexist", "exit status 1 #JUSTERR"},
	{"builtin echo foo_interp_missing", "foo_interp_missing\n"},
	{
		"echo() { printf 'bar_interp_missing\n'; }; echo foo_interp_missing; builtin echo foo_interp_missing",
		"bar_interp_missing\nfoo_interp_missing\n",
	},

	// type
	{"type", ""},
	{"type for", "for is a shell keyword\n"},
	{"type echo", "echo is a shell builtin\n"},
	{"echo() { :; }; type echo | grep 'is a function'", "echo is a function\n"},
	{"type $PATH_PROG | grep -q -E ' is (/|[A-Z]:)'", ""},
	{"type noexist", "type: noexist: not found\nexit status 1 #JUSTERR"},
	{"PATH=/; type $PATH_PROG", "type: " + pathProg + ": not found\nexit status 1 #JUSTERR"},
	{"shopt -s expand_aliases; alias foo_interp_missing='bar_interp_missing baz'\ntype foo_interp_missing", "foo_interp_missing is aliased to `bar_interp_missing baz'\n"},
	{"alias foo_interp_missing='bar_interp_missing baz'\ntype foo_interp_missing", "type: foo_interp_missing: not found\nexit status 1 #JUSTERR"},
	{"type -p $PATH_PROG | grep -q -E '^(/|[A-Z]:)'", ""},
	{"PATH=/; type -p $PATH_PROG", "exit status 1"},
	{"shopt -s expand_aliases; alias foo_interp_missing='bar_interp_missing'; type -t foo_interp_missing", "alias\n"},
	{"type -t case", "keyword\n"},
	{"foo_interp_missing(){ :; }; type -t foo_interp_missing", "function\n"},
	{"type -t type", "builtin\n"},
	{"type -t $PATH_PROG", "file\n"},
	{"type -t inexisting_dfgsdgfds", "exit status 1"},

	// trap
	{"trap 'echo at_exit' EXIT; true", "at_exit\n"},
	{"trap 'echo on_err' ERR; false; echo FAIL", "on_err\nFAIL\n"},
	{"trap 'echo on_err' ERR; false || true; echo OK", "OK\n"},
	{"trap 'echo at_exit' EXIT; trap - EXIT; echo OK", "OK\n"},
	{"set -e; trap 'echo A' ERR EXIT; false; echo FAIL", "A\nA\nexit status 1"},
	{"trap 'foo_interp_missingbar_interp_missing' UNKNOWN", "trap: UNKNOWN: invalid signal specification\nexit status 2 #JUSTERR"},
	// TODO: our builtin appears to not receive the piped bytes?
	// {"trap 'echo on_err' ERR; trap | grep -q '.*echo on_err.*'", "trap -- \"echo on_err\" ERR\n"},
	{"trap 'false' ERR EXIT; false", "exit status 1"},

	// eval
	{"eval", ""},
	{"eval ''", ""},
	{"eval echo foo_interp_missing", "foo_interp_missing\n"},
	{"eval 'echo foo_interp_missing'", "foo_interp_missing\n"},
	{"eval 'exit 1'", "exit status 1"},
	{"eval '('", "eval: 1:1: reached EOF without matching ( with )\nexit status 1 #JUSTERR"},
	{"set a b; eval 'echo $@'", "a b\n"},
	{"eval 'a=foo_interp_missing'; echo $a", "foo_interp_missing\n"},
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
		"echo 'echo foo_interp_missing' >a; source a; . a",
		"foo_interp_missing\nfoo_interp_missing\n",
	},
	{
		"echo 'echo $@' >a; source a; source a b c; echo $@",
		"\nb c\n\n",
	},
	{
		"echo 'foo_interp_missing=bar_interp_missing' >a; source a; echo $foo_interp_missing",
		"bar_interp_missing\n",
	},

	// source from PATH
	{
		"mkdir test; echo 'echo foo_interp_missing' >test/a; PATH=$PWD/test source a; . test/a",
		"foo_interp_missing\nfoo_interp_missing\n",
	},

	// source with set and shift
	{
		"echo 'set -- d e f' >a; source a; echo $@",
		"d e f\n",
	},
	{
		"echo 'echo $@' >a; set -- b c; source a; echo $@",
		"b c\nb c\n",
	},
	{
		"echo 'echo $@' >a; set -- b c; source a d e; echo $@",
		"d e\nb c\n",
	},
	{
		"echo 'shift; echo $@' >a; set -- b c; source a d e; echo $@",
		"e\nb c\n",
	},
	{
		"echo 'shift' >a; set -- b c; source a; echo $@",
		"c\n",
	},
	{
		"echo 'shift; set -- $@' >a; set -- b c; source a d e; echo $@",
		"e\n",
	},
	{
		"echo 'set -- g f'>b; echo 'set -- d e f; echo $@; source b;' >a; source a; echo $@",
		"d e f\ng f\n",
	},
	{
		"echo 'set -- g f'>b; echo 'echo $@; set -- d e f; source b;' >a; source a b c; echo $@",
		"b c\ng f\n",
	},
	{
		"echo 'shift; echo $@' >b; echo 'shift; echo $@; source b' >a; source a b c d; echo $@",
		"c d\nd\n\n",
	},
	{
		"echo 'set -- b c d' >b; echo 'source b' >a; set -- a; source a; echo $@",
		"b c d\n",
	},
	{
		"echo 'echo $@' >b; echo 'set -- b c d; source b' >a; set -- a; source a; echo $@",
		"b c d\nb c d\n",
	},
	{
		"echo 'shift; echo $@' >b; echo 'shift; echo $@; source b c d' >a; set -- a b; source a; echo $@",
		"b\nd\nb\n",
	},
	{
		"echo 'set -- a b c' >b; echo 'echo $@; source b; echo $@' >a; source a; echo $@",
		"\na b c\na b c\n",
	},

	// indexed arrays
	{
		"a=foo_interp_missing; echo ${a[0]} ${a[@]} ${a[x]}; echo ${a[1]}",
		"foo_interp_missing foo_interp_missing foo_interp_missing\n\n",
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
	{
		`a=(b); echo ${a[3]}`,
		"\n",
	},
	{
		`a=(b); echo ${a[-2]}`,
		"negative array index\n #JUSTERR",
	},
	// TODO: also test with gaps in arrays.
	{
		`a=([0]=' x ' [1]=' y '); for v in "${a[@]}"; do echo "$v"; done`,
		" x \n y \n",
	},
	{
		`a=([0]=' x ' [1]=' y '); for v in "${a[*]}"; do echo "$v"; done`,
		" x   y \n",
	},
	{
		`a=([0]=' x ' [1]=' y '); for v in "${!a[@]}"; do echo "$v"; done`,
		"0\n1\n",
	},
	{
		`a=([0]=' x ' [1]=' y '); for v in "${!a[*]}"; do echo "$v"; done`,
		"0 1\n",
	},

	// associative arrays
	{
		`a=foo_interp_missing; echo ${a[""]} ${a["x"]}`,
		"foo_interp_missing foo_interp_missing\n",
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
		`declare -A a=([x]=b [y]=c); for e in ${a[@]}; do echo $e; done | sort`,
		"b\nc\n",
	},
	{
		`declare -A a=([y]=b [x]=c); for e in ${a[*]}; do echo $e; done | sort`,
		"b\nc\n",
	},
	{
		`declare -A a=([x]=a); a["y"]=d; a["x"]=c; for e in ${a[@]}; do echo $e; done | sort`,
		"c\nd\n",
	},
	{
		`declare -A a=([x]=a); a[y]=d; a[x]=c; for e in ${a[@]}; do echo $e; done | sort`,
		"c\nd\n",
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
	{
		`declare -A a=(['a  1']=' x ' ['b  2']=' y '); for v in "${a[@]}"; do echo "$v"; done | sort`,
		" x \n y \n",
	},
	{
		`declare -A a=(['a  1']=' x ' ['b  2']=' y '); for v in "${a[*]}"; do echo "$v"; done | sort`,
		" x   y \n",
	},
	{
		`declare -A a=(['a  1']=' x ' ['b  2']=' y '); for v in "${!a[@]}"; do echo "$v"; done | sort`,
		"a  1\nb  2\n",
	},
	{
		`declare -A a=(['a  1']=' x ' ['b  2']=' y '); for v in "${!a[*]}"; do echo "$v"; done | sort`,
		"a  1 b  2\n",
	},

	// weird assignments
	{"a=b; a=(c d); echo ${a[@]}", "c d\n"},
	{"a=(b c); a=d; echo ${a[@]}", "d c\n"},
	{"declare -A a=([x]=b [y]=c); a=d; for e in ${a[@]}; do echo $e; done | sort", "b\nc\nd\n"},
	{"i=3; a=b; a[i]=x; echo ${a[@]}", "b x\n"},
	{"i=3; declare a=(b); a[i]=x; echo ${!a[@]}", "0 3\n"},
	{"i=3; declare -A a=(['x']=b); a[i]=x; for e in ${!a[@]}; do echo $e; done | sort", "i\nx\n"},

	// declare
	{"declare -B foo_interp_missing", "declare: invalid option \"-B\"\nexit status 2 #JUSTERR"},
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
	{"declare =bar_interp_missing", "declare: invalid name \"\"\nexit status 1 #JUSTERR"},
	{"declare $unset=$unset", "declare: invalid name \"\"\nexit status 1 #JUSTERR"},

	// export
	{"declare foo_interp_missing=bar_interp_missing; $ENV_PROG | grep '^foo_interp_missing='", "exit status 1"},
	{"declare -x foo_interp_missing=bar_interp_missing; $ENV_PROG | grep '^foo_interp_missing='", "foo_interp_missing=bar_interp_missing\n"},
	{"export foo_interp_missing=bar_interp_missing; $ENV_PROG | grep '^foo_interp_missing='", "foo_interp_missing=bar_interp_missing\n"},
	{"foo_interp_missing=bar_interp_missing; export foo_interp_missing; $ENV_PROG | grep '^foo_interp_missing='", "foo_interp_missing=bar_interp_missing\n"},
	{"export foo_interp_missing=bar_interp_missing; foo_interp_missing=baz; $ENV_PROG | grep '^foo_interp_missing='", "foo_interp_missing=baz\n"},
	{"export foo_interp_missing=bar_interp_missing; readonly foo_interp_missing=baz; $ENV_PROG | grep '^foo_interp_missing='", "foo_interp_missing=baz\n"},
	{"export foo_interp_missing=(1 2); $ENV_PROG | grep '^foo_interp_missing='", "exit status 1"},
	{"declare -A foo_interp_missing=([a]=b); export foo_interp_missing; $ENV_PROG | grep '^foo_interp_missing='", "exit status 1"},
	{"export foo_interp_missing=(b c); foo_interp_missing=x; $ENV_PROG | grep '^foo_interp_missing='", "exit status 1"},
	{"foo_interp_missing() { bar_interp_missing=foo_interp_missing; export bar_interp_missing; }; foo_interp_missing; $ENV_PROG | grep ^bar_interp_missing=", "bar_interp_missing=foo_interp_missing\n"},
	{"foo_interp_missing() { export bar_interp_missing; }; bar_interp_missing=foo_interp_missing; foo_interp_missing; $ENV_PROG | grep ^bar_interp_missing=", "bar_interp_missing=foo_interp_missing\n"},
	{"foo_interp_missing() { export bar_interp_missing; }; foo_interp_missing; bar_interp_missing=foo_interp_missing; $ENV_PROG | grep ^bar_interp_missing=", "bar_interp_missing=foo_interp_missing\n"},
	{"foo_interp_missing() { export bar_interp_missing=foo_interp_missing; }; foo_interp_missing; readonly bar_interp_missing; $ENV_PROG | grep ^bar_interp_missing=", "bar_interp_missing=foo_interp_missing\n"},

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
	{
		`f() { local x; [[ -v x ]] && echo set || echo unset; }; f`,
		"unset\n",
	},
	{
		`f() { local x=; [[ -v x ]] && echo set || echo unset; }; f`,
		"set\n",
	},
	{
		`export x=before; f() { local x; export x=after; $ENV_PROG | grep '^x='; }; f; echo $x`,
		"x=after\nbefore\n",
	},

	// unset global from inside function
	{"f() { unset foo_interp_missing; echo $foo_interp_missing; }; foo_interp_missing=bar_interp_missing; f", "\n"},
	{"f() { unset foo_interp_missing; }; foo_interp_missing=bar_interp_missing; f; echo $foo_interp_missing", "\n"},

	// name references
	{"declare -n foo_interp_missing=bar_interp_missing; bar_interp_missing=etc; [[ -R foo_interp_missing ]]", ""},
	{"declare -n foo_interp_missing=bar_interp_missing; bar_interp_missing=etc; [ -R foo_interp_missing ]", ""},
	{"nameref foo_interp_missing=bar_interp_missing; bar_interp_missing=etc; [[ -R foo_interp_missing ]]", " #IGNORE"},
	{"declare foo_interp_missing=bar_interp_missing; bar_interp_missing=etc; [[ -R foo_interp_missing ]]", "exit status 1"},
	{
		"declare -n foo_interp_missing=bar_interp_missing; bar_interp_missing=etc; echo $foo_interp_missing; bar_interp_missing=zzz; echo $foo_interp_missing",
		"etc\nzzz\n",
	},
	{
		"declare -n foo_interp_missing=bar_interp_missing; bar_interp_missing=(x y); echo ${foo_interp_missing[1]}; bar_interp_missing=(a b); echo ${foo_interp_missing[1]}",
		"y\nb\n",
	},
	{
		"declare -n foo_interp_missing=bar_interp_missing; bar_interp_missing=etc; echo $foo_interp_missing; unset bar_interp_missing; echo $foo_interp_missing",
		"etc\n\n",
	},
	{
		"declare -n a1=a2 a2=a3 a3=a4; a4=x; echo $a1 $a3",
		"x x\n",
	},
	{
		"declare -n foo_interp_missing=bar_interp_missing bar_interp_missing=foo_interp_missing; echo $foo_interp_missing",
		"\n #IGNORE",
	},
	{
		"declare -n foo_interp_missing=bar_interp_missing; echo $foo_interp_missing",
		"\n",
	},
	{
		"declare -n foo_interp_missing=bar_interp_missing; echo ${!foo_interp_missing}",
		"bar_interp_missing\n",
	},
	{
		"declare -n foo_interp_missing=bar_interp_missing; bar_interp_missing=etc; echo $foo_interp_missing; echo ${!foo_interp_missing}",
		"etc\nbar_interp_missing\n",
	},
	{
		"declare -n foo_interp_missing=bar_interp_missing; bar_interp_missing=etc; foo_interp_missing=xxx; echo $foo_interp_missing $bar_interp_missing",
		"xxx xxx\n",
	},
	{
		"declare -n foo_interp_missing=bar_interp_missing; foo_interp_missing=xxx; echo $foo_interp_missing $bar_interp_missing",
		"xxx xxx\n",
	},
	// TODO: figure this one out
	//{
	//        "declare -n foo_interp_missing=bar_interp_missing bar_interp_missing=baz; foo_interp_missing=xxx; echo $foo_interp_missing $bar_interp_missing; echo $baz",
	//        "xxx xxx\nxxx\n",
	//},
	{
		"echo ${!@}-${!*}-${!1}; set -- foo_interp_missing; echo ${!@}-${!*}-${!1}; foo_interp_missing=value; echo ${!@}-${!*}-${!1}",
		"--\n--\nvalue-value-value\n",
	},

	// read-only vars
	{"declare -r foo_interp_missing=bar_interp_missing; echo $foo_interp_missing", "bar_interp_missing\n"},
	{"readonly foo_interp_missing=bar_interp_missing; echo $foo_interp_missing", "bar_interp_missing\n"},
	{"readonly foo_interp_missing=bar_interp_missing; export foo_interp_missing; echo $foo_interp_missing", "bar_interp_missing\n"},
	{"readonly foo_interp_missing=bar_interp_missing; readonly bar_interp_missing=foo_interp_missing; export foo_interp_missing bar_interp_missing; echo $bar_interp_missing", "foo_interp_missing\n"},
	{
		"a=b; a=c; echo $a; readonly a; a=d",
		"c\na: readonly variable\nexit status 1 #JUSTERR",
	},
	{
		"declare -r foo_interp_missing=bar_interp_missing; foo_interp_missing=etc",
		"foo_interp_missing: readonly variable\nexit status 1 #JUSTERR",
	},
	{
		"declare -r foo_interp_missing=bar_interp_missing; export foo_interp_missing=",
		"foo_interp_missing: readonly variable\nexit status 1 #JUSTERR",
	},
	{
		"readonly foo_interp_missing=bar_interp_missing; foo_interp_missing=etc",
		"foo_interp_missing: readonly variable\nexit status 1 #JUSTERR",
	},
	{
		"foo_interp_missing() { bar_interp_missing=foo_interp_missing; readonly bar_interp_missing; }; foo_interp_missing; bar_interp_missing=bar_interp_missing",
		"bar_interp_missing: readonly variable\nexit status 1 #JUSTERR",
	},
	{
		"foo_interp_missing() { readonly bar_interp_missing; }; foo_interp_missing; bar_interp_missing=foo_interp_missing",
		"bar_interp_missing: readonly variable\nexit status 1 #JUSTERR",
	},
	{
		"foo_interp_missing() { readonly bar_interp_missing=foo_interp_missing; }; foo_interp_missing; export bar_interp_missing; $ENV_PROG | grep '^bar_interp_missing='",
		"bar_interp_missing=foo_interp_missing\n",
	},

	// multiple var modes at once
	{
		"declare -r -x foo_interp_missing=bar_interp_missing; $ENV_PROG | grep '^foo_interp_missing='",
		"foo_interp_missing=bar_interp_missing\n",
	},
	{
		"declare -r -x foo_interp_missing=bar_interp_missing; foo_interp_missing=x",
		"foo_interp_missing: readonly variable\nexit status 1 #JUSTERR",
	},

	// globbing
	{"echo .", ".\n"},
	{"echo ..", "..\n"},
	{"echo ./.", "./.\n"},
	{
		">a.x >b.x >c.x; echo *.x; rm a.x b.x c.x",
		"a.x b.x c.x\n",
	},
	{
		`>a.x; echo '*.x' "*.x"; rm a.x`,
		"*.x *.x\n",
	},
	{
		`>a.x >b.y; echo *'.'x; rm a.x`,
		"a.x\n",
	},
	{
		`>a.x; echo *'.x' "a."* '*'.x; rm a.x`,
		"a.x a.x *.x\n",
	},
	{
		"echo *.x; echo foo_interp_missing *.y bar_interp_missing",
		"*.x\nfoo_interp_missing *.y bar_interp_missing\n",
	},
	{
		"mkdir a; >a/b.x; echo */*.x | sed 's@\\\\@/@g'; cd a; echo *.x",
		"a/b.x\nb.x\n",
	},
	{
		"mkdir -p a/b/c; echo a/* | sed 's@\\\\@/@g'",
		"a/b\n",
	},
	{
		">.hidden >a; echo *; echo .h*; rm .hidden a",
		"a\n.hidden\n",
	},
	{
		`mkdir d; >d/.hidden >d/a; set -- "$(echo d/*)" "$(echo d/.h*)"; echo ${#1} ${#2}; rm -r d`,
		"3 9\n",
	},
	{
		"mkdir -p a/b/c; echo a/** | sed 's@\\\\@/@g'",
		"a/b\n",
	},
	{
		"shopt -s globstar; mkdir -p a/b/c; echo a/** | sed 's@\\\\@/@g'",
		"a/ a/b a/b/c\n",
	},
	{
		"shopt -s globstar; mkdir -p a/b/c; echo **/c | sed 's@\\\\@/@g'",
		"a/b/c\n",
	},
	{
		"shopt -s globstar; mkdir -p a/b; touch c; echo ** | sed 's@\\\\@/@g'",
		"a a/b c\n",
	},
	{
		"shopt -s globstar; mkdir -p a/b; touch c; echo **/ | sed 's@\\\\@/@g'",
		"a/ a/b/\n",
	},
	{
		"shopt -s globstar; mkdir -p a/b/c a/d; echo ** | sed 's@\\\\@/@g'",
		"a a/b a/b/c a/d\n",
	},
	{
		"mkdir foo; touch foo/bar; echo */bar */bar/ | sed 's@\\\\@/@g'",
		"foo/bar */bar/\n",
	},
	{
		"shopt -s nullglob; touch existing-1; echo missing-* existing-*",
		"existing-1\n",
	},
	// Extended globbing is not supported
	{"ls ab+(2|3).txt", "extended globbing is not supported\nexit status 1 #JUSTERR"},
	{"echo *(/)", "extended globbing is not supported\nexit status 1 #JUSTERR"},
	// Ensure that setting nullglob does not return invalid globs as null
	// strings.
	{
		"shopt -s nullglob; [ -n butter ] && echo bubbles",
		"bubbles\n",
	},
	{
		"cat <<EOF\n{foo_interp_missing,bar_interp_missing}\nEOF",
		"{foo_interp_missing,bar_interp_missing}\n",
	},
	{
		"cat <<EOF\n*.go\nEOF",
		"*.go\n",
	},
	{
		"mkdir -p a/b a/c; echo ./a/* | sed 's@\\\\@/@g'",
		"./a/b ./a/c\n",
	},
	{
		"mkdir -p a/b a/c d; cd d; echo ../a/* | sed 's@\\\\@/@g'",
		"../a/b ../a/c\n",
	},
	{
		"mkdir x-d1 x-d2; >x-f; echo x-*/ | sed 's@\\\\@/@g'",
		"x-d1/ x-d2/\n",
	},
	{
		"mkdir x-d1 x-d2; >x-f; echo ././x-*/// | sed 's@\\\\@/@g'",
		"././x-d1/ ././x-d2/\n",
	},
	{
		"mkdir -p x-d1/a x-d2/b; >x-f; echo x-*/* | sed 's@\\\\@/@g'",
		"x-d1/a x-d2/b\n",
	},
	{
		"mkdir -p foo_interp_missing/bar_interp_missing; ln -s foo_interp_missing sym; echo sy*/; echo sym/b*",
		"sym/\nsym/bar_interp_missing\n",
	},
	{
		">foo_interp_missing; ln -s foo_interp_missing sym; echo sy*; echo sy*/",
		"sym\nsy*/\n",
	},
	{
		"mkdir x-d; >x-f; test -d $PWD/x-*/",
		"",
	},
	{
		"mkdir dir; >dir/x-f; ln -s dir sym; cd sym; test -f $PWD/x-*",
		"",
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

	// tilde expansion
	{
		"[[ '~/foo_interp_missing' == ~/foo_interp_missing ]] || [[ ~/foo_interp_missing == '~/foo_interp_missing' ]]",
		"exit status 1",
	},
	{
		"case '~/foo_interp_missing' in ~/foo_interp_missing) echo match ;; esac",
		"",
	},
	{
		"a=~/foo_interp_missing; [[ $a == '~/foo_interp_missing' ]]",
		"exit status 1",
	},
	{
		`a=$(echo "~/foo_interp_missing"); [[ $a == '~/foo_interp_missing' ]]`,
		"",
	},

	// /dev/null
	{"echo foo_interp_missing >/dev/null", ""},
	{"cat </dev/null", ""},

	// time - real would be slow and flaky; see TestElapsedString
	{"{ time; } |& wc | tr -s ' '", " 4 6 42\n"},
	{"{ time echo -n; } |& wc | tr -s ' '", " 4 6 42\n"},
	{"{ time -p; } |& wc | tr -s ' '", " 3 6 29\n"},
	{"{ time -p echo -n; } |& wc | tr -s ' '", " 3 6 29\n"},

	// exec
	{"exec", ""},
	{
		"exec builtin echo foo_interp_missing",
		"\"builtin\": executable file not found in $PATH\nexit status 127 #JUSTERR",
	},
	{
		"exec $GOSH_PROG 'echo foo_interp_missing'; echo bar_interp_missing",
		"foo_interp_missing\n",
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
		"read -rX",
		"read: invalid option \"-X\"\nexit status 2 #JUSTERR",
	},
	{
		"read 0ab",
		"read: invalid identifier \"0ab\"\nexit status 2 #JUSTERR",
	},
	{
		"read <<< foo_interp_missing; echo $REPLY",
		"foo_interp_missing\n",
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
		"read a_0 <<< foo_interp_missing; echo $a_0",
		"foo_interp_missing\n",
	},
	{
		"read a b <<< 'foo_interp_missing  bar_interp_missing  baz  '; echo \"$a\"; echo \"$b\"",
		"foo_interp_missing\nbar_interp_missing  baz\n",
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
	{
		"read -p",
		"read: -p: option requires an argument\nexit status 2 #JUSTERR",
	},
	{
		"read -X -p",
		"read: invalid option \"-X\"\nexit status 2 #JUSTERR",
	},
	{
		"read -p 'Display me as a prompt. Continue? (y/n) ' choice <<< 'y'; echo $choice",
		"Display me as a prompt. Continue? (y/n) y\n #IGNORE bash requires a terminal",
	},
	{
		"read -r -p 'Prompt and raw flag together: ' a <<< '\\a\\b\\c'; echo $a",
		"Prompt and raw flag together: \\a\\b\\c\n #IGNORE bash requires a terminal",
	},

	// getopts
	{
		"getopts",
		"getopts: usage: getopts optstring name [arg ...]\nexit status 2",
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
		"getopts abc opt foo_interp_missing -a; echo $opt; echo $OPTIND",
		"?\n1\n",
	},
	{
		"getopts abc opt -a foo_interp_missing; echo $opt; echo $OPTIND",
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
		"OPTIND=foo_interp_missing; getopts abc opt -a -b -c; echo $opt;",
		"a\n",
	},
	{
		"while getopts ab:c opt -c -b arg -a foo_interp_missing; do echo $opt $OPTARG $OPTIND; done",
		"c 2\nb arg 4\na 5\n",
	},
	{
		"while getopts abc opt -ba -c foo_interp_missing; do echo $opt $OPTARG $OPTIND; done",
		"b 1\na 2\nc 3\n",
	},
	{
		"a() { while getopts abc: opt; do echo $opt $OPTARG; done }; a -a -b -c arg",
		"a\nb\nc arg\n",
	},
	// mapfile
	{
		"mapfile <<EOF\na\nb\nc\nEOF\n" + `for x in "${MAPFILE[@]}"; do echo "$x"; done`,
		"a\n\nb\n\nc\n\n",
	},
	{
		"mapfile -t <<EOF\na\nb\nc\nEOF\n" + `for x in "${MAPFILE[@]}"; do echo "$x"; done`,
		"a\nb\nc\n",
	},
	{
		"mapfile -t -d b <<EOF\nabc\nEOF\n" + `for x in "${MAPFILE[@]}"; do echo "$x"; done`,
		"a\nc\n\n",
	},
	{
		"mapfile -t butter <<EOF\na\nb\nc\nEOF\n" + `for x in "${butter[@]}"; do echo "$x"; done`,
		"a\nb\nc\n",
	},
}

var runTestsUnix = []runTest{
	{"[[ -n $PPID && $PPID -ge 0 ]]", ""}, // can be 0 if running as the init process
	{
		// no root user on windows
		"[[ ~root == '~root' ]]",
		"exit status 1",
	},

	// windows does not support paths with '*'
	{
		"mkdir -p '*/a.z' 'b/a.z'; cd '*'; set -- *.z; echo $#",
		"1\n",
	},
	{
		"mkdir -p 'a-*/d'; test -d $PWD/a-*/*",
		"",
	},

	// no fifos on windows
	{
		"[ -p a ] && echo x; mkfifo a; [ -p a ] && echo y",
		"y\n",
	},
	{
		"[[ -p a ]] && echo x; mkfifo a; [[ -p a ]] && echo y",
		"y\n",
	},

	{"sh() { :; }; sh -c 'echo foo_interp_missing'", ""},
	{"sh() { :; }; command sh -c 'echo foo_interp_missing'", "foo_interp_missing\n"},

	// chmod is practically useless on Windows
	{
		"[ -x a ] && echo x; >a; chmod 0755 a; [ -x a ] && echo y",
		"y\n",
	},
	{
		"[[ -x a ]] && echo x; >a; chmod 0755 a; [[ -x a ]] && echo y",
		"y\n",
	},
	{
		">a; [ -k a ] && echo x; chmod +t a; [ -k a ] && echo y",
		"y\n",
	},
	{
		">a; [ -u a ] && echo x; chmod u+s a; [ -u a ] && echo y",
		"y\n",
	},
	{
		">a; [ -g a ] && echo x; chmod g+s a; [ -g a ] && echo y",
		"y\n",
	},
	{
		">a; [[ -k a ]] && echo x; chmod +t a; [[ -k a ]] && echo y",
		"y\n",
	},
	{
		">a; [[ -u a ]] && echo x; chmod u+s a; [[ -u a ]] && echo y",
		"y\n",
	},
	{
		">a; [[ -g a ]] && echo x; chmod g+s a; [[ -g a ]] && echo y",
		"y\n",
	},
	{
		`mkdir a; chmod 0100 a; cd a`,
		"",
	},
	// Note that these will succeed if we're root.
	{
		`mkdir a; chmod 0000 a; cd a && test $UID -ne 0`,
		"exit status 1 #JUSTERR",
	},
	{
		`mkdir a; chmod 0222 a; cd a && test $UID -ne 0`,
		"exit status 1 #JUSTERR",
	},
	{
		`mkdir a; chmod 0444 a; cd a && test $UID -ne 0`,
		"exit status 1 #JUSTERR",
	},
	{
		`mkdir a; chmod 0010 a; cd a && test $UID -ne 0`,
		"exit status 1 #JUSTERR",
	},
	{
		`mkdir a; chmod 0001 a; cd a && test $UID -ne 0`,
		"exit status 1 #JUSTERR",
	},
	{
		`unset UID`,
		"UID: readonly variable\n #IGNORE",
	},
	{
		`test -n "$EUID" && echo OK`,
		"OK\n",
	},
	{
		`set EUID=newvalue; test EUID != newvalue && echo OK || echo EUID=$EUID`,
		"OK\n",
	},
	{
		`unset EUID`,
		"EUID: readonly variable\n #IGNORE",
	},
	// GID is not set in bash
	{
		`unset GID`,
		"GID: readonly variable\n #IGNORE",
	},
	{
		`[[ -z $GID ]] && echo "GID not set"`,
		"exit status 1 #JUSTERR #IGNORE",
	},

	// Unix-y PATH
	{
		"PATH=; bash -c 'echo foo_interp_missing'",
		"\"bash\": executable file not found in $PATH\nexit status 127 #JUSTERR",
	},
	{
		"cd /; sure/is/missing",
		"stat /sure/is/missing: no such file or directory\nexit status 127 #JUSTERR",
	},
	{
		"echo '#!/bin/sh\necho b' >a; chmod 0755 a; PATH=; a",
		"b\n",
	},
	{
		"mkdir c; cd c; echo '#!/bin/sh\necho b' >a; chmod 0755 a; PATH=; a",
		"b\n",
	},
	{
		"mkdir c; echo '#!/bin/sh\necho b' >c/a; chmod 0755 c/a; c/a",
		"b\n",
	},
	{
		"GOSH_CMD=lookpath $GOSH_PROG",
		"sh found\n",
	},

	// error strings which are too different on Windows
	{
		"echo foo_interp_missing >/shouldnotexist/file",
		"open /shouldnotexist/file: no such file or directory\nexit status 1 #JUSTERR",
	},
	{
		"set -e; echo foo_interp_missing >/shouldnotexist/file; echo foo_interp_missing",
		"open /shouldnotexist/file: no such file or directory\nexit status 1 #JUSTERR",
	},

	// process substitution; named pipes (fifos) are a TODO for windows
	{
		"sed 's/o/e/g' <(echo foo_interp_missing bar_interp_missing)",
		"fee_interp_missing bar_interp_missing\n",
	},
	{
		"cat <(echo foo_interp_missing) <(echo bar_interp_missing) <(echo baz)",
		"foo_interp_missing\nbar_interp_missing\nbaz\n",
	},
	{
		"cat <(cat <(echo nested))",
		"nested\n",
	},
	{
		"echo foo_interp_missing bar_interp_missing > >(sed 's/o/e/g')",
		"fee_interp_missing bar_interp_missing\n",
	},
	{
		"echo foo_interp_missing bar_interp_missing | tee >(sed 's/o/e/g') >/dev/null",
		"fee_interp_missing bar_interp_missing\n",
	},
	{
		"echo nested > >(cat > >(cat))",
		"nested\n",
	},
	// echo trace
	{
		`set -x; animals=("dog", "cat", "otter"); echo "hello ${animals[*]}"`,
		`+ animals=("dog", "cat", "otter")
+ echo 'hello dog, cat, otter'
hello dog, cat, otter
`,
	},
	{
		`set -x; s="always print a decimal point for %e, %E, %f, %F, %g and %G; do not remove trailing zeros for %g and %G"; echo "$s"`,
		`+ s='always print a decimal point for %e, %E, %f, %F, %g and %G; do not remove trailing zeros for %g and %G'
+ echo 'always print a decimal point for %e, %E, %f, %F, %g and %G; do not remove trailing zeros for %g and %G'
always print a decimal point for %e, %E, %f, %F, %g and %G; do not remove trailing zeros for %g and %G
`,
	},
	{
		`set -x
x=without; echo "$x"
x="double quote"; echo "$x"
x='single quote'; echo "$x"`,
		`+ x=without
+ echo without
without
+ x='double quote'
+ echo 'double quote'
double quote
+ x='single quote'
+ echo 'single quote'
single quote
`,
	},
	// for trace
	{
		`set -x
exec >/dev/null
echo "trace should go to stderr"`,
		`+ exec
+ echo 'trace should go to stderr'
`,
	},
	{
		`set -x
animals=(dog, cat, otter)
for i in ${animals[@]}
do
   echo "hello ${i}"
done
`,
		`+ animals=(dog, cat, otter)
+ for i in ${animals[@]}
+ echo 'hello dog,'
hello dog,
+ for i in ${animals[@]}
+ echo 'hello cat,'
hello cat,
+ for i in ${animals[@]}
+ echo 'hello otter'
hello otter
`,
	},
	{
		`set -x
loop() {
    for i do
        echo "something with $i"
    done
}
loop 1 2 3`,
		`+ loop 1 2 3
+ for i in "$@"
+ echo 'something with 1'
something with 1
+ for i in "$@"
+ echo 'something with 2'
something with 2
+ for i in "$@"
+ echo 'something with 3'
something with 3
`,
	},
	{
		`set -x; animals=(dog, cat, otter); for i in ${animals[@]}; do echo "hello ${i}"; done`,
		`+ animals=(dog, cat, otter)
+ for i in ${animals[@]}
+ echo 'hello dog,'
hello dog,
+ for i in ${animals[@]}
+ echo 'hello cat,'
hello cat,
+ for i in ${animals[@]}
+ echo 'hello otter'
hello otter
`,
	},
	{
		`set -x; a=x"y"$z b=(foo_interp_missing bar_interp_missing $none '')`,
		"+ a=xy\n+ b=(foo_interp_missing bar_interp_missing $none '')\n",
	},
	{
		`set -x; for i in a b; do echo $i; done`,
		`+ for i in a b
+ echo a
a
+ for i in a b
+ echo b
b
`,
	},
	{
		`set -x; for i in $none_a $none_b; do echo $i; done`,
		``,
	},
	// case trace
	{
		`set -x; pet=dog; case $pet in 'dog') echo "barks";; *) echo "unknown";; esac`,
		`+ pet=dog
+ case $pet in
+ echo barks
barks
`,
	},
	{
		`set -x
pet="dog"
case $pet in
  dog)
    echo "barks"
    ;;
  *)
    echo "unknown"
    ;;
esac`,
		`+ pet=dog
+ case $pet in
+ echo barks
barks
`,
	},
	// arithmetic
	{
		`set -x
a=$(( 4 + 5 )); echo $a
a=$((3+5)); echo $a`,
		`+ a=9
+ echo 9
9
+ a=8
+ echo 8
8
`,
	},
	{
		`set -x;
let a=5+4; echo $a
let "a = 5 + 4"; echo $a
let a++; echo $a`,
		`+ let a=5+4
+ echo 9
9
+ let 'a = 5 + 4'
+ echo 9
9
+ let a++
+ echo 10
10
`,
	},
	// functions
	{
		`set -x; function with_function () { echo 'hello, world'; }; with_function`,
		`+ with_function
+ echo 'hello, world'
hello, world
`,
	},
	{
		`set -x; without_function () { echo 'hello, world'; }; without_function`,
		`+ without_function
+ echo 'hello, world'
hello, world
`,
	},
	{
		// globbing wildcard as function name
		`@() { echo "$@"; }; @ lala; function +() { echo "$@"; }; + foo_interp_missing`,
		"lala\nfoo_interp_missing\n",
	},
	{
		`      @() { echo "$@"; }; @ lala;`,
		"lala\n",
	},
	{
		// globbing wildcard as function name but with space after the name
		`+ () { echo "$@"; }; + foo_interp_missing; @ () { echo "$@"; }; @ lala; ? () { echo "$@"; }; ? bar_interp_missing`,
		"foo_interp_missing\nlala\nbar_interp_missing\n",
	},
	// mapfile, no process substitution yet on Windows
	{
		`mapfile -t -d "" < <(printf "a\0b\n"); for x in "${MAPFILE[@]}"; do echo "$x"; done`,
		"a\nb\n\n",
	},
	// Windows does not support having a `\n` in a filename
	{
		`> $'bar\nbaz'; echo bar*baz`,
		"bar\nbaz\n",
	},
}

var runTestsWindows = []runTest{
	{"[[ -n $PPID || $PPID -gt 0 ]]", ""}, // os.Getppid can be 0 on windows
	{"cmd() { :; }; cmd /c 'echo foo_interp_missing'", ""},
	{"cmd() { :; }; command cmd /c 'echo foo_interp_missing'", "foo_interp_missing\r\n"},
	{
		"GOSH_CMD=lookpath $GOSH_PROG",
		"cmd found\n",
	},
}

// These tests are specific to 64-bit architectures, and that's fine. We don't
// need to add explicit versions for 32-bit.
var runTests64bit = []runTest{
	{"printf %i,%u -3 -3", "-3,18446744073709551613"},
	{"printf %o -3", "1777777777777777777775"},
	{"printf %x -3", "fffffffffffffffd"},
}

func init() {
	if runtime.GOOS == "windows" {
		runTests = append(runTests, runTestsWindows...)
	} else { // Unix-y
		runTests = append(runTests, runTestsUnix...)
	}
	if bits.UintSize == 64 {
		runTests = append(runTests, runTests64bit...)
	}
}

// ln -s: wine doesn't implement symlinks; see https://bugs.winehq.org/show_bug.cgi?id=44948
var skipOnWindows = regexp.MustCompile(`ln -s`)

// process substitutions seemflaky on mac; see https://github.com/mvdan/sh/issues/576
var skipOnMac = regexp.MustCompile(`>\(|<\(`)

func skipIfUnsupported(tb testing.TB, src string) {
	switch {
	case runtime.GOOS == "windows" && skipOnWindows.MatchString(src):
		tb.Skipf("skipping non-portable test on windows")
	case runtime.GOOS == "darwin" && skipOnMac.MatchString(src):
		tb.Skipf("skipping non-portable test on mac")
	}
}

func TestRunnerRun(t *testing.T) {
	t.Parallel()

	p := syntax.NewParser()
	for _, c := range runTests {
		c := c
		t.Run("", func(t *testing.T) {
			skipIfUnsupported(t, c.in)

			// Parse first, as we reuse a single parser.
			file := parse(t, p, c.in)

			t.Parallel()

			tdir := t.TempDir()
			var cb concBuffer
			r, err := interp.New(interp.Dir(tdir), interp.StdIO(nil, &cb, &cb),
				// TODO: why does this make some tests hang?
				// interp.Env(expand.ListEnviron(append(os.Environ(),
				// 	"FOO_INTERP_MISSING_NULL_BAR_INTERP_MISSING=foo_interp_missing\x00bar_interp_missing")...)),
				interp.OpenHandler(testOpenHandler),
				interp.ExecHandlers(testExecHandler),
			)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), runnerRunTimeout)
			defer cancel()
			if err := r.Run(ctx, file); err != nil {
				cb.WriteString(err.Error())
			}
			want := c.want
			if i := strings.Index(want, " #"); i >= 0 {
				want = want[:i]
			}
			if got := cb.String(); got != want {
				if len(got) > 200 {
					got = "…" + got[len(got)-200:]
				}
				t.Fatalf("wrong output in %q:\nwant: %q\ngot:  %q",
					c.in, want, got)
			}
		})
	}
}

func readLines(hc interp.HandlerContext) ([][]byte, error) {
	bs, err := io.ReadAll(hc.Stdin)
	if err != nil {
		return nil, err
	}
	if runtime.GOOS == "windows" {
		bs = bytes.ReplaceAll(bs, []byte("\r\n"), []byte("\n"))
	}
	bs = bytes.TrimSuffix(bs, []byte("\n"))
	return bytes.Split(bs, []byte("\n")), nil
}

func absPath(dir, path string) string {
	if path == "" {
		return ""
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	return filepath.Clean(path) // TODO: this clean is likely unnecessary
}

var testBuiltinsMap = map[string]func(interp.HandlerContext, []string) error{
	"cat": func(hc interp.HandlerContext, args []string) error {
		if len(args) == 0 {
			if hc.Stdin == nil || hc.Stdout == nil {
				return nil
			}
			_, err := io.Copy(hc.Stdout, hc.Stdin)
			return err
		}
		for _, arg := range args {
			path := absPath(hc.Dir, arg)
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			_, err = io.Copy(hc.Stdout, f)
			f.Close()
			if err != nil {
				return err
			}
		}
		return nil
	},
	"wc": func(hc interp.HandlerContext, args []string) error {
		bs, err := io.ReadAll(hc.Stdin)
		if err != nil {
			return err
		}
		if len(args) == 0 {
			fmt.Fprintf(hc.Stdout, "%7d", bytes.Count(bs, []byte("\n")))
			fmt.Fprintf(hc.Stdout, "%8d", len(bytes.Fields(bs)))
			fmt.Fprintf(hc.Stdout, "%8d\n", len(bs))
		} else if args[0] == "-c" {
			fmt.Fprintln(hc.Stdout, len(bs))
		} else if args[0] == "-l" {
			fmt.Fprintln(hc.Stdout, bytes.Count(bs, []byte("\n")))
		}
		return nil
	},
	"tr": func(hc interp.HandlerContext, args []string) error {
		if len(args) != 2 || len(args[1]) != 1 {
			return fmt.Errorf("usage: tr [-s -d] [character]")
		}
		squeeze := args[0] == "-s"
		char := args[1][0]
		bs, err := io.ReadAll(hc.Stdin)
		if err != nil {
			return err
		}
		for {
			i := bytes.IndexByte(bs, char)
			if i < 0 {
				hc.Stdout.Write(bs) // remaining
				break
			}
			hc.Stdout.Write(bs[:i]) // up to char
			bs = bs[i+1:]

			bs = bytes.TrimLeft(bs, string(char)) // remove repeats
			if squeeze {
				hc.Stdout.Write([]byte{char})
			}
		}
		return nil
	},
	"sort": func(hc interp.HandlerContext, args []string) error {
		lines, err := readLines(hc)
		if err != nil {
			return err
		}
		sort.Slice(lines, func(i, j int) bool {
			return bytes.Compare(lines[i], lines[j]) < 0
		})
		for _, line := range lines {
			fmt.Fprintf(hc.Stdout, "%s\n", line)
		}
		return nil
	},
	"grep": func(hc interp.HandlerContext, args []string) error {
		var rx *regexp.Regexp
		quiet := false
		for _, arg := range args {
			if arg == "-q" {
				quiet = true
			} else if arg == "-E" {
			} else if rx == nil {
				rx = regexp.MustCompile(arg)
			} else {
				return fmt.Errorf("unexpected arg: %q", arg)
			}
		}
		lines, err := readLines(hc)
		if err != nil {
			return err
		}
		anyMatch := false
		for _, line := range lines {
			if rx.Match(line) {
				if quiet {
					return nil
				}
				anyMatch = true
				fmt.Fprintf(hc.Stdout, "%s\n", line)
			}
		}
		if !anyMatch {
			return interp.NewExitStatus(1)
		}
		return nil
	},
	"sed": func(hc interp.HandlerContext, args []string) error {
		f := hc.Stdin
		switch len(args) {
		case 1:
		case 2:
			var err error
			f, err = os.Open(absPath(hc.Dir, args[1]))
			if err != nil {
				return err
			}
		default:
			return fmt.Errorf("usage: sed pattern [file]")
		}
		expr := args[0]
		if expr == "" || expr[0] != 's' {
			return fmt.Errorf("unimplemented")
		}
		sep := expr[1]
		expr = expr[2:]
		from := expr[:strings.IndexByte(expr, sep)]
		expr = expr[len(from)+1:]
		to := expr[:strings.IndexByte(expr, sep)]
		bs, err := io.ReadAll(f)
		if err != nil {
			return err
		}
		rx := regexp.MustCompile(from)
		bs = rx.ReplaceAllLiteral(bs, []byte(to))
		_, err = hc.Stdout.Write(bs)
		return err
	},
	"mkdir": func(hc interp.HandlerContext, args []string) error {
		for _, arg := range args {
			if arg == "-p" {
				continue
			}
			path := absPath(hc.Dir, arg)
			if err := os.MkdirAll(path, 0o777); err != nil {
				return err
			}
		}
		return nil
	},
	"rm": func(hc interp.HandlerContext, args []string) error {
		for _, arg := range args {
			if arg == "-r" {
				continue
			}
			path := absPath(hc.Dir, arg)
			if err := os.RemoveAll(path); err != nil {
				return err
			}
		}
		return nil
	},
	"ln": func(hc interp.HandlerContext, args []string) error {
		symbolic := args[0] == "-s"
		if symbolic {
			args = args[1:]
		}
		oldname := absPath(hc.Dir, args[0])
		newname := absPath(hc.Dir, args[1])
		if symbolic {
			return os.Symlink(oldname, newname)
		}
		return os.Link(oldname, newname)
	},
	"touch": func(hc interp.HandlerContext, args []string) error {
		filenames := args // create all arugments as filenames

		newTime := time.Now()
		if args[0] == "-t" {
			if len(args) < 3 {
				return fmt.Errorf("usage: touch [-t [[CC]YY]MMDDhhmm[.SS]] file")
			}
			filenames = args[2:] // treat the rest of the args as filenames

			arg := args[1]
			if len(arg) > 15 {
				return fmt.Errorf("usage: touch [-t [[CC]YY]MMDDhhmm[.SS]] file")
			}
			s, err := time.Parse("200601021504.05", arg)
			if err != nil {
				return err
			}
			newTime = s
		}

		for _, arg := range filenames {
			if strings.HasPrefix(arg, "-") {
				return fmt.Errorf("usage: touch [-t [[CC]YY]MMDDhhmm[.SS]] file")
			}
			path := absPath(hc.Dir, arg)
			// create the file if it does not exist
			f, err := os.OpenFile(path, os.O_CREATE, 0o666)
			if err != nil {
				return err
			}
			f.Close()
			// change the modification and access time
			if err := os.Chtimes(path, newTime, newTime); err != nil {
				return err
			}
		}
		return nil
	},
	"sleep": func(hc interp.HandlerContext, args []string) error {
		for _, arg := range args {
			// assume and default unit to be in seconds
			d, err := time.ParseDuration(fmt.Sprintf("%ss", arg))
			if err != nil {
				return err
			}
			time.Sleep(d)
		}
		return nil
	},
}

func testExecHandler(next interp.ExecHandlerFunc) interp.ExecHandlerFunc {
	return func(ctx context.Context, args []string) error {
		if fn := testBuiltinsMap[args[0]]; fn != nil {
			return fn(interp.HandlerCtx(ctx), args[1:])
		}
		return next(ctx, args)
	}
}

func testOpenHandler(ctx context.Context, path string, flag int, perm os.FileMode) (io.ReadWriteCloser, error) {
	if runtime.GOOS == "windows" && path == "/dev/null" {
		path = "NUL"
	}

	return interp.DefaultOpenHandler()(ctx, path, flag, perm)
}

func TestRunnerRunConfirm(t *testing.T) {
	if testing.Short() {
		t.Skip("calling bash is slow")
	}
	if !hasBash50 {
		t.Skip("bash 5.0 required to run")
	}
	t.Parallel()

	if runtime.GOOS == "windows" {
		// For example, it seems to treat environment variables as
		// case-sensitive, which isn't how Windows works.
		t.Skip("bash on Windows emulates Unix-y behavior")
	}
	for _, c := range runTests {
		c := c
		t.Run("", func(t *testing.T) {
			if strings.Contains(c.want, " #IGNORE") {
				return
			}
			skipIfUnsupported(t, c.in)
			t.Parallel()
			tdir := t.TempDir()
			ctx, cancel := context.WithTimeout(context.Background(), runnerRunTimeout)
			defer cancel()
			cmd := exec.CommandContext(ctx, "bash")
			cmd.Dir = tdir
			cmd.Stdin = strings.NewReader(c.in)
			out, err := cmd.CombinedOutput()
			if strings.Contains(c.want, " #JUSTERR") {
				// bash sometimes exits with status code 0 and
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
	t.Parallel()

	withPath := func(strs ...string) func(*interp.Runner) error {
		prefix := []string{
			"PATH=" + os.Getenv("PATH"),
			"ENV_PROG=" + os.Getenv("ENV_PROG"),
		}
		return interp.Env(expand.ListEnviron(append(prefix, strs...)...))
	}
	opts := func(list ...interp.RunnerOption) []interp.RunnerOption {
		return list
	}
	cases := []struct {
		opts     []interp.RunnerOption
		in, want string
	}{
		{
			nil,
			"$ENV_PROG | grep '^INTERP_GLOBAL='",
			"INTERP_GLOBAL=value\n",
		},
		{
			opts(withPath()),
			"$ENV_PROG | grep '^INTERP_GLOBAL='",
			"exit status 1",
		},
		{
			opts(withPath("INTERP_GLOBAL=bar_interp_missing")),
			"$ENV_PROG | grep '^INTERP_GLOBAL='",
			"INTERP_GLOBAL=bar_interp_missing\n",
		},
		{
			opts(withPath("a=b")),
			"echo $a",
			"b\n",
		},
		{
			opts(withPath("A=b")),
			"$ENV_PROG | grep '^A='; echo $A",
			"A=b\nb\n",
		},
		{
			opts(withPath("A=b", "A=c")),
			"$ENV_PROG | grep '^A='; echo $A",
			"A=c\nc\n",
		},
		{
			opts(withPath("HOME=")),
			"echo $HOME",
			"\n",
		},
		{
			opts(withPath("PWD=foo_interp_missing")),
			"[[ $PWD == foo_interp_missing ]]",
			"exit status 1",
		},
		{
			opts(interp.Params("foo_interp_missing")),
			"echo $@",
			"foo_interp_missing\n",
		},
		{
			opts(interp.Params("-u", "--", "foo_interp_missing")),
			"echo $@; echo $unset",
			"foo_interp_missing\nunset: unbound variable\nexit status 1",
		},
		{
			opts(interp.Params("-u", "--", "foo_interp_missing")),
			"echo $@; echo ${unset:-default}",
			"foo_interp_missing\ndefault\n",
		},
		{
			opts(interp.Params("foo_interp_missing")),
			"set >/dev/null; echo $@",
			"foo_interp_missing\n",
		},
		{
			opts(interp.Params("foo_interp_missing")),
			"set -e; echo $@",
			"foo_interp_missing\n",
		},
		{
			opts(interp.Params("foo_interp_missing")),
			"set --; echo $@",
			"\n",
		},
		{
			opts(interp.Params("foo_interp_missing")),
			"set bar_interp_missing; echo $@",
			"bar_interp_missing\n",
		},
	}
	p := syntax.NewParser()
	for _, c := range cases {
		t.Run("", func(t *testing.T) {
			skipIfUnsupported(t, c.in)
			file := parse(t, p, c.in)
			var cb concBuffer
			r, err := interp.New(append(c.opts,
				interp.StdIO(nil, &cb, &cb),
				interp.OpenHandler(testOpenHandler),
				interp.ExecHandlers(testExecHandler),
			)...)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), runnerRunTimeout)
			defer cancel()
			if err := r.Run(ctx, file); err != nil {
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
	t.Parallel()

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
	for _, in := range cases {
		t.Run("", func(t *testing.T) {
			file := parse(t, p, in)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			r, _ := interp.New()
			errChan := make(chan error)
			go func() {
				errChan <- r.Run(ctx, file)
			}()

			timeout := 500 * time.Millisecond
			select {
			case err := <-errChan:
				if err != nil && err != ctx.Err() {
					t.Fatal("Runner did not use ctx.Err()")
				}
			case <-time.After(timeout):
				t.Fatalf("program was not killed in %s", timeout)
			}
		})
	}
}

func TestRunnerAltNodes(t *testing.T) {
	t.Parallel()

	in := "echo foo_interp_missing"
	file := parse(t, nil, in)
	want := "foo_interp_missing\n"
	nodes := []syntax.Node{
		file,
		file.Stmts[0],
		file.Stmts[0].Cmd,
	}
	for _, node := range nodes {
		var cb concBuffer
		r, _ := interp.New(interp.StdIO(nil, &cb, &cb))
		ctx, cancel := context.WithTimeout(context.Background(), runnerRunTimeout)
		defer cancel()
		if err := r.Run(ctx, node); err != nil {
			cb.WriteString(err.Error())
		}
		if got := cb.String(); got != want {
			t.Fatalf("wrong output in %q:\nwant: %q\ngot:  %q",
				in, want, got)
		}
	}
}

func TestRunnerDir(t *testing.T) {
	t.Parallel()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Run("Missing", func(t *testing.T) {
		_, err := interp.New(interp.Dir("missing"))
		if err == nil {
			t.Fatal("expected New to error when Dir is missing")
		}
	})
	t.Run("NotDir", func(t *testing.T) {
		_, err := interp.New(interp.Dir("interp_test.go"))
		if err == nil {
			t.Fatal("expected New to error when Dir is not a dir")
		}
	})
	t.Run("NotDirAbs", func(t *testing.T) {
		_, err := interp.New(interp.Dir(filepath.Join(wd, "interp_test.go")))
		if err == nil {
			t.Fatal("expected New to error when Dir is not a dir")
		}
	})
	t.Run("Relative", func(t *testing.T) {
		// On Windows, it's impossible to make a relative path from one
		// drive to another. Use the parent directory, as that's for
		// sure in the same drive as the current directory.
		rel := ".." + string(filepath.Separator)
		r, err := interp.New(interp.Dir(rel))
		if err != nil {
			t.Fatal(err)
		}
		if !filepath.IsAbs(r.Dir) {
			t.Errorf("Runner.Dir is not absolute")
		}
	})
	// Ensure that we treat symlinks and short paths properly, especially
	// with Dir and globbing.
	t.Run("SymlinkOrShortPath", func(t *testing.T) {
		tdir := t.TempDir()

		realDir := filepath.Join(tdir, "real-long-dir-name")
		realFile := filepath.Join(realDir, "realfile")

		if err := os.Mkdir(realDir, 0o777); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(realFile, []byte(""), 0o666); err != nil {
			t.Fatal(err)
		}

		var altDir string
		if runtime.GOOS == "windows" {
			short, err := shortPathName(realDir)
			if err != nil {
				t.Fatal(err)
			}
			altDir = short
			// We replace tdir later, and it might have been shortened.
			tdir = filepath.Dir(altDir)
		} else {
			altDir = filepath.Join(tdir, "symlink")
			if err := os.Symlink(realDir, altDir); err != nil {
				t.Fatal(err)
			}
		}

		var b bytes.Buffer
		r, err := interp.New(interp.Dir(altDir), interp.StdIO(nil, &b, &b))
		if err != nil {
			t.Fatal(err)
		}
		file := parse(t, nil, "echo $PWD $PWD/*")
		ctx, cancel := context.WithTimeout(context.Background(), runnerRunTimeout)
		defer cancel()
		if err := r.Run(ctx, file); err != nil {
			t.Fatal(err)
		}
		got := b.String()
		got = strings.ReplaceAll(got, tdir, "")
		got = strings.TrimSpace(got)
		want := `/symlink /symlink/realfile`
		if runtime.GOOS == "windows" {
			want = `\\REAL.{4} \\REAL.{4}\\realfile`
		}
		if !regexp.MustCompile(want).MatchString(got) {
			t.Fatalf("\nwant regexp: %q\ngot: %q", want, got)
		}
	})
}

func TestRunnerIncremental(t *testing.T) {
	t.Parallel()

	file := parse(t, nil, "echo foo_interp_missing; false; echo bar_interp_missing; exit 0; echo baz")
	want := "foo_interp_missing\nbar_interp_missing\n"
	var b bytes.Buffer
	r, _ := interp.New(interp.StdIO(nil, &b, &b))
	ctx, cancel := context.WithTimeout(context.Background(), runnerRunTimeout)
	defer cancel()
	for _, stmt := range file.Stmts {
		err := r.Run(ctx, stmt)
		if _, ok := interp.IsExitStatus(err); !ok && err != nil {
			// Keep track of unexpected errors.
			b.WriteString(err.Error())
		}
		if r.Exited() {
			break
		}
	}
	if got := b.String(); got != want {
		t.Fatalf("\nwant: %q\ngot:  %q", want, got)
	}
}

func TestRunnerResetFields(t *testing.T) {
	t.Parallel()

	tdir := t.TempDir()
	logPath := filepath.Join(tdir, "log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	r, _ := interp.New(
		interp.Params("-f", "--", "first", tdir, logPath),
		interp.Dir(tdir),
		interp.OpenHandler(testOpenHandler),
		interp.ExecHandlers(testExecHandler),
	)
	// Check that using option funcs and Runner fields directly is still
	// kept by Reset.
	interp.StdIO(nil, logFile, os.Stderr)(r)
	r.Env = expand.ListEnviron(append(os.Environ(), "GLOBAL=foo_interp_missing")...)

	file := parse(t, nil, `
# Params set 3 arguments
[[ $# -eq 3 ]] || exit 10
[[ $1 == "first" ]] || exit 11

# Params set the -f option (noglob)
[[ -o noglob ]] || exit 12

# $PWD was set via Dir, and should be equal to $2
[[ "$PWD" == "$2" ]] || exit 13

# stdout should go into the log file, which is at $3
echo line1
echo line2
[[ "$(wc -l <$3)" == "2" ]] || exit 14

# $GLOBAL was set directly via the Env field
[[ "$GLOBAL" == "foo_interp_missing" ]] || exit 15

# Change all of the above within the script. Reset should undo this.
set +f -- newargs
cd
exec >/dev/null 2>/dev/null
GLOBAL=
export GLOBAL=
`)
	ctx, cancel := context.WithTimeout(context.Background(), runnerRunTimeout)
	defer cancel()
	for i := 0; i < 3; i++ {
		if err := r.Run(ctx, file); err != nil {
			t.Fatalf("run number %d: %v", i, err)
		}
		r.Reset()
		// empty the log file too
		logFile.Truncate(0)
		logFile.Seek(0, io.SeekStart)
	}
}

func TestRunnerManyResets(t *testing.T) {
	t.Parallel()
	r, _ := interp.New()
	for i := 0; i < 5; i++ {
		r.Reset()
	}
}

func TestRunnerFilename(t *testing.T) {
	t.Parallel()

	want := "f.sh\n"
	file, _ := syntax.NewParser().Parse(strings.NewReader("echo $0"), "f.sh")
	var b bytes.Buffer
	r, _ := interp.New(interp.StdIO(nil, &b, &b))
	ctx, cancel := context.WithTimeout(context.Background(), runnerRunTimeout)
	defer cancel()
	if err := r.Run(ctx, file); err != nil {
		t.Fatal(err)
	}
	if got := b.String(); got != want {
		t.Fatalf("\nwant: %q\ngot:  %q", want, got)
	}
}

func TestRunnerEnvNoModify(t *testing.T) {
	t.Parallel()

	env := expand.ListEnviron("one=1", "two=2")
	file := parse(t, nil, `echo -n "$one $two; "; one=x; unset two`)

	var b bytes.Buffer
	r, _ := interp.New(interp.Env(env), interp.StdIO(nil, &b, &b))
	ctx, cancel := context.WithTimeout(context.Background(), runnerRunTimeout)
	defer cancel()
	for i := 0; i < 3; i++ {
		r.Reset()
		err := r.Run(ctx, file)
		if err != nil {
			t.Fatal(err)
		}
	}

	want := "1 2; 1 2; 1 2; "
	if got := b.String(); got != want {
		t.Fatalf("\nwant: %q\ngot:  %q", want, got)
	}
}

func TestMalformedPathOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Skipping windows test on non-windows GOOS")
	}
	tdir := t.TempDir()
	t.Parallel()

	path := filepath.Join(tdir, "test.cmd")
	script := []byte("@echo foo_interp_missing")
	if err := os.WriteFile(path, script, 0o777); err != nil {
		t.Fatal(err)
	}

	// set PATH to c:\tmp\dir instead of C:\tmp\dir
	volume := filepath.VolumeName(tdir)
	pathList := strings.ToLower(volume) + tdir[len(volume):]

	file := parse(t, nil, "test.cmd")
	var cb concBuffer
	r, _ := interp.New(interp.Env(expand.ListEnviron("PATH="+pathList)), interp.StdIO(nil, &cb, &cb))
	ctx, cancel := context.WithTimeout(context.Background(), runnerRunTimeout)
	defer cancel()
	if err := r.Run(ctx, file); err != nil {
		t.Fatal(err)
	}
	want := "foo_interp_missing\r\n"
	if got := cb.String(); got != want {
		t.Fatalf("wrong output:\nwant: %q\ngot:  %q", want, got)
	}
}

func TestReadShouldNotPanicWithNilStdin(t *testing.T) {
	t.Parallel()

	r, err := interp.New()
	if err != nil {
		t.Fatal(err)
	}

	f := parse(t, nil, "read foo_interp_missingbar_interp_missing")
	ctx, cancel := context.WithTimeout(context.Background(), runnerRunTimeout)
	defer cancel()
	if err := r.Run(ctx, f); err == nil {
		t.Fatal("it should have retuned an error")
	}
}

func TestRunnerVars(t *testing.T) {
	t.Parallel()

	r, err := interp.New()
	if err != nil {
		t.Fatal(err)
	}

	f := parse(t, nil, "FOO_INTERP_MISSING=updated; BAR_INTERP_MISSING=new")
	ctx, cancel := context.WithTimeout(context.Background(), runnerRunTimeout)
	defer cancel()
	if err := r.Run(ctx, f); err != nil {
		t.Fatal(err)
	}

	if want, got := "updated", r.Vars["FOO_INTERP_MISSING"].String(); got != want {
		t.Fatalf("wrong output:\nwant: %q\ngot:  %q", want, got)
	}
}

func TestRunnerSubshell(t *testing.T) {
	t.Parallel()

	r1, err := interp.New()
	if err != nil {
		t.Fatal(err)
	}

	r2 := r1.Subshell()
	f1 := parse(t, nil, "PARENT=foo_interp_missing")
	f2 := parse(t, nil, "CHILD=bar_interp_missing")

	ctx, cancel := context.WithTimeout(context.Background(), runnerRunTimeout)
	defer cancel()
	if err := r1.Run(ctx, f1); err != nil {
		t.Fatal(err)
	}
	if err := r2.Run(ctx, f2); err != nil {
		t.Fatal(err)
	}

	if want, got := "foo_interp_missing", r1.Vars["PARENT"].String(); got != want {
		t.Fatalf("wrong output:\nwant: %q\ngot:  %q", want, got)
	}
	if want, got := "bar_interp_missing", r2.Vars["CHILD"].String(); got != want {
		t.Fatalf("wrong output:\nwant: %q\ngot:  %q", want, got)
	}

	r3 := r2.Subshell()
	f3 := parse(t, nil, "CHILD=modified")
	if err := r3.Run(ctx, f3); err != nil {
		t.Fatal(err)
	}
	if want, got := "bar_interp_missing", r2.Vars["CHILD"].String(); got != want {
		t.Fatalf("wrong output:\nwant: %q\ngot:  %q", want, got)
	}
	if want, got := "modified", r3.Vars["CHILD"].String(); got != want {
		t.Fatalf("wrong output:\nwant: %q\ngot:  %q", want, got)
	}
}
