// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package sh

import (
	"strings"
	"testing"
)

func TestParseErr(t *testing.T) {
	errs := []struct {
		in, want string
	}{
		{
			"'",
			`1:2: unexpected token EOF - wanted '`,
		},
		{
			`"`,
			`1:2: unexpected token EOF - wanted "`,
		},
		{
			"`",
			"1:2: unexpected token EOF - wanted `",
		},
		{
			`'\''`,
			`1:5: unexpected token EOF - wanted '`,
		},
		{
			";",
			`1:1: unexpected token ; - wanted command`,
		},
		{
			"à(){}",
			`1:1: invalid func name "à"`,
		},
		{
			"{",
			`1:2: unexpected token EOF - wanted command`,
		},
		{
			"{}",
			`1:2: unexpected token } - wanted command`,
		},
		{
			"}",
			`1:1: unexpected token } - wanted command`,
		},
		{
			"{#}",
			`1:4: unexpected token EOF - wanted }`,
		},
		{
			"(",
			`1:2: unexpected token EOF - wanted command`,
		},
		{
			")",
			`1:1: unexpected token ) - wanted command`,
		},
		{
			"()",
			`1:2: unexpected token ) - wanted command`,
		},
		{
			"( foo;",
			`1:7: unexpected token EOF - wanted )`,
		},
		{
			"&",
			`1:1: unexpected token & - wanted command`,
		},
		{
			"|",
			`1:1: unexpected token | - wanted command`,
		},
		{
			"foo;;",
			`1:4: unexpected token ;; after command`,
		},
		{
			"foo(",
			`1:5: unexpected token EOF - wanted )`,
		},
		{
			"à(",
			`1:3: unexpected token EOF - wanted )`,
		},
		{
			"foo'",
			`1:5: unexpected token EOF - wanted '`,
		},
		{
			`foo"`,
			`1:5: unexpected token EOF - wanted "`,
		},
		{
			"foo()",
			`1:6: unexpected token EOF - wanted command`,
		},
		{
			"foo() {",
			`1:8: unexpected token EOF - wanted command`,
		},
		{
			"echo foo(",
			`1:9: unexpected token ( after command`,
		},
		{
			"foo &&",
			`1:7: unexpected token EOF - wanted command`,
		},
		{
			"foo |",
			`1:6: unexpected token EOF - wanted command`,
		},
		{
			"foo ||",
			`1:7: unexpected token EOF - wanted command`,
		},
		{
			"foo >",
			`1:6: unexpected token EOF - wanted word`,
		},
		{
			"foo >>",
			`1:7: unexpected token EOF - wanted word`,
		},
		{
			"foo >&",
			`1:7: unexpected token EOF - wanted word`,
		},
		{
			"foo <",
			`1:6: unexpected token EOF - wanted word`,
		},
		{
			"if",
			`1:3: unexpected token EOF - wanted command`,
		},
		{
			"if foo;",
			`1:8: unexpected token EOF - wanted then`,
		},
		{
			"if foo; bar",
			`1:9: unexpected token word - wanted then`,
		},
		{
			"if foo; then bar;",
			`1:18: unexpected token EOF - wanted fi`,
		},
		{
			"'foo' '",
			`1:8: unexpected token EOF - wanted '`,
		},
		{
			"'foo\n' '",
			`2:4: unexpected token EOF - wanted '`,
		},
		{
			"while",
			`1:6: unexpected token EOF - wanted command`,
		},
		{
			"while foo;",
			`1:11: unexpected token EOF - wanted do`,
		},
		{
			"while foo; bar",
			`1:12: unexpected token word - wanted do`,
		},
		{
			"while foo; do bar",
			`1:18: unexpected token EOF - wanted done`,
		},
		{
			"while foo; do bar;",
			`1:19: unexpected token EOF - wanted done`,
		},
		{
			"for",
			`1:4: unexpected token EOF - wanted word`,
		},
		{
			"for i",
			`1:6: unexpected token EOF - wanted in`,
		},
		{
			"for i in;",
			`1:10: unexpected token EOF - wanted do`,
		},
		{
			"for i in 1 2 3;",
			`1:16: unexpected token EOF - wanted do`,
		},
		{
			"for i in 1 2 3; do echo $i;",
			`1:28: unexpected token EOF - wanted done`,
		},
		{
			"for i in 1 2 3; echo $i;",
			`1:17: unexpected token word - wanted do`,
		},
		{
			"for in 1 2 3; do echo $i; done",
			`1:8: unexpected token word - wanted in`,
		},
		{
			"foo &\n;",
			`2:1: unexpected token ; - wanted command`,
		},
		{
			"echo $(foo",
			`1:11: unexpected token EOF - wanted )`,
		},
		{
			"echo ${foo",
			`1:11: unexpected token EOF - wanted }`,
		},
		{
			"#foo\n{",
			`2:2: unexpected token EOF - wanted command`,
		},
		{
			`echo "foo${bar"`,
			`1:16: unexpected token EOF - wanted }`,
		},
		{
			"echo `foo${bar`",
			`1:16: unexpected token EOF - wanted }`,
		},
		{
			"foo\n;",
			`2:1: unexpected token ; - wanted command`,
		},
	}
	for _, c := range errs {
		r := strings.NewReader(c.in)
		_, err := Parse(r, "")
		if err == nil {
			t.Fatalf("Expected error in %q", c.in)
		}
		got := err.Error()[1:]
		if got != c.want {
			t.Fatalf("Error mismatch in %q\nwant: %s\ngot:  %s",
				c.in, c.want, got)
		}
	}
}
