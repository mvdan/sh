// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

// Package typedjson allows encoding and decoding shell syntax trees as JSON.
// The decoding process needs to know what syntax node types to decode into,
// so the "typed JSON" requires "Type" keys in some syntax tree node objects:
//
//   - The root node
//   - Any node represented as an interface field in the parent Go type
//
// The types of all other nodes can be inferred from context alone.
//
// For the sake of efficiency and simplicity, the "Type" key
// described above must be first in each JSON object.
package typedjson

// TODO: encoding and decoding nodes other than File is untested.

import (
	"encoding/json"
	"fmt"
	"io"
	"reflect"

	"mvdan.cc/sh/v3/syntax"
)

// Encode is a shortcut for EncodeOptions.Encode, with the default options.
func Encode(w io.Writer, node syntax.Node) error {
	return EncodeOptions{}.Encode(w, node)
}

// EncodeOptions allows configuring how syntax nodes are encoded.
type EncodeOptions struct {
	Indent string // e.g. "\t"

	// Allows us to add options later.
}

// Encode writes node to w in its typed JSON form,
// as described in the package documentation.
func (opts EncodeOptions) Encode(w io.Writer, node syntax.Node) error {
	val := reflect.ValueOf(node)
	encVal, tname := encodeValue(val)
	if tname == "" {
		panic("node did not contain a named type?")
	}
	encVal.Elem().Field(0).SetString(tname)
	enc := json.NewEncoder(w)
	if opts.Indent != "" {
		enc.SetIndent("", opts.Indent)
	}
	return enc.Encode(encVal.Interface())
}

func encodeValue(val reflect.Value) (reflect.Value, string) {
	switch val.Kind() {
	case reflect.Ptr:
		if val.IsNil() {
			break
		}
		return encodeValue(val.Elem())
	case reflect.Interface:
		if val.IsNil() {
			break
		}
		enc, tname := encodeValue(val.Elem())
		if tname == "" {
			panic("interface did not contain a named type?")
		}
		enc.Elem().Field(0).SetString(tname)
		return enc, ""
	case reflect.Struct:
		// Construct a new struct with an optional Type, Pos and End,
		// and then all the visible fields which aren't positions.
		typ := val.Type()
		fields := []reflect.StructField{typeField, posField, endField}
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			typ := anyType
			if field.Type == posType {
				typ = exportedPosType
			}
			fields = append(fields, reflect.StructField{
				Name: field.Name,
				Type: typ,
				Tag:  `json:",omitempty"`,
			})
		}
		encTyp := reflect.StructOf(fields)
		enc := reflect.New(encTyp).Elem()

		// Pos methods are defined on struct pointer receivers.
		for i, name := range [...]string{"Pos", "End"} {
			if fn := val.Addr().MethodByName(name); fn.IsValid() {
				encodePos(enc.Field(1+i), fn.Call(nil)[0])
			}
		}
		// Do the rest of the fields.
		for i := 3; i < encTyp.NumField(); i++ {
			ftyp := encTyp.Field(i)
			fval := val.FieldByName(ftyp.Name)
			if ftyp.Type == exportedPosType {
				encodePos(enc.Field(i), fval)
			} else {
				encElem, _ := encodeValue(fval)
				if encElem.IsValid() {
					enc.Field(i).Set(encElem)
				}
			}
		}

		// Addr helps prevent an allocation as we use interface{} fields.
		return enc.Addr(), typ.Name()
	case reflect.Slice:
		n := val.Len()
		if n == 0 {
			break
		}
		enc := reflect.MakeSlice(anySliceType, n, n)
		for i := 0; i < n; i++ {
			elem := val.Index(i)
			encElem, _ := encodeValue(elem)
			enc.Index(i).Set(encElem)
		}
		return enc, ""
	case reflect.Bool:
		if val.Bool() {
			return val, ""
		}
	case reflect.String:
		if val.String() != "" {
			return val, ""
		}
	case reflect.Uint32:
		if val.Uint() != 0 {
			return val, ""
		}
	default:
		panic(val.Kind().String())
	}
	return noValue, ""
}

