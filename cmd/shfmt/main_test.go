// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"

	"mvdan.cc/sh/v3/syntax"
)

func init() {
	parser = syntax.NewParser(syntax.KeepComments(true))
	printer = syntax.NewPrinter()
}

func TestMain(m *testing.M) {
	os.Exit(testscript.RunMain(m, map[string]func() int{
		"shfmt": main1,
	}))
}

var update = flag.Bool("u", false, "update testscript output files")

func TestScripts(t *testing.T) {
	t.Parallel()
	testscript.Run(t, testscript.Params{
		Dir: filepath.Join("testdata", "scripts"),
		Setup: func(env *testscript.Env) error {
			bindir := filepath.Join(env.WorkDir, ".bin")
			if err := os.Mkdir(bindir, 0777); err != nil {
				return err
			}
			binfile := filepath.Join(bindir, "shfmt")
			if runtime.GOOS == "windows" {
				binfile += ".exe"
			}
			if err := os.Symlink(os.Args[0], binfile); err != nil {
				return err
			}
			env.Vars = append(env.Vars, fmt.Sprintf("PATH=%s%c%s", bindir, filepath.ListSeparator, os.Getenv("PATH")))
			env.Vars = append(env.Vars, "TESTSCRIPT_COMMAND=shfmt")
			return nil
		},
		UpdateScripts: *update,
	})
}

type action uint

const (
	None action = iota
	Skip
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
	{Skip, false, filepath.Join(".git", "ext.sh"), " foo"},
	{Skip, false, filepath.Join(".svn", "ext.sh"), " foo"},
	{Skip, false, filepath.Join(".hg", "ext.sh"), " foo"},
	{Error, false, "parse-error.sh", " foo("},
	{None, true, "reallylongdir/symlink-file", "ext-shebang.sh"},
	{None, true, "symlink-dir", "reallylongdir"},
	{None, true, "symlink-none", "reallylongdir/nonexistent"},
}

// errPathMentioned extracts filenames from error lines. We can't rely on
// Windows paths not containing colon characters, so we must find the end of the
// path based on the ":line:col: " suffix.
var errPathMentioned = regexp.MustCompile(`^(.+):\d+:\d+: `)

func TestWalk(t *testing.T) {
	t.Parallel()
	tdir, err := ioutil.TempDir("", "shfmt-walk")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tdir)
	for _, wt := range walkTests {
		path := filepath.Join(tdir, wt.path)
		dir, _ := filepath.Split(path)
		os.MkdirAll(dir, 0777)
		if wt.symlink {
			if err := os.Symlink(wt.body, path); err != nil {
				t.Fatal(err)
			}
			continue
		}
		err := ioutil.WriteFile(path, []byte(wt.body), 0666)
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
	doWalk(tdir)
	modified := map[string]bool{}
	outScan := bufio.NewScanner(&outBuf)
	for outScan.Scan() {
		path := outScan.Text()
		modified[path] = true
	}
	for _, wt := range walkTests {
		t.Run(wt.path, func(t *testing.T) {
			mod := modified[filepath.Join(tdir, wt.path)]
			if mod && wt.want != Modify {
				t.Fatalf("walk had to not run on %s but did", wt.path)
			} else if !mod && wt.want == Modify {
				t.Fatalf("walk had to run on %s but didn't", wt.path)
			}
			err := errored[filepath.Join(tdir, wt.path)]
			if err && wt.want != Error {
				t.Fatalf("walk had to not error on %s but did", wt.path)
			} else if !err && wt.want == Error {
				t.Fatalf("walk had to error on %s but didn't", wt.path)
			}
		})
	}
	if doWalk(tdir); outBuf.Len() > 0 {
		t.Fatal("shfmt -l -w printed paths on a duplicate run")
	}
	*list, *write = false, false
	if doWalk(tdir); outBuf.Len() == 0 {
		t.Fatal("shfmt without -l nor -w did not print anything")
	}
	if doWalk(filepath.Join(tdir, ".hidden")); outBuf.Len() == 0 {
		t.Fatal("`shfmt .hidden` did not print anything")
	}
	if doWalk(filepath.Join(tdir, "nonexistent")); !gotError {
		t.Fatal("`shfmt nonexistent` did not error")
	}
	*find = true
	doWalk(tdir)
	numFound := strings.Count(outBuf.String(), "\n")
	if want := 13; numFound != want {
		t.Fatalf("shfmt -f printed %d paths, but wanted %d", numFound, want)
	}
	for _, wt := range walkTests {
		t.Run(wt.path, func(t *testing.T) {
			path := filepath.Join(tdir, wt.path)
			doWalk(path)
			isShell := outBuf.Len() > 0
			if isShell && wt.want == None {
				t.Fatalf("shfmt -f wrongly detected %s as shell script", wt.path)
			}
		})
	}
	*find = false
}
