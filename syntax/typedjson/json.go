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
	"encoding"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"reflect"

	"mvdan.cc/sh/v3/syntax"
)

// Encode is a shortcut for [EncodeOptions.Encode] with the default options.
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
	enc.SetEscapeHTML(false)
	if opts.Indent != "" {
		enc.SetIndent("", opts.Indent)
	}
	return enc.Encode(encVal.Interface())
}

func encodeValue(val reflect.Value) (reflect.Value, string) {
	switch val.Kind() {
	case reflect.Pointer:
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
		for field := range typ.Fields() {
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

		// Node methods are defined on struct pointer receivers.
		if node, _ := reflect.TypeAssert[syntax.Node](val.Addr()); node != nil {
			encodePos(enc.Field(1), node.Pos()) // posField
			encodePos(enc.Field(2), node.End()) // endField
		}
		// Do the rest of the fields.
		for i := 3; i < encTyp.NumField(); i++ {
			ftyp := encTyp.Field(i)
			fval := val.FieldByName(ftyp.Name)
			if ftyp.Type == exportedPosType {
				encodePos(enc.Field(i), fval.Interface().(syntax.Pos))
			} else {
				encElem, _ := encodeValue(fval)
				if encElem.IsValid() {
					enc.Field(i).Set(encElem)
				}
			}
		}

		// Addr helps prevent an allocation as we use any fields.
		return enc.Addr(), typ.Name()
	case reflect.Slice:
		n := val.Len()
		if n == 0 {
			break
		}
		enc := reflect.MakeSlice(anySliceType, n, n)
		for i := range n {
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
	case reflect.Uint8, reflect.Uint32:
		if val.Uint() == 0 {
			break
		}
		// Encode token-derived operator enums as their syntax string form
		// so the wire format stays stable as new tokens are added.
		if s, ok := reflect.TypeAssert[fmt.Stringer](val); ok {
			return reflect.ValueOf(s.String()), ""
		}
		return val, ""
	default:
		panic(val.Kind().String())
	}
	return noValue, ""
}

var (
	noValue reflect.Value

	anyType         = reflect.TypeFor[any]()
	anySliceType    = reflect.TypeFor[[]any]()
	posType         = reflect.TypeFor[syntax.Pos]()
	exportedPosType = reflect.TypeFor[*exportedPos]()

	// TODO(v4): derived fields like Type, Pos, and End should have clearly
	// different names to prevent confusion. For example: _type, _pos, _end.
	typeField = reflect.StructField{
		Name: "Type",
		Type: reflect.TypeFor[string](),
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

func encodePos(encPtr reflect.Value, val syntax.Pos) {
	// TODO: perhaps we should encode recovered positions, as that is still useful information.
	if !val.IsValid() {
		return
	}
	enc := reflect.New(exportedPosType.Elem())
	encPtr.Set(enc)
	enc = enc.Elem()

	enc.Field(0).SetUint(uint64(val.Offset()))
	enc.Field(1).SetUint(uint64(val.Line()))
	enc.Field(2).SetUint(uint64(val.Col()))
}

// posFieldNames are the fields of [exportedPos], which must all be present.
var posFieldNames = [...]string{"Offset", "Line", "Col"}

func decodePos(val reflect.Value, enc any) error {
	obj, ok := enc.(map[string]any)
	if !ok {
		return fmt.Errorf("cannot decode JSON %s into a position", jsonTypeName(enc))
	}
	if len(obj) != len(posFieldNames) {
		return fmt.Errorf("a position must contain exactly the fields %s", posFieldNames)
	}
	var nums [len(posFieldNames)]uint
	for i, name := range posFieldNames {
		fv, ok := obj[name]
		if !ok {
			return fmt.Errorf("a position must contain the field %q", name)
		}
		num, ok := fv.(float64)
		if !ok {
			return fmt.Errorf("cannot decode JSON %s into the position field %q", jsonTypeName(fv), name)
		}
		// Note that this is only a sanity bound;
		// [syntax.NewPos] clamps beyond its own tighter limits.
		u, ok := jsonUint(num)
		if !ok {
			return fmt.Errorf("the position field %q is out of range: %v", name, num)
		}
		nums[i] = uint(u)
	}
	val.Set(reflect.ValueOf(syntax.NewPos(nums[0], nums[1], nums[2])))
	return nil
}

// jsonUint converts a JSON number to an integer, reporting whether it is a
// non-negative integer which fits in a uint32, as no syntax node field is
// wider than that. The upper bound also keeps the conversion well defined.
func jsonUint(num float64) (uint64, bool) {
	if num < 0 || num > math.MaxUint32 || num != math.Trunc(num) {
		return 0, false
	}
	return uint64(num), true
}

// jsonTypeName describes a decoded JSON value in terms of JSON types,
// as the Go types that [encoding/json] decodes into are an implementation detail.
func jsonTypeName(enc any) string {
	switch enc.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case float64:
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		return fmt.Sprintf("%T", enc)
	}
}

// Decode is a shortcut for [DecodeOptions.Decode] with the default options.
func Decode(r io.Reader) (syntax.Node, error) {
	return DecodeOptions{}.Decode(r)
}

// DecodeOptions allows configuring how syntax nodes are encoded.
type DecodeOptions struct {
	// Empty for now; allows us to add options later.
}

// Decode reads a node from r in its typed JSON form,
// as described in the package documentation.
//
// Malformed input results in an error. Note that the resulting tree is only
// validated as far as its types and shape; just like a tree built by hand,
// it may be missing nodes which the printer and interpreter require.
func (opts DecodeOptions) Decode(r io.Reader) (syntax.Node, error) {
	var enc any
	if err := json.NewDecoder(r).Decode(&enc); err != nil {
		return nil, err
	}
	node := new(syntax.Node)
	if err := decodeValue(reflect.ValueOf(node).Elem(), enc); err != nil {
		return nil, err
	}
	if *node == nil {
		// Only a JSON null decodes into a nil node without an error.
		return nil, fmt.Errorf("cannot decode JSON null into a syntax node")
	}
	return *node, nil
}

var nodeByName = map[string]reflect.Type{
	"File": reflect.TypeFor[syntax.File](),
	"Word": reflect.TypeFor[syntax.Word](),

	"Lit":       reflect.TypeFor[syntax.Lit](),
	"SglQuoted": reflect.TypeFor[syntax.SglQuoted](),
	"DblQuoted": reflect.TypeFor[syntax.DblQuoted](),
	"ParamExp":  reflect.TypeFor[syntax.ParamExp](),
	"CmdSubst":  reflect.TypeFor[syntax.CmdSubst](),
	"CallExpr":  reflect.TypeFor[syntax.CallExpr](),
	"ArithmExp": reflect.TypeFor[syntax.ArithmExp](),
	"ProcSubst": reflect.TypeFor[syntax.ProcSubst](),
	"ExtGlob":   reflect.TypeFor[syntax.ExtGlob](),
	"BraceExp":  reflect.TypeFor[syntax.BraceExp](),

	"ArithmCmd":    reflect.TypeFor[syntax.ArithmCmd](),
	"BinaryCmd":    reflect.TypeFor[syntax.BinaryCmd](),
	"IfClause":     reflect.TypeFor[syntax.IfClause](),
	"ForClause":    reflect.TypeFor[syntax.ForClause](),
	"WhileClause":  reflect.TypeFor[syntax.WhileClause](),
	"CaseClause":   reflect.TypeFor[syntax.CaseClause](),
	"Block":        reflect.TypeFor[syntax.Block](),
	"Subshell":     reflect.TypeFor[syntax.Subshell](),
	"FuncDecl":     reflect.TypeFor[syntax.FuncDecl](),
	"TestClause":   reflect.TypeFor[syntax.TestClause](),
	"DeclClause":   reflect.TypeFor[syntax.DeclClause](),
	"LetClause":    reflect.TypeFor[syntax.LetClause](),
	"TimeClause":   reflect.TypeFor[syntax.TimeClause](),
	"CoprocClause": reflect.TypeFor[syntax.CoprocClause](),
	"TestDecl":     reflect.TypeFor[syntax.TestDecl](),

	"UnaryArithm":  reflect.TypeFor[syntax.UnaryArithm](),
	"BinaryArithm": reflect.TypeFor[syntax.BinaryArithm](),
	"ParenArithm":  reflect.TypeFor[syntax.ParenArithm](),
	"FlagsArithm":  reflect.TypeFor[syntax.FlagsArithm](),

	"UnaryTest":  reflect.TypeFor[syntax.UnaryTest](),
	"BinaryTest": reflect.TypeFor[syntax.BinaryTest](),
	"ParenTest":  reflect.TypeFor[syntax.ParenTest](),

	"WordIter":   reflect.TypeFor[syntax.WordIter](),
	"CStyleLoop": reflect.TypeFor[syntax.CStyleLoop](),
}

// decodeValue decodes enc, which comes from untrusted input, into val.
// Every reflect operation below must first check that val can hold the value,
// so that a mismatch results in an error rather than a panic.
func decodeValue(val reflect.Value, enc any) error {
	switch enc := enc.(type) {
	case map[string]any:
		typ := val.Type()
		if typeName, _ := enc["Type"].(string); typeName != "" {
			nodeType := nodeByName[typeName]
			if nodeType == nil {
				return fmt.Errorf("unknown type: %q", typeName)
			}
			if !reflect.PointerTo(nodeType).AssignableTo(typ) {
				return fmt.Errorf("cannot decode %s into %s", typeName, typ)
			}
			val.Set(reflect.New(nodeType))
		} else if val.Kind() == reflect.Pointer && val.IsNil() {
			val.Set(reflect.New(typ.Elem()))
		}
		for val.Kind() == reflect.Pointer || val.Kind() == reflect.Interface {
			if val.IsNil() {
				return fmt.Errorf(`missing "Type" to decode a JSON object into %s`, typ)
			}
			val = val.Elem()
		}
		if val.Kind() != reflect.Struct {
			return fmt.Errorf("cannot decode JSON object into %s", typ)
		}
		for name, fv := range enc {
			switch name {
			case "Type", "Pos", "End":
				// Type is already used above. Pos and End came from method calls.
				continue
			}
			fval := val.FieldByName(name)
			if !fval.IsValid() || !fval.CanSet() {
				return fmt.Errorf("unknown field for %s: %q", val.Type(), name)
			}
			if fval.Type() == posType {
				if err := decodePos(fval, fv); err != nil {
					return err
				}
				continue
			}
			if err := decodeValue(fval, fv); err != nil {
				return err
			}
		}
	case []any:
		if val.Kind() != reflect.Slice {
			return fmt.Errorf("cannot decode JSON array into %s", val.Type())
		}
		for _, encElem := range enc {
			elem := reflect.New(val.Type().Elem()).Elem()
			if err := decodeValue(elem, encElem); err != nil {
				return err
			}
			val.Set(reflect.Append(val, elem))
		}
	case string:
		if val.Kind() == reflect.String {
			val.SetString(enc)
			break
		}
		// Operators are encoded as their syntax form, such as "&&".
		if u, ok := textUnmarshaler(val); ok {
			return u.UnmarshalText([]byte(enc))
		}
		return fmt.Errorf("cannot decode JSON string into %s", val.Type())
	case float64:
		// Note that encoding/json defaults to float64 for numbers,
		// and that the kinds mirror those encoded by [encodeValue].
		switch val.Kind() {
		case reflect.Uint8, reflect.Uint32:
			if _, ok := textUnmarshaler(val); ok {
				// Operator integer values are not part of the wire format.
				return fmt.Errorf("cannot decode JSON number into %s; a string is required", val.Type())
			}
			u, ok := jsonUint(enc)
			if !ok || val.OverflowUint(u) {
				return fmt.Errorf("cannot decode the JSON number %v into %s", enc, val.Type())
			}
			val.SetUint(u)
		default:
			return fmt.Errorf("cannot decode JSON number into %s", val.Type())
		}
	default:
		// Note that a JSON null leaves the destination as its zero value.
		if enc != nil {
			encVal := reflect.ValueOf(enc)
			if !encVal.Type().AssignableTo(val.Type()) {
				return fmt.Errorf("cannot decode JSON %s into %s", jsonTypeName(enc), val.Type())
			}
			val.Set(encVal)
		}
	}
	return nil
}

// textUnmarshaler reports whether val is an operator enum,
// as those are encoded as strings rather than as their integer values.
// Note that val is always addressable,
// as [decodeValue] only writes to new values and to their fields.
func textUnmarshaler(val reflect.Value) (encoding.TextUnmarshaler, bool) {
	return reflect.TypeAssert[encoding.TextUnmarshaler](val.Addr())
}