var (
	noValue reflect.Value

	anyType         = reflect.TypeOf((*interface{})(nil)).Elem() // interface{}
	anySliceType    = reflect.SliceOf(anyType)                   // []interface{}
	posType         = reflect.TypeOf((*syntax.Pos)(nil)).Elem()  // syntax.Pos
	exportedPosType = reflect.TypeOf((*exportedPos)(nil))        // *exportedPos

	// TODO(v4): derived fields like Type, Pos, and End should have clearly
	// different names to prevent confusion. For example: _type, _pos, _end.
	typeField = reflect.StructField{
		Name: "Type",
		Type: reflect.TypeOf((*string)(nil)).Elem(),
		Tag:  `json:",omitempty"`,
	}
	posField = reflect.StructField{
		Name: "Pos",
		Type: exportedPosType,
		Tag:  `json:",omitempty"`,
	}
	endField = reflect.StructField{
		Name: "End",
		Type: exportedPosType,
		Tag:  `json:",omitempty"`,
	}
)

type exportedPos struct {
	Offset, Line, Col uint
}

func encodePos(encPtr, val reflect.Value) {
	if !val.MethodByName("IsValid").Call(nil)[0].Bool() {
		return
	}
	enc := reflect.New(exportedPosType.Elem())
	encPtr.Set(enc)
	enc = enc.Elem()

	enc.Field(0).Set(val.MethodByName("Offset").Call(nil)[0])
	enc.Field(1).Set(val.MethodByName("Line").Call(nil)[0])
	enc.Field(2).Set(val.MethodByName("Col").Call(nil)[0])
}

func decodePos(val reflect.Value, enc map[string]interface{}) {
	offset := uint(enc["Offset"].(float64))
	line := uint(enc["Line"].(float64))
	column := uint(enc["Col"].(float64))
	val.Set(reflect.ValueOf(syntax.NewPos(offset, line, column)))
}

// Decode is a shortcut for DecodeOptions.Decode, with the default options.
func Decode(r io.Reader) (syntax.Node, error) {
	return DecodeOptions{}.Decode(r)
}

// DecodeOptions allows configuring how syntax nodes are encoded.
type DecodeOptions struct {
	// Empty for now; allows us to add options later.
}

// Decode writes node to w in its typed JSON form,
// as described in the package documentation.
func (opts DecodeOptions) Decode(r io.Reader) (syntax.Node, error) {
	var enc interface{}
	if err := json.NewDecoder(r).Decode(&enc); err != nil {
		return nil, err
	}
	node := new(syntax.Node)
	if err := decodeValue(reflect.ValueOf(node).Elem(), enc); err != nil {
		return nil, err
	}
	return *node, nil
}

