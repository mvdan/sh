// Copyright (c) 2026, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package typedjson_test

import (
	"encoding"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/go-quicktest/qt"

	"mvdan.cc/sh/v3/syntax"
	"mvdan.cc/sh/v3/syntax/typedjson"
)

// schema is the subset of JSON Schema which schema.json uses.
type schema struct {
	Ref        string             `json:"$ref"`
	Type       string             `json:"type"`
	Const      string             `json:"const"`
	Enum       []any              `json:"enum"`
	Minimum    any                `json:"minimum"`
	Maximum    any                `json:"maximum"`
	Items      *schema            `json:"items"`
	Properties map[string]*schema `json:"properties"`
	Required   []string           `json:"required"`
	AnyOf      []*schema          `json:"anyOf"`
	Defs       map[string]*schema `json:"$defs"`

	// AdditionalProperties is false for closed objects, which let the
	// anyOf lists above tell nodes apart by their Type property.
	AdditionalProperties any `json:"additionalProperties"`
}

func (s *schema) UnmarshalJSON(data []byte) error {
	if string(data) == "true" { // allows any value
		return nil
	}
	type plain schema
	return json.Unmarshal(data, (*plain)(s))
}

// describe summarizes the shape of a property, like "array of ref:Stmt".
func (s *schema) describe() string {
	switch {
	case s.Ref != "":
		return "ref:" + strings.TrimPrefix(s.Ref, "#/$defs/")
	case s.Const != "":
		return s.Const
	case s.Items != nil:
		return "array of " + s.Items.describe()
	}
	return s.Type
}

// describeGo is like [schema.describe] for a Go field type.
func describeGo(typ reflect.Type) string {
	switch typ.Kind() {
	case reflect.Pointer:
		return describeGo(typ.Elem())
	case reflect.Slice:
		return "array of " + describeGo(typ.Elem())
	case reflect.Bool:
		return "boolean"
	case reflect.String:
		return "string"
	}
	return "ref:" + typ.Name()
}

// TestSchema checks that schema.json, generated from schema.cue,
// describes every syntax type reachable from any node.
//
// TODO(cue-lang/cue#4555): also validate encoded nodes against schema.json
// via cue, which currently reports structural cycles for nested nodes.
func TestSchema(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("schema.json")
	qt.Assert(t, qt.IsNil(err))
	var root schema
	qt.Assert(t, qt.IsNil(json.Unmarshal(data, &root)))
	// The root is the Node interface.
	qt.Assert(t, qt.IsNil(root.Defs["Node"]))
	root.Defs["Node"] = &root

	// Decoding a node with only a type gives us its Go type.
	var nodeTypes []reflect.Type
	for _, name := range allNodeNames {
		node, err := typedjson.Decode(strings.NewReader(fmt.Sprintf(`{"Type":%q}`, name)))
		qt.Assert(t, qt.IsNil(err))
		nodeTypes = append(nodeTypes, reflect.TypeOf(node))
	}
	nodeIface := reflect.TypeFor[syntax.Node]()
	textUnmarshaler := reflect.TypeFor[encoding.TextUnmarshaler]()

	seen := make(map[string]bool)
	var check func(typ reflect.Type)
	check = func(typ reflect.Type) {
		for typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Slice {
			typ = typ.Elem()
		}
		if seen[typ.Name()] || typ.Kind() == reflect.Bool || typ.Kind() == reflect.String {
			return
		}
		seen[typ.Name()] = true
		def := root.Defs[typ.Name()]
		qt.Assert(t, qt.IsNotNil(def), qt.Commentf("missing definition for %s", typ))

		switch {
		case typ == reflect.TypeFor[syntax.Pos]():
			qt.Assert(t, qt.ContentEquals(def.Required, []string{"Offset", "Line", "Col"}))
			qt.Assert(t, qt.ContentEquals(slices.Collect(maps.Keys(def.Properties)), def.Required))
			qt.Assert(t, qt.Equals(def.AdditionalProperties, false))
			for name, prop := range def.Properties {
				// The bounds which the decoder enforces on positions.
				want := &schema{Type: "integer", Minimum: 0.0, Maximum: float64(math.MaxUint32)}
				qt.Assert(t, qt.DeepEquals(prop, want), qt.Commentf("Pos.%s", name))
			}
		case typ == reflect.TypeFor[syntax.OptState]():
			want := []any{float64(syntax.OptUnset), float64(syntax.OptOn), float64(syntax.OptOff)}
			qt.Assert(t, qt.ContentEquals(def.Enum, want))
		case reflect.PointerTo(typ).Implements(textUnmarshaler):
			// Operators are encoded as strings. Try every token until the
			// stringer runs out of names, keeping those which decode back.
			var want []any
			op := reflect.New(typ)
			for i := uint64(0); ; i++ {
				op.Elem().SetUint(i)
				str := fmt.Sprint(op.Elem())
				if strings.HasPrefix(str, "token(") {
					break
				}
				if err := op.Interface().(encoding.TextUnmarshaler).UnmarshalText([]byte(str)); err == nil {
					want = append(want, str)
				}
			}
			qt.Assert(t, qt.ContentEquals(def.Enum, want), qt.Commentf("%s", typ))
		case typ.Kind() == reflect.Interface:
			var want []string
			for _, nodeType := range nodeTypes {
				if nodeType.Implements(typ) {
					want = append(want, describeGo(nodeType))
					check(nodeType)
				}
			}
			var got []string
			for _, s := range def.AnyOf {
				got = append(got, s.describe())
			}
			qt.Assert(t, qt.ContentEquals(got, want), qt.Commentf("%s", typ))
			qt.Assert(t, qt.DeepEquals(def.Required, []string{"Type"}), qt.Commentf("%s", typ))
		case typ.Kind() == reflect.Struct:
			want := make(map[string]string)
			if reflect.PointerTo(typ).Implements(nodeIface) {
				want["Type"] = typ.Name()
				want["Pos"] = "ref:Pos"
				want["End"] = "ref:Pos"
			}
			for field := range typ.Fields() {
				want[field.Name] = describeGo(field.Type)
				check(field.Type)
			}
			got := make(map[string]string)
			for name, prop := range def.Properties {
				got[name] = prop.describe()
			}
			qt.Assert(t, qt.DeepEquals(got, want), qt.Commentf("%s", typ))
			qt.Assert(t, qt.HasLen(def.Required, 0), qt.Commentf("%s", typ))
			qt.Assert(t, qt.Equals(def.AdditionalProperties, false), qt.Commentf("%s", typ))
		default:
			t.Fatalf("unexpected type %s", typ)
		}
	}
	check(nodeIface)

	// No definitions should be left unchecked.
	qt.Assert(t, qt.ContentEquals(slices.Collect(maps.Keys(root.Defs)), slices.Collect(maps.Keys(seen))))
}
