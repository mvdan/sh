// Copyright (c) 2018, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

// Package expand contains code to perform various shell expansions
// on programs parsed by the [syntax] package as either [syntax.LangBash]
// or [syntax.LangPOSIX], behaving like Bash as a result.
//
// For one-call helpers such as expanding or splitting a string,
// see [mvdan.cc/sh/v3/shell].
//
// Unlike Bash, expansions are limited in size, so that a script cannot
// quickly exhaust memory by growing a string or an array exponentially.
// An expansion fails if it would produce over 16MiB or more than a million
// fields, and a pattern over 64KiB fails, except in globbing,
// where it matches no files.
package expand