var nodeByName = map[string]reflect.Type{
	"File": reflect.TypeOf((*syntax.File)(nil)).Elem(),
	"Word": reflect.TypeOf((*syntax.Word)(nil)).Elem(),

	"Lit":       reflect.TypeOf((*syntax.Lit)(nil)).Elem(),
	"SglQuoted": reflect.TypeOf((*syntax.SglQuoted)(nil)).Elem(),
	"DblQuoted": reflect.TypeOf((*syntax.DblQuoted)(nil)).Elem(),
	"ParamExp":  reflect.TypeOf((*syntax.ParamExp)(nil)).Elem(),
	"CmdSubst":  reflect.TypeOf((*syntax.CmdSubst)(nil)).Elem(),
	"CallExpr":  reflect.TypeOf((*syntax.CallExpr)(nil)).Elem(),
	"ArithmExp": reflect.TypeOf((*syntax.ArithmExp)(nil)).Elem(),
	"ProcSubst": reflect.TypeOf((*syntax.ProcSubst)(nil)).Elem(),
	"ExtGlob":   reflect.TypeOf((*syntax.ExtGlob)(nil)).Elem(),
	"BraceExp":  reflect.TypeOf((*syntax.BraceExp)(nil)).Elem(),

	"ArithmCmd":    reflect.TypeOf((*syntax.ArithmCmd)(nil)).Elem(),
	"BinaryCmd":    reflect.TypeOf((*syntax.BinaryCmd)(nil)).Elem(),
	"IfClause":     reflect.TypeOf((*syntax.IfClause)(nil)).Elem(),
	"ForClause":    reflect.TypeOf((*syntax.ForClause)(nil)).Elem(),
	"WhileClause":  reflect.TypeOf((*syntax.WhileClause)(nil)).Elem(),
	"CaseClause":   reflect.TypeOf((*syntax.CaseClause)(nil)).Elem(),
	"Block":        reflect.TypeOf((*syntax.Block)(nil)).Elem(),
	"Subshell":     reflect.TypeOf((*syntax.Subshell)(nil)).Elem(),
	"FuncDecl":     reflect.TypeOf((*syntax.FuncDecl)(nil)).Elem(),
	"TestClause":   reflect.TypeOf((*syntax.TestClause)(nil)).Elem(),
	"DeclClause":   reflect.TypeOf((*syntax.DeclClause)(nil)).Elem(),
	"LetClause":    reflect.TypeOf((*syntax.LetClause)(nil)).Elem(),
	"TimeClause":   reflect.TypeOf((*syntax.TimeClause)(nil)).Elem(),
	"CoprocClause": reflect.TypeOf((*syntax.CoprocClause)(nil)).Elem(),
	"TestDecl":     reflect.TypeOf((*syntax.TestDecl)(nil)).Elem(),

	"UnaryArithm":  reflect.TypeOf((*syntax.UnaryArithm)(nil)).Elem(),
	"BinaryArithm": reflect.TypeOf((*syntax.BinaryArithm)(nil)).Elem(),
	"ParenArithm":  reflect.TypeOf((*syntax.ParenArithm)(nil)).Elem(),

	"UnaryTest":  reflect.TypeOf((*syntax.UnaryTest)(nil)).Elem(),
	"BinaryTest": reflect.TypeOf((*syntax.BinaryTest)(nil)).Elem(),
	"ParenTest":  reflect.TypeOf((*syntax.ParenTest)(nil)).Elem(),

	"WordIter":   reflect.TypeOf((*syntax.WordIter)(nil)).Elem(),
	"CStyleLoop": reflect.TypeOf((*syntax.CStyleLoop)(nil)).Elem(),
}

func decodeValue(val reflect.Value, enc interface{}) error {
	switch enc := enc.(type) {
	case map[string]interface{}:
		if val.Kind() == reflect.Ptr && val.IsNil() {
			val.Set(reflect.New(val.Type().Elem()))
		}
		if typeName, _ := enc["Type"].(string); typeName != "" {
			typ := nodeByName[typeName]
			if typ == nil {
				return fmt.Errorf("unknown type: %q", typeName)
			}
			val.Set(reflect.New(typ))
		}
		for val.Kind() == reflect.Ptr || val.Kind() == reflect.Interface {
			val = val.Elem()
		}
		for name, fv := range enc {
			fval := val.FieldByName(name)
			switch name {
			case "Type", "Pos", "End":
				// Type is already used above. Pos and End came from method calls.
				continue
			}
			if !fval.IsValid() {
				return fmt.Errorf("unknown field for %s: %q", val.Type(), name)
			}
			if fval.Type() == posType {
				// TODO: don't panic on bad input
				decodePos(fval, fv.(map[string]interface{}))
				continue
			}
			if err := decodeValue(fval, fv); err != nil {
				return err
			}
		}
	case []interface{}:
		for _, encElem := range enc {
			elem := reflect.New(val.Type().Elem()).Elem()
			if err := decodeValue(elem, encElem); err != nil {
				return err
			}
			val.Set(reflect.Append(val, elem))
		}
	case float64:
		// Tokens and thus operators are uint32, but encoding/json defaults to float64.
		// TODO: reject invalid operators.
		u := uint64(enc)
		val.SetUint(u)
	default:
		if enc != nil {
			val.Set(reflect.ValueOf(enc))
		}
	}
	return nil
}
