// Copyright (c) 2018, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package shell

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
)

func strEnviron(pairs ...string) func(string) string {
	return func(name string) string {
		prefix := name + "="
		for _, pair := range pairs {
			if val := strings.TrimPrefix(pair, prefix); val != pair {
				return val
			}
		}
		return ""
	}
}

var expandTests = []struct {
	in   string
	env  func(name string) string
	want string
}{
	{"foo", nil, "foo"},
	{"\nfoo\n", nil, "\nfoo\n"},
	{"a-$b-c", nil, "a--c"},
	{"${INTERP_GLOBAL:+hasOsEnv}", nil, "hasOsEnv"},
	{"a-$b-c", strEnviron(), "a--c"},
	{"a-$b-c", strEnviron("b=b_val"), "a-b_val-c"},
	{"${x//o/a}", strEnviron("x=foo"), "faa"},
	{"~", strEnviron("HOME=/my/home"), "/my/home"},
	{"~/foo/bar", strEnviron("HOME=/my/home"), "/my/home/foo/bar"},
}

func TestExpand(t *testing.T) {
	os.Setenv("INTERP_GLOBAL", "value")
	for i := range expandTests {
		t.Run(fmt.Sprintf("%02d", i), func(t *testing.T) {
			tc := expandTests[i]
			t.Parallel()
			got, err := Expand(tc.in, tc.env)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("\nwant: %q\ngot:  %q", tc.want, got)
			}
		})
	}
}

func TestUnexpectedCmdSubst(t *testing.T) {
	t.Parallel()
	want := "unexpected command substitution at 1:6"
	for _, fn := range []func() error{
		func() error {
			_, err := Expand("echo $(uname -a)", nil)
			return err
		},
		func() error {
			_, err := Fields("echo $(uname -a)", nil)
			return err
		},
	} {
		got := fmt.Sprint(fn())
		if !strings.Contains(got, want) {
			t.Fatalf("wanted error %q, got: %s", want, got)
		}
	}
}

var fieldsTests = []struct {
	in   string
	env  func(name string) string
	want []string
}{
	{"foo", nil, []string{"foo"}},
	{"\nfoo\n", nil, []string{"foo"}},
	{"foo bar", nil, []string{"foo", "bar"}},
	{"foo 'bar baz'", nil, []string{"foo", "bar baz"}},
	{"$x", strEnviron("x=foo bar"), []string{"foo", "bar"}},
	{`"$x"`, strEnviron("x=foo bar"), []string{"foo bar"}},
}

func TestFields(t *testing.T) {
	os.Setenv("INTERP_GLOBAL", "value")
	for i := range fieldsTests {
		t.Run(fmt.Sprintf("%02d", i), func(t *testing.T) {
			tc := fieldsTests[i]
			t.Parallel()
			got, err := Fields(tc.in, tc.env)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("\nwant: %q\ngot:  %q", tc.want, got)
			}
		})
	}
}
