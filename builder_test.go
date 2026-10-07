package jsonfast

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// defaultBuilder is the capacity New documents for a capacity below 1.
const defaultBuilder = 256

func TestNewGivesTheCapacityOrTheDefault(t *testing.T) {
	for _, tc := range []struct{ capacity, want int }{
		{2 * defaultBuilder, 2 * defaultBuilder}, {1, 1}, {0, defaultBuilder}, {-1, defaultBuilder},
	} {
		if b := New(tc.capacity); b.Len() != 0 || cap(b.buf) != tc.want || b.needSep {
			t.Errorf("New(%d) holds %d bytes with capacity %d, want none with %d",
				tc.capacity, b.Len(), cap(b.buf), tc.want)
		}
	}
	assertAllocs(t, 1, func() { New(0).AddNullElement() })
	for _, n := range tooLarge() {
		if !panics(func() { New(n) }) {
			t.Errorf("New(%d) returned, want a panic: a capacity past what a byte slice can hold panics", n)
		}
	}
}

func TestBuilderZeroValueIsReady(t *testing.T) {
	var b Builder
	b.BeginObject()
	b.AddStringField("k", "v")
	b.AddIntField("n", 1)
	b.EndObject()
	if got, want := string(b.Bytes()), `{"k":"v","n":1}`; got != want {
		t.Fatalf("zero Builder wrote %s, want %s", got, want)
	}
	var bare Builder
	bare.AddStringField("first", "no leading comma")
	if got, want := string(bare.Bytes()), `"first":"no leading comma"`; got != want {
		t.Fatalf("zero Builder wrote %s, want %s", got, want)
	}
}

func TestBuilderResetEmptiesAndKeepsTheBuffer(t *testing.T) {
	b := New(0)
	b.BeginObject()
	b.AddStringField("r", "s")
	b.Reset()
	if b.Len() != 0 || len(b.Bytes()) != 0 || cap(b.buf) != defaultBuilder || b.needSep {
		t.Fatalf("Reset left %d bytes, capacity %d and separator %v, want 0, %d and false",
			b.Len(), cap(b.buf), b.needSep, defaultBuilder)
	}
	b.AddStringField("r", "s")
	if got, want := string(b.Bytes()), `"r":"s"`; got != want {
		t.Fatalf("after Reset the Builder wrote %s, want %s", got, want)
	}
}

func TestBuilderBytesAliasesTheBuffer(t *testing.T) {
	b := New(0)
	b.AppendRawString(plainText)
	if got := b.Bytes(); &got[0] != &b.buf[0] || b.Len() != len(plainText) {
		t.Fatalf("Bytes = %q, Len = %d, want an alias of the buffer", got, b.Len())
	}
}

func TestBuilderGrowMakesRoomOnce(t *testing.T) {
	const room = 1 << 10
	b := New(1)
	b.AppendRawString(plainText)
	b.Grow(room)
	if cap(b.buf)-b.Len() < room || string(b.Bytes()) != plainText {
		t.Fatalf("Grow(%d) left capacity %d for %q, want room for %d more", room, cap(b.buf), b.Bytes(), room)
	}
	before := cap(b.buf)
	b.Grow(room / 2)
	if cap(b.buf) != before {
		t.Fatalf("Grow with room reallocated: capacity %d, want %d", cap(b.buf), before)
	}
	filler := make([]byte, room)
	assertAllocs(t, 1, func() {
		var grown Builder
		grown.Grow(room)
		grown.AppendRaw(filler)
	})
	for _, n := range append([]int{-1}, tooLarge()...) {
		got, want := panics(func() { b.Grow(n) }), panics(func() { _ = slices.Grow(b.buf, n) })
		if !got || got != want {
			t.Errorf("Grow(%d) panicked: %v; slices.Grow panicked: %v, want both", n, got, want)
		}
	}
}

func TestBuilderOpenersFormAClosedMatrix(t *testing.T) {
	key := NewFieldKey("fk")
	b := New(0)
	b.BeginArray()
	b.BeginArrayElement()
	b.EndArray()
	b.BeginObjectElement()
	b.BeginArrayField("a")
	b.BeginArrayElement()
	b.AddInt64Element(1)
	b.EndArray()
	b.BeginObjectElement()
	b.EndObject()
	b.EndArray()
	b.BeginArrayFieldKey(key)
	b.AddStringElement("x")
	b.EndArray()
	b.BeginObjectField("o")
	b.BeginObjectFieldKey(key)
	b.EndObject()
	b.EndObject()
	b.EndObject()
	b.BeginArrayElement()
	b.EndArray()
	b.EndArray()
	want := `[[],{"a":[[1],{}],"fk":["x"],"o":{"fk":{}}},[]]`
	if got := string(b.Bytes()); got != want || !json.Valid(b.Bytes()) {
		t.Fatalf("openers wrote %s, want %s", got, want)
	}
}

