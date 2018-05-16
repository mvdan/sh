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
	val := reflect.ValueOf(f)
	v, _ := recurse(val, val)
	enc := json.NewEncoder(w)
	if pretty {
		enc.SetIndent("", "\t")
	}
	return enc.Encode(v)
}

func recurse(val, valPtr reflect.Value) (interface{}, string) {
	switch val.Kind() {
	case reflect.Ptr:
		elem := val.Elem()
		if !elem.IsValid() {
			return nil, ""
		}
		return recurse(elem, val)
	case reflect.Interface:
		if val.IsNil() {
			return nil, ""
		}
		v, tname := recurse(val.Elem(), val)
		m := v.(map[string]interface{})
		m["Type"] = tname
		return m, ""
	case reflect.Struct:
		m := make(map[string]interface{}, val.NumField()+1)
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
			v, _ := recurse(fval, fval)
			switch ftyp.Name {
			case "StmtList":
				// inline their fields
				for name, v := range v.(map[string]interface{}) {
					m[name] = v
				}
			default:
				m[ftyp.Name] = v
			}
		}
		// use valPtr to find the method, as methods are defined on the
		// pointer values.
		if posMethod := valPtr.MethodByName("Pos"); posMethod.IsValid() {
			m["Pos"] = translatePos(posMethod.Call(nil)[0])
		}
		if posMethod := valPtr.MethodByName("End"); posMethod.IsValid() {
			m["End"] = translatePos(posMethod.Call(nil)[0])
		}
		return m, typ.Name()
	case reflect.Slice:
		l := make([]interface{}, val.Len())
		for i := 0; i < val.Len(); i++ {
			elem := val.Index(i)
			l[i], _ = recurse(elem.Addr(), elem)
		}
		return l, ""
	default:
		return val.Interface(), ""
	}
}

func translatePos(val reflect.Value) map[string]interface{} {
	return map[string]interface{}{
		"Offset": val.MethodByName("Offset").Call(nil)[0].Uint(),
		"Line":   val.MethodByName("Line").Call(nil)[0].Uint(),
		"Col":    val.MethodByName("Col").Call(nil)[0].Uint(),
	}
}
