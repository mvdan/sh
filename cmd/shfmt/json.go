// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package main

import (
	"encoding/json"
	"go/ast"
	"io"
	"reflect"

	"mvdan.cc/sh/v3/syntax"
)

func writeJSON(w io.Writer, node syntax.Node, pretty bool) error {
	val := reflect.ValueOf(node)
	v, _ := encode(val)
	enc := json.NewEncoder(w)
	if pretty {
		enc.SetIndent("", "\t")
	}
	return enc.Encode(v)
}

func encode(val reflect.Value) (interface{}, string) {
	switch val.Kind() {
	case reflect.Ptr:
		elem := val.Elem()
		if !elem.IsValid() {
			return nil, ""
		}
		return encode(elem)
	case reflect.Interface:
		if val.IsNil() {
			return nil, ""
		}
		v, tname := encode(val.Elem())
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
			v, _ := encode(fval)
			m[ftyp.Name] = v
		}
		// Pos methods are defined on struct pointer receivers.
		for _, name := range [...]string{"Pos", "End"} {
			if fn := val.Addr().MethodByName(name); fn.IsValid() {
				m[name] = translatePos(fn.Call(nil)[0])
			}
		}
		return m, typ.Name()
	case reflect.Slice:
		l := make([]interface{}, val.Len())
		for i := 0; i < val.Len(); i++ {
			elem := val.Index(i)
			l[i], _ = encode(elem)
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
