// Copyright (c) 2019, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package pattern_test

import (
	"fmt"
	"regexp"

	"mvdan.cc/sh/v3/pattern"
)

func ExampleRegexp() {
	pat := "foo?bar*"
	fmt.Println(pat)

	expr, err := pattern.Regexp(pat, 0)
	if err != nil {
		return
	}
	fmt.Println(expr)

	rx := regexp.MustCompile(expr)
	fmt.Println(rx.MatchString("foo bar baz"))
	fmt.Println(rx.MatchString("foobarbaz"))
	// Output:
	// foo?bar*
	// foo.bar.*
	// true
	// false
}

func ExampleQuoteMeta() {
	pat := "foo?bar*"
	fmt.Println(pat)

	quoted := pattern.QuoteMeta(pat)
	fmt.Println(quoted)

	expr, err := pattern.Regexp(quoted, 0)
	if err != nil {
		return
	}

	rx := regexp.MustCompile(expr)
	fmt.Println(rx.MatchString("foo bar baz"))
	fmt.Println(rx.MatchString("foo?bar*"))
	// Output:
	// foo?bar*
	// foo\?bar\*
	// false
	// true
}
