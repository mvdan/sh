// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package main

import (
	"encoding/json"
	"go/ast"
	"io"
	"reflect"

	"mvdan.cc/sh/syntax"
)

func writeJSON(w io.Writer, f *syntax.File, pretty bool) error {
	v, _ := recurse(reflect.ValueOf(f))
	enc := json.NewEncoder(w)
	if pretty {
		enc.SetIndent("", "\t")
	}
	return enc.Encode(v)
}

func recurse(val reflect.Value) (interface{}, string) {
	switch val.Kind() {
	case reflect.Ptr:
		elem := val.Elem()
		if !elem.IsValid() {
			return nil, ""
		}
		return recurse(elem)
	case reflect.Interface:
		if val.IsNil() {
			return nil, ""
		}
		v, tname := recurse(val.Elem())
		m := v.(map[string]interface{})
		m["Type"] = tname
		return m, ""
	case reflect.Struct:
		m := make(map[string]interface{}, val.NumField()+1)
		addField := func(name string, v interface{}) {
			switch x := v.(type) {
			case bool:
				if !x {
					return
				}
			case string:
				if x == "" {
					return
				}
			case []interface{}:
				if len(x) == 0 {
					return
				}
			case nil:
				return
			}
			m[name] = v
		}
		typ := val.Type()
		for i := 0; i < val.NumField(); i++ {
			ftyp := typ.Field(i)
			if ftyp.Type.Name() == "Pos" {
				continue
			}
			if !ast.IsExported(ftyp.Name) {
				continue
			}
			fval := val.Field(i)
			v, _ := recurse(fval)
			switch ftyp.Name {
			case "StmtList":
				// inline their fields
				m := v.(map[string]interface{})
				for name, v := range m {
					addField(name, v)
				}
			default:
				addField(ftyp.Name, v)
			}
		}
		return m, typ.Name()
	case reflect.Slice:
		l := make([]interface{}, val.Len())
		for i := 0; i < val.Len(); i++ {
			l[i], _ = recurse(val.Index(i))
		}
		return l, ""
	default:
		return val.Interface(), ""
	}
}
