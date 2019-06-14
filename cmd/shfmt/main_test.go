// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package main

import (
	"bufio"
	"bytes"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"mvdan.cc/sh/v3/syntax"
)

func init() {
	parser = syntax.NewParser(syntax.KeepComments)
	printer = syntax.NewPrinter()
}

func TestStdin(t *testing.T) {
	var buf bytes.Buffer
	out = &buf
	t.Run("Regular", func(t *testing.T) {
		in = strings.NewReader(" foo")
		buf.Reset()
		if err := formatStdin(); err != nil {
			t.Fatal(err)
		}
		if got, want := buf.String(), "foo\n"; got != want {
			t.Fatalf("got=%q want=%q", got, want)
		}
	})

	t.Run("List", func(t *testing.T) {
		*list = true
		defer func() { *list = false }()
		in = strings.NewReader(" foo")
		buf.Reset()
		if err := formatStdin(); err != nil {
			t.Fatal(err)
		}
		if got, want := buf.String(), "<standard input>\n"; got != want {
			t.Fatalf("got=%q want=%q", got, want)
		}
	})

	t.Run("Diff", func(t *testing.T) {
		if _, err := exec.LookPath("diff"); err != nil {
			t.Skip("skipping as the diff tool is not available")
		}
		*diff = true
		defer func() { *diff = false }()
		in = strings.NewReader(" foo\nbar\n\n")
		buf.Reset()
		if err := formatStdin(); err != errChangedWithDiff {
			t.Fatalf("got=%q want=%q", err, errChangedWithDiff)
		}
		want := `diff -u <standard input>.orig <standard input>
@@ -1,3 +1,2 @@
- foo
+foo
 bar
-
`
		if got := buf.String(); got != want {
			t.Fatalf("got:\n%swant:\n%s", got, want)
		}
	})

	t.Run("DiffColored", func(t *testing.T) {
		if _, err := exec.LookPath("diff"); err != nil {
			t.Skip("skipping as the diff tool is not available")
		}
		*diff = true
		color = true
		defer func() { *diff = false; color = false }()
		in = strings.NewReader(" foo\nbar\n\n")
		buf.Reset()
		if err := formatStdin(); err != errChangedWithDiff {
			t.Fatalf("got=%q want=%q", err, errChangedWithDiff)
		}
		want := "diff -u <standard input>.orig <standard input>\n@@ -1,3 +1,2 @@\n\x1b[31m- foo\x1b[0m\n\x1b[32m+foo\x1b[0m\n bar\n\x1b[31m-\x1b[0m\n"
		if got := buf.String(); got != want {
			t.Fatalf("got:\n%swant:\n%s", got, want)
		}
	})
}

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
	*find = false
}
