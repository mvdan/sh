// Copyright (c) 2018, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package shell

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"reflect"
	"strings"
	"testing"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/syntax"

	"github.com/kr/pretty"
)

var mapTests = []struct {
	in   string
	want map[string]expand.Variable
}{
	{
		"a=x; b=y",
		map[string]expand.Variable{
			"a": {Kind: expand.String, Str: "x"},
			"b": {Kind: expand.String, Str: "y"},
		},
	},
	{
		"a=x; a=y; X=(a b c)",
		map[string]expand.Variable{
			"a": {Kind: expand.String, Str: "y"},
			"X": {Kind: expand.Indexed, List: []string{"a", "b", "c"}},
		},
	},
	{
		"a=$(echo foo | sed 's/o/a/g')",
		map[string]expand.Variable{
			"a": {Kind: expand.String, Str: "faa"},
		},
	},
}

var errTests = []struct {
	in   string
	want string
}{
	{
		"a=b; exit 1",
		"exit status 1",
	},
}

func TestSourceNode(t *testing.T) {
	for i := range mapTests {
		t.Run(fmt.Sprintf("%02d", i), func(t *testing.T) {
			tc := mapTests[i]
			t.Parallel()
			p := syntax.NewParser()
			file, err := p.Parse(strings.NewReader(tc.in), "")
			if err != nil {
				t.Fatal(err)
			}
			got, err := SourceNode(context.Background(), file)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(tc.want, got) {
				t.Fatal(strings.Join(pretty.Diff(tc.want, got), "\n"))
			}
		})
	}
}

func TestSourceNodeErr(t *testing.T) {
	for i := range errTests {
		t.Run(fmt.Sprintf("%02d", i), func(t *testing.T) {
			tc := errTests[i]
			t.Parallel()
			p := syntax.NewParser()
			file, err := p.Parse(strings.NewReader(tc.in), "")
			if err != nil {
				t.Fatal(err)
			}
			_, err = SourceNode(context.Background(), file)
			if err == nil {
				t.Fatal("wanted non-nil error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not match %q", err, tc.want)
			}
		})
	}
}

func TestSourceFileContext(t *testing.T) {
	t.Parallel()
	tf, err := ioutil.TempFile("", "sh-shell")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tf.Name())
	const src = "cat" // block forever
	if _, err := tf.WriteString(src); err != nil {
		t.Fatal(err)
	}
	if err := tf.Close(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() {
		_, err := SourceFile(ctx, tf.Name())
		errc <- err
	}()
	cancel()
	err = <-errc
	want := "context canceled"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not match %q", err, want)
	}
}
