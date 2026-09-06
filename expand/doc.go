// Copyright (c) 2018, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

// Package expand contains code to perform various shell expansions
// on programs parsed by the [syntax] package as either [syntax.LangBash]
// or [syntax.LangPOSIX], behaving like Bash as a result.
//
// For one-call helpers such as expanding or splitting a string,
// see [mvdan.cc/sh/v3/shell].
package expand
