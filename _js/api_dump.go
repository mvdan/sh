// Copyright (c) 2019, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

// +build ignore

// api_dump is a simple program to create a description of what the syntax
// package's API is. The main purpose is so that the JS package users can
// inspect the API programmatically, to generate documentation, code, etc.
//
// To run with Go 1.11 or later: GO111MODULE=on go run api_dump.go

package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"reflect"

	"golang.org/x/tools/go/packages"
)

type Package struct {
	Types map[string]NamedType `json:"types"`
	Funcs map[string]DocType   `json:"funcs"`
}

type NamedType struct {
	Doc  string      `json:"doc"`
	Type interface{} `json:"type"`

	EnumValues   []string `json:"enumvalues,omitempty"`
	Implementers []string `json:"implementers,omitempty"`

	Methods map[string]DocType `json:"methods"`
}

type DocType struct {
	Doc  string      `json:"doc"`
	Type interface{} `json:"type"`
}

func main() {
	cfg := &packages.Config{Mode: packages.LoadSyntax}
	pkgs, err := packages.Load(cfg, "mvdan.cc/sh/v3/syntax")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if packages.PrintErrors(pkgs) > 0 {
		os.Exit(1)
	}
	if len(pkgs) != 1 {
		panic("expected exactly one package")
	}

	dump := &Package{
		Types: map[string]NamedType{},
		Funcs: map[string]DocType{},
	}

	// from identifier position to its doc
	docs := make(map[token.Pos]*ast.CommentGroup)

	pkg := pkgs[0]
	for _, file := range pkg.Syntax {
		ast.Inspect(file, func(node ast.Node) bool {
			switch node := node.(type) {
			case *ast.FuncDecl:
				docs[node.Name.Pos()] = node.Doc
			case *ast.GenDecl:
				if len(node.Specs) != 1 {
					break
				}
				// TypeSpec.Doc is sometimes not passed on
				switch spec := node.Specs[0].(type) {
				case *ast.TypeSpec:
					spec.Doc = node.Doc
				}
			case *ast.TypeSpec:
				docs[node.Name.Pos()] = node.Doc
			case *ast.Field:
				if len(node.Names) != 1 {
					break
				}
				docs[node.Names[0].Pos()] = node.Doc
			case *ast.ValueSpec:
				if len(node.Names) != 1 {
					break
				}
				docs[node.Names[0].Pos()] = node.Doc
			}
			return true
		})
	}

	scope := pkg.Types.Scope()
	var allImpls []*types.Pointer
	var allConsts []*types.Const

	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		if !obj.Exported() {
			continue
		}
		switch obj := obj.(type) {
		case *types.TypeName:
			// not interfaces
			if _, ok := obj.Type().(*types.Interface); !ok {
				// include pointer receivers too
				allImpls = append(allImpls, types.NewPointer(obj.Type()))
			}
		case *types.Const:
			allConsts = append(allConsts, obj)
		}
	}

	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		if !obj.Exported() {
			continue
		}
		if fn, ok := obj.(*types.Func); ok {
			dump.Funcs[fn.Name()] = DocType{
				Doc:  docs[fn.Pos()].Text(),
				Type: dumpType(docs, fn.Type()),
			}
			continue
		}
		tname, ok := obj.(*types.TypeName)
		if !ok {
			continue
		}
		name := tname.Name()
		named, ok := obj.Type().(*types.Named)
		if !ok {
			continue
		}

		under := named.Underlying()
		dumpNamed := NamedType{
			Doc:     docs[tname.Pos()].Text(),
			Type:    dumpType(docs, under),
			Methods: map[string]DocType{},
		}
		switch under := under.(type) {
		case *types.Basic:
			if under.Info()&types.IsInteger == 0 {
				break
			}
			for _, cnst := range allConsts {
				if cnst.Type() == named {
					dumpNamed.EnumValues = append(dumpNamed.EnumValues, cnst.Name())
				}
			}
		case *types.Interface:
			for _, typ := range allImpls {
				if types.Implements(typ, under) {
					dumpNamed.Implementers = append(dumpNamed.Implementers, typ.Elem().String())
				}
			}
		}
		for i := 0; i < named.NumMethods(); i++ {
			fn := named.Method(i)
			if !fn.Exported() {
				continue
			}
			dumpNamed.Methods[fn.Name()] = DocType{
				Doc:  docs[fn.Pos()].Text(),
				Type: dumpType(docs, fn.Type()),
			}
		}
		dump.Types[name] = dumpNamed
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "\t")
	if err := enc.Encode(dump); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
}

func dumpType(docs map[token.Pos]*ast.CommentGroup, typ types.Type) interface{} {
	dump := map[string]interface{}{}
	switch typ := typ.(type) {
	case *types.Interface:
		dump["kind"] = "interface"
		methods := map[string]DocType{}
		for i := 0; i < typ.NumMethods(); i++ {
			fn := typ.Method(i)
			if !fn.Exported() {
				continue
			}
			methods[fn.Name()] = DocType{
				Doc:  docs[fn.Pos()].Text(),
				Type: dumpType(docs, fn.Type()),
			}
		}
		dump["methods"] = methods
		return dump
	case *types.Struct:
		dump["kind"] = "struct"
		type Field struct {
			Doc      string      `json:"doc"`
			Type     interface{} `json:"type"`
			Embedded bool        `json:"embedded"`
		}
		fields := map[string]Field{}
		for i := 0; i < typ.NumFields(); i++ {
			fd := typ.Field(i)
			if !fd.Exported() {
				continue
			}
			fields[fd.Name()] = Field{
				Doc:      docs[fd.Pos()].Text(),
				Type:     dumpType(docs, fd.Type()),
				Embedded: fd.Embedded(),
			}
		}
		dump["fields"] = fields
		return dump
	case *types.Slice:
		dump["kind"] = "list"
		dump["elem"] = dumpType(docs, typ.Elem())
		return dump
	case *types.Pointer:
		dump["kind"] = "pointer"
		dump["elem"] = dumpType(docs, typ.Elem())
		return dump
	case *types.Signature:
		dump["kind"] = "function"
		dump["params"] = dumpTuple(docs, typ.Params())
		dump["results"] = dumpTuple(docs, typ.Results())
		return dump
	case *types.Basic:
		return typ.String()
	case *types.Named:
		return typ.String()
	}
	panic("TODO: " + reflect.TypeOf(typ).String())
}

func dumpTuple(docs map[token.Pos]*ast.CommentGroup, tuple *types.Tuple) []interface{} {
	typs := make([]interface{}, 0)
	for i := 0; i < tuple.Len(); i++ {
		vr := tuple.At(i)
		typs = append(typs, map[string]interface{}{
			"name": vr.Name(),
			"type": dumpType(docs, vr.Type()),
		})
	}
	return typs
}