func TestBuilderEndObjectAndEndArraySetTheSeparator(t *testing.T) {
	b := New(0)
	b.BeginObject()
	b.BeginObjectField("empty")
	b.EndObject()
	b.AddIntField("after", 1)
	b.BeginArrayField("list")
	b.EndArray()
	b.AddNullField("tail")
	b.EndObject()
	want := `{"empty":{},"after":1,"list":[],"tail":null}`
	if got := string(b.Bytes()); got != want || !json.Valid(b.Bytes()) {
		t.Fatalf("Builder wrote %s, want %s", got, want)
	}
}

func TestBuilderAddFieldsWriteExactJSON(t *testing.T) {
	for _, tc := range []struct {
		add  func(b *Builder)
		want string
	}{
		{func(b *Builder) { b.AddStringField("msg", "a\"b\\c\nd") }, `"msg":"a\"b\\c\nd"`},
		{func(b *Builder) { b.AddStringField("a\"b\n\xff", "") }, `"a\"b\n` + "\uFFFD" + `":""`},
		{func(b *Builder) { b.AddStringArrayField("ids", []string{"a", `c"d`}) }, `"ids":["a","c\"d"]`},
		{func(b *Builder) { b.AddStringArrayField("none", nil) }, `"none":[]`},
		{func(b *Builder) { b.AddRawJSONField("r", []byte(`{"x":[1]}`)) }, `"r":{"x":[1]}`},
		{func(b *Builder) { b.AddBoolField("t", true) }, `"t":true`},
		{func(b *Builder) { b.AddBoolField("f", false) }, `"f":false`},
		{func(b *Builder) { b.AddNullField("n") }, `"n":null`},
		{func(b *Builder) { b.AddIntField("i", -42) }, `"i":-42`},
		{func(b *Builder) { b.AddIntField("i", math.MinInt) }, `"i":` + strconv.Itoa(math.MinInt)},
		{func(b *Builder) { b.AddInt64Field("i", math.MaxInt64) }, `"i":9223372036854775807`},
		{func(b *Builder) { b.AddUint64Field("u", math.MaxUint64) }, `"u":18446744073709551615`},
		{func(b *Builder) { b.AddFloat64Field("f", math.SmallestNonzeroFloat64) }, `"f":5e-324`},
		{func(b *Builder) { b.AddFloat64Field("f", math.NaN()) }, `"f":null`},
		{func(b *Builder) { b.AddRawBytesField([]byte(`k\n`), []byte("7")) }, `"k\n":7`},
	} {
		b := New(0)
		tc.add(b)
		if got := string(b.Bytes()); got != tc.want {
			t.Errorf("Builder wrote %s, want %s", got, tc.want)
		}
		tc.add(b)
		if got, want := string(b.Bytes()), tc.want+","+tc.want; got != want {
			t.Errorf("a second field wrote %s, want %s", got, want)
		}
	}
}

func TestBuilderAddElementsFormatLikeFields(t *testing.T) {
	b := New(0)
	b.AddStringElement("bare")
	if got := string(b.Bytes()); got != `"bare"` {
		t.Fatalf("a first element wrote %s, want a bare string", got)
	}
	b.Reset()
	b.BeginArray()
	b.AddStringElement("a\"\n\xff")
	b.AddInt64Element(math.MinInt64)
	b.AddInt64Element(0)
	b.AddIntElement(math.MaxInt)
	b.AddUint64Element(math.MaxUint64)
	b.AddFloat64Element(math.MaxFloat64)
	b.AddFloat64Element(math.SmallestNonzeroFloat64)
	b.AddFloat64Element(math.Inf(-1))
	b.AddBoolElement(true)
	b.AddBoolElement(false)
	b.AddNullElement()
	b.AddRawJSONElement([]byte(`{"r":[1]}`))
	b.EndArray()
	want := `["a\"\n` + replacement + `",-9223372036854775808,0,` + strconv.Itoa(math.MaxInt) +
		`,18446744073709551615,1.7976931348623157e+308,5e-324,null,true,false,null,{"r":[1]}]`
	if got := string(b.Bytes()); got != want || !json.Valid(b.Bytes()) {
		t.Fatalf("elements wrote %s, want %s", got, want)
	}
}

