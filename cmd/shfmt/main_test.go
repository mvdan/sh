// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package main

import (
	"bufio"
	"bytes"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type action uint

const (
	None action = iota
	Modify
	Error
)

var walkTests = []struct {
	want       action
	path, body string
}{
	{Modify, "shebang-1", "#!/bin/sh\n foo"},
	{Modify, "shebang-2", "#!/bin/bash\n foo"},
	{Modify, "shebang-3", "#!/usr/bin/sh\n foo"},
	{Modify, "shebang-4", "#!/usr/bin/env bash\n foo"},
	{Modify, "shebang-5", "#!/bin/env sh\n foo"},
	{Modify, "shebang-space", "#! /bin/sh\n foo"},
	{Modify, "shebang-tabs", "#!\t/bin/env\tsh\n foo"},
	{Modify, "ext.sh", " foo"},
	{Modify, "ext.bash", " foo"},
	{Modify, "ext-shebang.sh", "#!/bin/sh\n foo"},
	{None, ".hidden", " foo long enough"},
	{None, ".hidden-shebang", "#!/bin/sh\n foo"},
	{None, "..hidden-shebang", "#!/bin/sh\n foo"},
	{None, "noext-empty", " foo"},
	{None, "noext-noshebang", " foo long enough"},
	{None, "ext.other", " foo"},
	{None, "ext-shebang.other", "#!/bin/sh\n foo"},
	{None, "shebang-nospace", "#!/bin/envsh\n foo"},
	{None, filepath.Join(".git", "ext.sh"), " foo"},
	{None, filepath.Join(".svn", "ext.sh"), " foo"},
	{None, filepath.Join(".hg", "ext.sh"), " foo"},
	{Error, "parse-error.sh", " foo("},
}

func TestWalk(t *testing.T) {
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
		if err := ioutil.WriteFile(wt.path, []byte(wt.body), 0666); err != nil {
			t.Fatal(err)
		}
	}
	var outBuf bytes.Buffer
	out = &outBuf
	*list, *write = true, true
	gotError := false
	errored := map[string]bool{}
	onError := func(err error) {
		gotError = true
		line := err.Error()
		if i := strings.IndexByte(line, ':'); i >= 0 {
			errored[line[:i]] = true
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
	if err := ioutil.WriteFile("nowrite", []byte(" foo"), 0444); err != nil {
		t.Fatal(err)
	}
	*write = true
	if doWalk("nowrite"); !gotError {
		t.Fatal("`shfmt nowrite` did not error")
	}
}
