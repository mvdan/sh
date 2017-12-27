// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package main

import (
	"bufio"
	"bytes"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"mvdan.cc/sh/syntax"
)

type action uint

const (
	None action = iota
	Modify
	Error
)

var walkTests = []struct {
	want       action
	symlink    bool
	path, body string
}{
	{Modify, false, "shebang-1", "#!/bin/sh\n foo"},
	{Modify, false, "shebang-2", "#!/bin/bash\n foo"},
	{Modify, false, "shebang-3", "#!/usr/bin/sh\n foo"},
	{Modify, false, "shebang-4", "#!/usr/bin/env bash\n foo"},
	{Modify, false, "shebang-5", "#!/bin/env sh\n foo"},
	{Modify, false, "shebang-space", "#! /bin/sh\n foo"},
	{Modify, false, "shebang-tabs", "#!\t/bin/env\tsh\n foo"},
	{Modify, false, "shebang-args", "#!/bin/bash -e -x\nfoo"},
	{Modify, false, "ext.sh", " foo"},
	{Modify, false, "ext.bash", " foo"},
	{Modify, false, "ext-shebang.sh", "#!/bin/sh\n foo"},
	{Modify, false, filepath.Join("dir", "ext.sh"), " foo"},
	{None, false, ".hidden", " foo long enough"},
	{None, false, ".hidden-shebang", "#!/bin/sh\n foo"},
	{None, false, "..hidden-shebang", "#!/bin/sh\n foo"},
	{None, false, "noext-empty", " foo"},
	{None, false, "noext-noshebang", " foo long enough"},
	{None, false, "shebang-nonewline", "#!/bin/shfoo"},
	{None, false, "ext.other", " foo"},
	{None, false, "ext-shebang.other", "#!/bin/sh\n foo"},
	{None, false, "shebang-nospace", "#!/bin/envsh\n foo"},
	{None, false, filepath.Join(".git", "ext.sh"), " foo"},
	{None, false, filepath.Join(".svn", "ext.sh"), " foo"},
	{None, false, filepath.Join(".hg", "ext.sh"), " foo"},
	{Error, false, "parse-error.sh", " foo("},
	{None, true, "reallylongdir/symlink-file", "ext-shebang.sh"},
	{None, true, "symlink-dir", "reallylongdir"},
	{None, true, "symlink-none", "reallylongdir/nonexistent"},
}

var errPathMentioned = regexp.MustCompile(`([^ :]+):`)

func TestWalk(t *testing.T) {
	parser = syntax.NewParser(syntax.KeepComments)
	printer = syntax.NewPrinter()
	dir, err := ioutil.TempDir("", "shfmt-walk")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	for _, wt := range walkTests {
		if dir, _ := filepath.Split(wt.path); dir != "" {
			dir = dir[:len(dir)-1]
			os.Mkdir(dir, 0777)
		}
		if wt.symlink {
			if err := os.Symlink(wt.body, wt.path); err != nil {
				t.Fatal(err)
			}
			continue
		}
		err := ioutil.WriteFile(wt.path, []byte(wt.body), 0666)
		if err != nil {
			t.Fatal(err)
		}
	}
	var outBuf bytes.Buffer
	out = &outBuf
	*list, *write = true, true
	*simple = true
	gotError := false
	errored := map[string]bool{}
	onError := func(err error) {
		gotError = true
		line := err.Error()
		if sub := errPathMentioned.FindStringSubmatch(line); sub != nil {
			errored[sub[1]] = true
		}
	}
	doWalk := func(path string) {
		gotError = false
		outBuf.Reset()
		walk(path, onError)
	}
	doWalk(".")
	modified := map[string]bool{}
	outScan := bufio.NewScanner(&outBuf)
	for outScan.Scan() {
		path := outScan.Text()
		modified[path] = true
	}
	for _, wt := range walkTests {
		t.Run(wt.path, func(t *testing.T) {
			mod := modified[wt.path]
			if mod && wt.want != Modify {
				t.Fatalf("walk had to not run on %s but did", wt.path)
			} else if !mod && wt.want == Modify {
				t.Fatalf("walk had to run on %s but didn't", wt.path)
			}
			err := errored[wt.path]
			if err && wt.want != Error {
				t.Fatalf("walk had to not err on %s but did", wt.path)
			} else if !err && wt.want == Error {
				t.Fatalf("walk had to err on %s but didn't", wt.path)
			}
		})
	}
	if doWalk("."); outBuf.Len() > 0 {
		t.Fatal("shfmt -l -w printed paths on a duplicate run")
	}
	*list, *write = false, false
	if doWalk("."); outBuf.Len() == 0 {
		t.Fatal("shfmt without -l nor -w did not print anything")
	}
	if doWalk(".hidden"); outBuf.Len() == 0 {
		t.Fatal("`shfmt .hidden` did not print anything")
	}
	if doWalk("nonexistent"); !gotError {
		t.Fatal("`shfmt nonexistent` did not error")
	}
	*find = true
	doWalk(".")
	numFound := strings.Count(outBuf.String(), "\n")
	if want := 13; numFound != want {
		t.Fatalf("shfmt -f printed %d paths, but wanted %d", numFound, want)
	}
	*find = false
	*write = true
}