func TestBuilderAddRawBytesFieldOpensAContainerUnderAnInputName(t *testing.T) {
	b := New(0)
	b.BeginObject()
	b.AddRawBytesField([]byte("key"), []byte(`"value"`))
	b.AddRawBytesField([]byte(`caf\u00e9`), nil)
	b.BeginObject()
	b.AddRawBytesField([]byte("n"), []byte("42"))
	b.EndObject()
	b.AddRawBytesField([]byte("list"), nil)
	b.BeginArray()
	b.AddBoolElement(true)
	b.EndArray()
	b.EndObject()
	want := `{"key":"value","caf\u00e9":{"n":42},"list":[true]}`
	if got := string(b.Bytes()); got != want {
		t.Fatalf("Builder wrote %s, want %s", got, want)
	}
}

func TestBuilderAddStringBytesWritersWriteTheStringOfTheBytes(t *testing.T) {
	for _, p := range []string{"", plainText, "a\"b\\c\n\x00\x1f", cafe, "<&>\u2028\u2029", "\xff\xfe"} {
		b := New(0)
		b.BeginArray()
		b.AddStringBytesElement([]byte(p))
		b.BeginObjectElement()
		b.AddStringBytesField(plainText, []byte(p))
		b.EndObject()
		b.EndArray()
		str := stdJSON(t, string([]rune(p)))
		want := "[" + str + `,{"` + plainText + `":` + str + "}]"
		if got := encodingJSONForm(b.Bytes()); got != want {
			t.Errorf("the StringBytes writers wrote %s for %q, want %s", b.Bytes(), p, want)
		}
	}
}

func TestBuilderAddStringArrayElementWritesOneArrayPerElement(t *testing.T) {
	b := New(0)
	b.BeginArray()
	b.AddStringArrayElement([]string{"a", `b"`})
	b.AddStringArrayElement(nil)
	b.AddStringArrayElement([]string{})
	b.EndArray()
	if got, want := string(b.Bytes()), `[["a","b\""],[],[]]`; got != want {
		t.Fatalf("AddStringArrayElement wrote %s, want %s", got, want)
	}
}

func TestBuilderEmptyRawValueLeavesTheValueToTheNextWriter(t *testing.T) {
	b := New(0)
	b.BeginObject()
	b.AddRawJSONField("a", nil)
	b.BeginArray()
	b.AddRawJSONElement(nil)
	b.BeginObject()
	b.EndObject()
	b.AddRawJSONElement([]byte{})
	b.AppendRawString(litTrue)
	b.EndArray()
	b.AddRawJSONField("b", nil)
	b.AppendRaw([]byte(oneArray))
	b.EndObject()
	if got, want := string(b.Bytes()), `{"a":[{},true],"b":[1]}`; got != want {
		t.Fatalf("empty raw values wrote %s, want %s", got, want)
	}
}

func TestBuilderAddRawMembersSplicesTheMembersOfAnObject(t *testing.T) {
	for _, tc := range []struct {
		write func(b *Builder)
		want  string
	}{
		{func(b *Builder) { b.AddRawMembers([]byte(" { \"a\" : 1 , \"b\":[2] }\n")) }, `{"a" : 1 , "b":[2],"z":0}`},
		{func(b *Builder) { b.AddRawMembers([]byte(emptyObject)) }, `{"z":0}`},
		{func(b *Builder) { b.AddRawMembers([]byte(" {\t\n } ")) }, `{"z":0}`},
		{func(b *Builder) { b.AddIntField("y", 1); b.AddRawMembers([]byte(objectA1)) }, `{"y":1,"a":1,"z":0}`},
		{func(b *Builder) { b.AddRawMembers([]byte(objectA1)); b.AddRawMembers([]byte(`{"b":{}}`)) },
			`{"a":1,"b":{},"z":0}`},
	} {
		b := New(0)
		b.BeginObject()
		tc.write(b)
		b.AddIntField("z", 0)
		b.EndObject()
		if got := string(b.Bytes()); got != tc.want || !json.Valid(b.Bytes()) {
			t.Errorf("AddRawMembers wrote %s, want %s", got, tc.want)
		}
	}
	for _, raw := range []string{"", "{", "}", "}{", plainText, " { "} {
		if panics(func() { New(0).AddRawMembers([]byte(raw)) }) {
			t.Errorf("AddRawMembers(%q) panicked, want no panic", raw)
		}
	}
}

