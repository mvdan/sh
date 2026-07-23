// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package main

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
)

func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){
		"shfmt": main,
	})
}

var update = flag.Bool("u", false, "update testscript output files")

func TestScript(t *testing.T) {
	t.Parallel()
	testscript.Run(t, testscript.Params{
		Dir:                 filepath.Join("testdata", "script"),
		UpdateScripts:       *update,
		RequireExplicitExec: true,
		Cmds: map[string]func(ts *testscript.TestScript, neg bool, args []string){
			// txtar archive files always end with a newline,
			// so this helps test files which do not end with one.
			"trim-newline": func(ts *testscript.TestScript, neg bool, args []string) {
				if neg || len(args) == 0 {
					ts.Fatalf("usage: trim-newline file...")
				}
				for _, arg := range args {
					path := ts.MkAbs(arg)
					data, err := os.ReadFile(path)
					ts.Check(err)
					trimmed, ok := bytes.CutSuffix(data, []byte("\n"))
					if !ok {
						ts.Fatalf("%s does not end with a newline", arg)
					}
					ts.Check(os.WriteFile(path, trimmed, 0o666))
				}
			},
		},
	})
}