func FuzzBuilderAddRawMembers(f *testing.F) {
	for _, s := range []string{emptyObject, objectA1, ` { "a" : [1, {"b":2}] , "c":"d" } `, repeatedA, oneArray} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if trimmed := bytes.TrimLeft(data, " \t\r\n"); !json.Valid(data) || len(trimmed) == 0 || trimmed[0] != '{' {
			return
		}
		b := New(0)
		b.BeginObject()
		b.AddRawMembers(data)
		b.AddNullField("z")
		b.EndObject()
		gotNames, gotValues, err := decoderMembers(b.Bytes())
		noError(t, err)
		names, values, err := decoderMembers(data)
		noError(t, err)
		if strictJSON(data) && !slices.Contains(names, "z") {
			strictlyRead(t, b.Bytes())
		}
		names, values = append(names, "z"), append(values, json.RawMessage(litNull))
		if !slices.Equal(gotNames, names) || !slices.EqualFunc(gotValues, values, rawEqual) {
			t.Fatalf("AddRawMembers(%q) wrote %s: members %q, want %q", data, b.Bytes(), gotNames, names)
		}
	})
}

// familyExceptions maps each kind that lacks a form of its family to the reason.
func familyExceptions() map[string]string {
	return map[string]string{
		"RawBytes": "its name is written as it is: a FieldKey is that same idea, and " +
			"AddRawJSONElement its element form",
	}
}

// valueParam returns the name the value parameter of the writers of kind takes.
func valueParam(kind string) string {
	switch {
	case strings.HasPrefix(kind, "Raw"):
		return "raw"
	case kind == "StringArray":
		return "values"
	case strings.HasPrefix(kind, "Time"):
		return "t"
	}
	return "v"
}

// builderWriters returns the parameter names of every Builder method named Add*,
// by method name, as the source files of the package declare them.
func builderWriters() (map[string][]string, error) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		return nil, fmt.Errorf("glob: %w", err)
	}
	writers := map[string][]string{}
	fset := token.NewFileSet()
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fset, file, nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, fmt.Errorf("parse: %w", err)
		}
		addWriters(writers, parsed)
	}
	return writers, nil
}

// addWriters records in writers the parameter names of each method of file
// named Add*.
func addWriters(writers map[string][]string, file *ast.File) {
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || !strings.HasPrefix(fn.Name.Name, "Add") {
			continue
		}
		var params []string
		for _, field := range fn.Type.Params.List {
			for _, n := range field.Names {
				params = append(params, n.Name)
			}
		}
		writers[fn.Name.Name] = params
	}
}

// The forms of a writer family, as the suffixes of their method names.
const (
	formField    = "Field"
	formFieldKey = "FieldKey"
	formElement  = "Element"
)

// writerForm splits the name of a Builder writer into its kind and its form,
// or returns no kind for a method that is no Field, FieldKey or Element writer.
func writerForm(method string) (kind, form string) {
	for _, suffix := range []string{formFieldKey, formField, formElement} {
		if k, ok := strings.CutSuffix(strings.TrimPrefix(method, "Add"), suffix); ok {
			return k, suffix
		}
	}
	return "", ""
}

// writerParams returns the parameter names the writer of kind in form takes.
func writerParams(kind, form string) []string {
	want := map[string][]string{formField: {"name"}, formFieldKey: {"k"}, formElement: nil}[form]
	switch kind {
	case "Null":
		return want
	case "Value":
		return append(want, "v", "text")
	case "RawBytes":
		return []string{"name", "raw"}
	}
	return append(want, valueParam(kind))
}

func TestBuilderWriterFamilies(t *testing.T) {
	writers, err := builderWriters()
	noError(t, err)
	exceptions := familyExceptions()
	for method, params := range writers {
		kind, form := writerForm(method)
		if kind == "" {
			continue
		}
		if _, exempt := exceptions[kind]; !exempt {
			for _, twin := range []string{formField, formFieldKey, formElement} {
				if _, ok := writers["Add"+kind+twin]; !ok {
					t.Errorf("%s has no twin Add%s%s, want every form of its family", method, kind, twin)
				}
			}
		}
		if want := writerParams(kind, form); !slices.Equal(params, want) {
			t.Errorf("%s takes %q, want %q", method, params, want)
		}
	}
}

func TestBuilderElementWritersMatchEncodingJSON(t *testing.T) {
	text := []byte("a\"<\u2028\xff")
	for _, tc := range []struct {
		v     any
		write func(b *Builder)
	}{
		{"x\n\u2029", func(b *Builder) { b.AddStringElement("x\n\u2029") }},
		{string([]rune(string(text))), func(b *Builder) { b.AddStringBytesElement(text) }},
		{true, func(b *Builder) { b.AddBoolElement(true) }},
		{math.MinInt, func(b *Builder) { b.AddIntElement(math.MinInt) }},
		{int64(math.MinInt64), func(b *Builder) { b.AddInt64Element(math.MinInt64) }},
		{uint64(math.MaxUint64), func(b *Builder) { b.AddUint64Element(math.MaxUint64) }},
		{math.SmallestNonzeroFloat64, func(b *Builder) { b.AddFloat64Element(math.SmallestNonzeroFloat64) }},
		{[]string{}, func(b *Builder) { b.AddStringArrayElement(nil) }},
		{nil, (*Builder).AddNullElement},
	} {
		element, value := New(0), New(0)
		tc.write(element)
		value.AddValueElement(tc.v, nil)
		want := stdJSON(t, tc.v)
		if _, isStrings := tc.v.([]string); isStrings {
			want = emptyArray
		}
		if got := encodingJSONForm(element.Bytes()); got != want {
			t.Errorf("the Element writer of %#v wrote %s, encoding/json %s", tc.v, element.Bytes(), want)
		}
		if _, isStrings := tc.v.([]string); !isStrings && !bytes.Equal(value.Bytes(), element.Bytes()) {
			t.Errorf("AddValueElement(%#v) wrote %s, want %s as its Element writer writes",
				tc.v, value.Bytes(), element.Bytes())
		}
	}
}

func TestBuilderAppendRawAndAppendRawStringCopyAsTheyAre(t *testing.T) {
	b := New(0)
	b.AppendRaw([]byte(`{"k":`))
	b.AppendRawString(`"v\n"}`)
	b.AppendRaw(nil)
	if got, want := string(b.Bytes()), `{"k":"v\n"}`; got != want {
		t.Fatalf("raw appends wrote %s, want %s", got, want)
	}
}

func TestBuilderAllocs(t *testing.T) {
	b := New(0)
	raw := []byte(`{"nested":true}`)
	values := []string{"id-1", "id-2", "id-3"}
	assertAllocs(t, 0, func() {
		b.Reset()
		b.BeginObject()
		b.AddStringField("hostname", "webserver-prod-01")
		b.AddIntField("severity", 4)
		b.AddInt64Field("i", -9007199254740993)
		b.AddUint64Field("u", math.MaxUint64)
		b.AddFloat64Field("f", 2.718281828459045)
		b.AddBoolField("b", true)
		b.AddNullField("n")
		b.AddRawJSONField("r", raw)
		b.AddRawBytesField(raw[2:8], raw)
		b.AddStringBytesField("p", raw)
		b.AddStringArrayField("items", values)
		b.AddRawMembers(raw)
		b.BeginArrayField("list")
		b.AddStringElement("x")
		b.AddStringBytesElement(raw)
		b.AddStringArrayElement(values)
		b.AddInt64Element(-7)
		b.AddIntElement(12)
		b.AddUint64Element(math.MaxUint64)
		b.AddFloat64Element(2.5)
		b.AddBoolElement(true)
		b.AddNullElement()
		b.AddRawJSONElement(raw)
		b.BeginObjectElement()
		b.EndObject()
		b.BeginArrayElement()
		b.EndArray()
		b.EndArray()
		b.BeginObjectField("o")
		b.EndObject()
		b.AddRawBytesField(raw[2:8], nil)
		b.BeginArray()
		b.EndArray()
		b.EndObject()
		b.AppendRaw(raw)
		b.AppendRawString(emptyObject)
		_, _ = b.Len(), b.Bytes()
	})
}

// The texts of the fuzz steps that open a level.
const (
	arrayOpener  = "["
	objectOpener = "{"
)

// fuzzStep is one item the document fuzzer writes, with the text it starts.
type fuzzStep struct {
	field   func(b *Builder, name string)
	element func(b *Builder)
	start   string
}

// builderLevel is one open container of the reference writer.
type builderLevel struct {
	items    int
	isObject bool
}

// write adds the item s to b as a field named name in an object, and as an
// element in an array.
func (l *builderLevel) write(b *Builder, s fuzzStep, name string) {
	if l.isObject {
		s.field(b, name)
	} else {
		s.element(b)
	}
	l.items++
}

func (l *builderLevel) closer() byte {
	if l.isObject {
		return '}'
	}
	return ']'
}

func (l *builderLevel) end(b *Builder) {
	if l.isObject {
		b.EndObject()
		return
	}
	b.EndArray()
}

// escapedValue is the string value of the fuzz steps, one byte of it invalid
// UTF-8.
const escapedValue = "s\n\xff"

// fuzzSteps lists the items that the fuzz byte op selects among, with values
// derived from op.
func fuzzSteps(op byte) []fuzzStep {
	v, f, flag := int64(op)+math.MinInt8, math.Ldexp(float64(op), -2), op&1 == 1
	raw, strs := []byte(`{"r":[1]}`), []string{"a", "b\n"}
	return []fuzzStep{
		{func(b *Builder, n string) { b.AddStringField(n, escapedValue) },
			func(b *Builder) { b.AddStringElement(escapedValue) },
			`"s\n` + "\uFFFD" + `"`},
		{func(b *Builder, n string) { b.AddInt64Field(n, v) },
			func(b *Builder) { b.AddInt64Element(v) },
			strconv.FormatInt(v, decimalRadix)},
		{func(b *Builder, n string) { b.AddIntField(n, int(v)) },
			func(b *Builder) { b.AddIntElement(int(v)) },
			strconv.FormatInt(v, decimalRadix)},
		{func(b *Builder, n string) { b.AddUint64Field(n, uint64(op)) },
			func(b *Builder) { b.AddUint64Element(uint64(op)) },
			strconv.FormatUint(uint64(op), decimalRadix)},
		{func(b *Builder, n string) { b.AddFloat64Field(n, f) },
			func(b *Builder) { b.AddFloat64Element(f) },
			strconv.FormatFloat(f, 'f', -1, float64Bits)},
		{func(b *Builder, n string) { b.AddBoolField(n, flag) },
			func(b *Builder) { b.AddBoolElement(flag) },
			strconv.FormatBool(flag)},
		{(*Builder).AddNullField, (*Builder).AddNullElement, litNull},
		{func(b *Builder, n string) { b.AddRawJSONField(n, raw) },
			func(b *Builder) { b.AddRawJSONElement(raw) },
			string(raw)},
		{func(b *Builder, n string) { b.AddRawBytesField([]byte(n), raw) },
			func(b *Builder) { b.AddRawJSONElement(raw) },
			string(raw)},
		{func(b *Builder, n string) { b.AddRawBytesField([]byte(n), nil); b.AppendRaw(raw) },
			func(b *Builder) { b.AddRawJSONElement(raw) },
			string(raw)},
		{func(b *Builder, n string) { b.AddRawBytesField([]byte(n), nil); b.AppendRawString(litTrue) },
			func(b *Builder) { b.AddBoolElement(true) },
			litTrue},
		{func(b *Builder, n string) { b.AddStringArrayField(n, strs) },
			func(b *Builder) { b.AddStringArrayElement(strs) },
			`["a","b\n"]`},
		{(*Builder).BeginArrayField, (*Builder).BeginArrayElement, arrayOpener},
		{(*Builder).BeginObjectField, (*Builder).BeginObjectElement, objectOpener},
		{func(b *Builder, n string) { b.AddRawBytesField([]byte(n), nil); b.BeginObject() },
			(*Builder).BeginObjectElement, objectOpener},
		{func(b *Builder, n string) { b.AddStringFieldKey(NewFieldKey(n), "z") },
			func(b *Builder) { b.AddStringElement("z") },
			`"z"`},
		{func(b *Builder, n string) { b.BeginArrayFieldKey(NewFieldKey(n)) },
			(*Builder).BeginArrayElement, arrayOpener},
		{func(b *Builder, n string) { b.BeginObjectFieldKey(NewFieldKey(n)) },
			(*Builder).BeginObjectElement, objectOpener},
		{func(b *Builder, n string) { b.AddStringBytesField(n, []byte(escapedValue)) },
			func(b *Builder) { b.AddStringBytesElement([]byte(escapedValue)) },
			`"s\n` + "\uFFFD" + `"`},
		{func(b *Builder, n string) { b.AddRawJSONField(n, nil); b.BeginArray() },
			func(b *Builder) { b.AddRawJSONElement(nil); b.BeginArray() },
			arrayOpener},
		{func(b *Builder, n string) { b.AddRawMembers([]byte(`{"` + n + `":` + string(raw) + "}")) },
			func(b *Builder) { b.AddRawJSONElement(raw) },
			string(raw)},
	}
}

// fuzzSeed returns the ops that write every step once, an opener followed by
// the op that closes its level: as elements, and as members of an object.
func fuzzSeed() []byte {
	steps := fuzzSteps(0)
	var closer, object byte
	for _, s := range steps {
		if s.start == objectOpener {
			object = closer
		}
		closer++
	}
	var (
		elements []byte
		op       byte
	)
	for _, s := range steps {
		elements = append(elements, op)
		if s.start == arrayOpener || s.start == objectOpener {
			elements = append(elements, closer)
		}
		op++
	}
	return append(append(elements, append([]byte{object}, elements...)...), closer)
}

// fuzzDocument writes one document through b and returns what a reference
// writer that counts the items of each level writes; each op byte picks a
// step, or one past the last step, which closes the innermost level.
func fuzzDocument(b *Builder, ops []byte) []byte {
	b.BeginArray()
	ref := []byte{'['}
	stack := []builderLevel{{}}
	for _, op := range ops {
		top := &stack[len(stack)-1]
		steps := fuzzSteps(op)
		if int(op)%(len(steps)+1) == len(steps) {
			if len(stack) > 1 {
				ref = append(ref, top.closer())
				top.end(b)
				stack = stack[:len(stack)-1]
			}
			continue
		}
		step := steps[int(op)%(len(steps)+1)]
		name := "k" + strconv.Itoa(top.items)
		if top.items > 0 {
			ref = append(ref, ',')
		}
		if top.isObject {
			ref = append(ref, `"`+name+`":`...)
		}
		ref = append(ref, step.start...)
		top.write(b, step, name)
		if step.start == arrayOpener || step.start == objectOpener {
			stack = append(stack, builderLevel{isObject: step.start == objectOpener})
		}
	}
	for i := range slices.Backward(stack) {
		ref = append(ref, stack[i].closer())
		stack[i].end(b)
	}
	return ref
}

func FuzzBuilderElements(f *testing.F) {
	// Every step as an element and as a member, and deeper nesting.
	f.Add(fuzzSeed())
	f.Add([]byte{14, 26, 33, 16, 31, 18, 37, 200, 255, 12, 13, 17, 21})
	f.Fuzz(func(t *testing.T, ops []byte) {
		b := New(1)
		b.Grow(len(ops))
		want := fuzzDocument(b, ops)
		if got := b.Bytes(); !bytes.Equal(got, want) || b.Len() != len(want) || !json.Valid(got) {
			t.Fatalf("ops %v: Builder wrote %s (Len %d), want %s", ops, got, b.Len(), want)
		}
		strictlyRead(t, b.Bytes())
		b.Reset()
		fuzzDocument(b, ops)
		if !bytes.Equal(b.Bytes(), want) {
			t.Fatalf("ops %v: Builder wrote %s after Reset, want %s", ops, b.Bytes(), want)
		}
	})
}

func BenchmarkBuilderAddStringField(b *testing.B) {
	benchWrite(b, `{"key1":"value1","key2":"value2","key3":"value3"}`, func(builder *Builder) {
		builder.BeginObject()
		builder.AddStringField("key1", "value1")
		builder.AddStringField("key2", "value2")
		builder.AddStringField("key3", "value3")
		builder.EndObject()
	})
}

func BenchmarkBuilderSyslogObject(b *testing.B) {
	benchWrite(b, syslogRecord(b), func(builder *Builder) {
		builder.BeginObject()
		builder.AddStringField("message", logMessage)
		builder.AddStringField("hostname", logHost)
		builder.AddIntField("severity", logSeverity)
		builder.AddIntField("facility", logFacility)
		builder.AddStringField("app_name", logApp)
		builder.AddStringField("proc_id", logProc)
		builder.AddStringField("msg_id", logMsgID)
		builder.AddIntField("version", logVersion)
		builder.AddStringField("source", logSource)
		builder.EndObject()
	})
}

// arrayObjects is how many objects BenchmarkBuilderArray writes per array.
const arrayObjects = 16

func BenchmarkBuilderArray(b *testing.B) {
	type scored struct {
		ID    int64   `json:"id"`
		Score float64 `json:"score"`
	}
	items := make([]scored, arrayObjects)
	for i := range items {
		items[i] = scored{ID: int64(i), Score: float64(i) / math.Pi}
	}
	want, err := json.Marshal(items)
	noError(b, err)
	benchWrite(b, string(want), func(builder *Builder) {
		builder.BeginArray()
		for i := range int64(arrayObjects) {
			builder.BeginObjectElement()
			builder.AddInt64Field("id", i)
			builder.AddFloat64Field("score", float64(i)/math.Pi)
			builder.EndObject()
		}
		builder.EndArray()
	})
}

func BenchmarkBuilderFields(b *testing.B) {
	values, name, raw, spliced := []string{"x", "y"}, []byte("raw"), []byte(objectA1), []byte(`{"m":0}`)
	want := `{"s":"v","p":"{\"a\":1}","m":0,"a":["x","y"],"i":-7,"i64":-9223372036854775808,` +
		`"u":18446744073709551615,"f":3.141592653589793,"b":true,"n":null,"r":{"a":1},"raw":{"a":1},"o":{},"l":[]}`
	benchWrite(b, want, func(builder *Builder) {
		builder.BeginObject()
		builder.AddStringField("s", "v")
		builder.AddStringBytesField("p", raw)
		builder.AddRawMembers(spliced)
		builder.AddStringArrayField("a", values)
		builder.AddIntField("i", -7)
		builder.AddInt64Field("i64", math.MinInt64)
		builder.AddUint64Field("u", math.MaxUint64)
		builder.AddFloat64Field("f", math.Pi)
		builder.AddBoolField("b", true)
		builder.AddNullField("n")
		builder.AddRawJSONField("r", raw)
		builder.AddRawBytesField(name, raw)
		builder.BeginObjectField("o")
		builder.EndObject()
		builder.BeginArrayField("l")
		builder.EndArray()
		builder.EndObject()
	})
}

func BenchmarkBuilderElements(b *testing.B) {
	raw, values := []byte(objectA1), []string{"x", "y"}
	want := `["s","{\"a\":1}",["x","y"],-7,-9223372036854775808,18446744073709551615,3.141592653589793,` +
		`true,null,{"a":1},{},[]]`
	benchWrite(b, want, func(builder *Builder) {
		builder.BeginArray()
		builder.AddStringElement("s")
		builder.AddStringBytesElement(raw)
		builder.AddStringArrayElement(values)
		builder.AddIntElement(-7)
		builder.AddInt64Element(math.MinInt64)
		builder.AddUint64Element(math.MaxUint64)
		builder.AddFloat64Element(math.Pi)
		builder.AddBoolElement(true)
		builder.AddNullElement()
		builder.AddRawJSONElement(raw)
		builder.BeginObjectElement()
		builder.EndObject()
		builder.BeginArrayElement()
		builder.EndArray()
		builder.EndArray()
	})
}

func BenchmarkBuilderAppendRaw(b *testing.B) {
	const head, tail = `{"r":`, "}"
	raw := []byte(objectA1)
	benchWrite(b, head+objectA1+tail, func(builder *Builder) {
		builder.Grow(len(head) + len(raw) + len(tail))
		builder.AppendRawString(head)
		builder.AppendRaw(raw)
		builder.AppendRawString(tail)
	})
}

func ExampleBuilder() {
	var b Builder
	b.BeginObject()
	b.AddStringField("message", "Hello, World!")
	b.AddIntField("severity", 2)
	b.BeginArrayField("tags")
	b.AddStringElement("auth")
	b.BeginObjectElement()
	b.AddBoolField("ok", true)
	b.EndObject()
	b.EndArray()
	b.EndObject()
	fmt.Println(string(b.Bytes()))
	// Output: {"message":"Hello, World!","severity":2,"tags":["auth",{"ok":true}]}
}

func ExampleNew() {
	b := New(0)
	b.BeginArray()
	b.AddFloat64Element(1.0 / 2)
	b.AddFloat64Element(math.MaxFloat64)
	b.AddNullElement()
	b.EndArray()
	fmt.Println(string(b.Bytes()))
	// Output: [0.5,1.7976931348623157e+308,null]
}
