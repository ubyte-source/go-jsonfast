package jsonfast

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"runtime/debug"
	"strings"
	"testing"
)

// stdDecode decodes data with stdValue.
func stdDecode(tb testing.TB, data []byte) (v any, repeated bool) {
	tb.Helper()
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	v, repeated, err := stdValue(dec)
	noError(tb, err)
	return v, repeated
}

// decodeBoth decodes data held in a string and in a byte slice, and requires
// both to agree.
func decodeBoth(tb testing.TB, data string, maxDepth int) (any, error) {
	tb.Helper()
	v, err := DecodeValue(data, maxDepth, numberOf[string])
	vb, errb := DecodeValue([]byte(data), maxDepth, numberOf[[]byte])
	if !reflect.DeepEqual(v, vb) || !is(err, errb) {
		tb.Fatalf("DecodeValue(%q) = %#v, %v from a string, want %#v, %v as from bytes", data, v, err, vb, errb)
	}
	return v, err
}

func TestDecodeValueDecodesAsEncodingJSON(t *testing.T) {
	for _, data := range []string{
		litNull, litTrue, litFalse, "0", "-0", "1.50", "-2e-3", "1e400", "123456789012345678901234567890",
		quotedHello, `"a\nb\u00e9\ud83d\ude00"`, `"` + cafe + `"`, emptyArray, emptyObject, twoArray,
		" \t\r\n[ 1 , \"x\" , null , true , false , { } , [ ] ] \n",
		`{"a":1,"b":{"c":[1,{"d":"e"}],"f":null},"g":[[],[{}]]}`,
		`{ "k" : "v" , "caf\u00e9" : -1.5e+3 , "" : "" }`,
		`[{"a":1},{"a":2},{"b":{"a":3}}]`, `{"a":{"a":{"a":1}}}`,
	} {
		want, _ := stdDecode(t, []byte(data))
		got, err := decodeBoth(t, data, MaxDepth)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("DecodeValue(%q) = %#v, %v; want %#v", data, got, err, want)
		}
	}
}

func TestDecodeValueRefusesMalformed(t *testing.T) {
	for _, data := range []string{
		"", blank, "{", "[1", `{"a":1}x`, "[1]]", objectTrail, arrayTrail, arrayGap, `{"a"}`,
		"\"\xfe\"", loneHigh, `{"` + "\xc3" + `":1}`, "+1", "01", `"\x"`, "nul",
		repeatedA + " x", `[{"a":1,"a":2},]`, `{"a":1,"a":2,"b":"\ud800"}`, `{"a":1,"a":[2}`,
		"[\"]\t", "{\"a\":\"}\t",
	} {
		if v, err := decodeBoth(t, data, MaxDepth); v != nil || !is(err, ErrMalformed) {
			t.Errorf("DecodeValue(%q) = %#v, %v; want nil, ErrMalformed", data, v, err)
		}
	}
}

func TestDecodeValueHonorsTheDepthBound(t *testing.T) {
	const depth = 3
	if _, err := decodeBoth(t, arrays(depth), depth); err != nil {
		t.Fatalf("DecodeValue at the bound = %v, want nil", err)
	}
	if v, err := decodeBoth(t, arrays(depth+1), depth); v != nil || !is(err, ErrMalformed) {
		t.Fatalf("DecodeValue past the bound = %#v, %v; want nil, ErrMalformed", v, err)
	}
}

func TestDecodeValueAnswersANegativeBoundAtOnce(t *testing.T) {
	refuse := func(doc string) func() {
		return func() {
			if v, err := DecodeValue(doc, -1, numberOf[string]); v != nil || !is(err, ErrMalformed) {
				t.Fatalf("DecodeValue(%.12q, -1) = %#v, %v; want nil, ErrMalformed", doc, v, err)
			}
		}
	}
	assertCost(t, refuse("1"), refuse(strings.Repeat(" ", decidedSpaces)+"1"), 1)
}

func TestDecodeValueRefusesRepeatedNames(t *testing.T) {
	for _, data := range []string{
		`{"k":1,"k":2}`, `{"z":1,"\u007a":2}`, `{"a":1,"b":2,"a":1}`,
		`[1,{"x":{"b":1,"b":2}}]`, `{"k":[{"b":[],"b":{}}]}`, `[{"x":{"b":1,"b":2}}]`,
	} {
		if _, repeated := stdDecode(t, []byte(data)); !repeated {
			t.Fatalf("encoding/json finds no repeated name in the fixture %q, want one", data)
		}
		if v, err := decodeBoth(t, data, MaxDepth); v != nil || !is(err, ErrDuplicateName) {
			t.Errorf("DecodeValue(%q) = %#v, %v; want nil, ErrDuplicateName", data, v, err)
		}
	}
}

func TestDecodeValueAliasesStringInput(t *testing.T) {
	data := `{"plain":"text","escaped":"a\tb"}`
	v, err := DecodeValue(data, MaxDepth, numberOf[string])
	noError(t, err)
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("DecodeValue(%q) = %#v, want a map", data, v)
	}
	if plain, ok := m["plain"].(string); !ok || !aliases(plain, data, len(`{"plain":"`)) {
		t.Errorf("value %#v is a copy, want a view of the input", m["plain"])
	}
	if escaped, ok := m["escaped"].(string); !ok || escaped != "a\tb" {
		t.Errorf("escaped value = %#v, want %q", m["escaped"], "a\tb")
	}
}

func TestDecodeValueCopiesByteInput(t *testing.T) {
	data := []byte(`["plain",{"name":"a\tb"}]`)
	v, err := DecodeValue(data, MaxDepth, numberOf[[]byte])
	noError(t, err)
	for i := range data {
		data[i] = 'x'
	}
	if want := []any{"plain", map[string]any{"name": "a\tb"}}; !reflect.DeepEqual(v, want) {
		t.Fatalf("DecodeValue = %#v after its input changed, want %#v", v, want)
	}
}

func TestValueDecoderTextCopiesByteInputIntoAChunkOfTheRest(t *testing.T) {
	pad := strings.Repeat(" ", documentedRoom)
	for _, tc := range []struct {
		doc  string
		want int
	}{
		{`["plain","cd"]`, len(`"plain","cd"]`)},
		{`["plain",` + pad + `"cd"]`, documentedRoom},
		{`["pl\u0061in","cd"]`, len(`"pl\u0061in","cd"]`)},
		{`["pl\u0061in",` + pad + `"cd"]`, documentedRoom},
	} {
		data := []byte(tc.doc)
		vd := valueDecoder[[]byte]{number: numberOf[[]byte], data: data}
		w := newWalker(data, MaxDepth)
		w.i = len(`["`)
		s, ok := vd.text(w)
		end := strings.IndexByte(tc.doc, ',')
		if s != plainText || !ok || w.i != end || cap(vd.arena.buf) != tc.want {
			t.Errorf("text of %.20q = %q, %v, past %d in a chunk of %d, want %q, true, past %d in one of %d",
				tc.doc, s, ok, w.i, cap(vd.arena.buf), plainText, end, tc.want)
		}
	}
}

func TestDecodeValueKeepsTheGoroutineStackFlat(t *testing.T) {
	const depth = 1 << 18
	defer debug.SetMaxStack(debug.SetMaxStack(flatStack))
	v, err := DecodeValue(arrays(depth), math.MaxInt, numberOf[string])
	noError(t, err)
	for range depth - 1 {
		a, ok := v.([]any)
		if !ok || len(a) != 1 {
			t.Fatalf("a level of the document decoded to %#v, want one array", v)
		}
		v = a[0]
	}
	if a, ok := v.([]any); !ok || len(a) != 0 {
		t.Fatalf("the innermost level decoded to %#v, want an empty array", v)
	}
}

func TestDecodeValueHandsEachNumberItsRawText(t *testing.T) {
	var seen []string
	number := func(raw string) any {
		seen = append(seen, raw)
		return "#" + raw
	}
	v, err := DecodeValue(`{"n": [ -0 , 1.50e+2,7]}`, MaxDepth, number)
	noError(t, err)
	want := map[string]any{"n": []any{"#-0", "#1.50e+2", "#7"}}
	if !reflect.DeepEqual(v, want) || fmt.Sprint(seen) != "[-0 1.50e+2 7]" {
		t.Fatalf("DecodeValue = %#v with raw texts %q, want %#v with [-0 1.50e+2 7]", v, seen, want)
	}
}

func FuzzDecodeValue(f *testing.F) {
	for _, s := range []string{
		`{"a":[1,{"b":null}],"c":"\u00e9"}`, `{"y":1,"\u0079":2}`, `[{"x":1},{"x":2}]`,
		"\"\xfe\"", loneHigh, arrays(seedNesting), " 1e400 ",
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(checkDecodeValue)
}

// checkDecodeValue holds DecodeValue to encoding/json on data: malformed
// unless ValidUTF8, a repeated name refused, else the same values.
func checkDecodeValue(t *testing.T, data []byte) {
	got, err := DecodeValue(data, MaxDepth, numberOf[[]byte])
	if !jsonUTF8(data) {
		if got != nil || !is(err, ErrMalformed) {
			t.Fatalf("DecodeValue(%q) = %#v, %v; want nil, ErrMalformed", data, got, err)
		}
		return
	}
	want, repeated := stdDecode(t, data)
	if repeated && (got != nil || !is(err, ErrDuplicateName)) {
		t.Fatalf("DecodeValue(%q) = %#v, %v; want nil, ErrDuplicateName", data, got, err)
	}
	if !repeated && (err != nil || !reflect.DeepEqual(got, want)) {
		t.Fatalf("DecodeValue(%q) = %#v, %v; want %#v", data, got, err, want)
	}
}

// Allocations of DecodeValue: valueDocument as a string and as a byte slice,
// 16 and 17 nested arrays, and the array of the escapedTokens.
const (
	documentAllocs      = 13
	documentBytesAllocs = 14
	inlineLevelsAllocs  = 31
	spilledLevelsAllocs = 34
	escapedAllocs       = 40
)

func TestDecodeValueAllocs(t *testing.T) {
	raw := []byte(valueDocument)
	fits, spills, escaped := arrays(documentedOpenValues), arrays(documentedOpenValues+1), string(escapedArray())
	// Containers and boxed values allocate, byte input copies its strings, and a
	// level past the inline ones and decoded strings take stack and arena chunks.
	for _, tc := range []struct {
		decode func() (any, error)
		allocs float64
	}{
		{func() (any, error) { return DecodeValue(valueDocument, MaxDepth, numberOf[string]) }, documentAllocs},
		{func() (any, error) { return DecodeValue(raw, MaxDepth, numberOf[[]byte]) }, documentBytesAllocs},
		{func() (any, error) { return DecodeValue(fits, MaxDepth, numberOf[string]) }, inlineLevelsAllocs},
		{func() (any, error) { return DecodeValue(spills, MaxDepth, numberOf[string]) }, spilledLevelsAllocs},
		{func() (any, error) { return DecodeValue(escaped, MaxDepth, numberOf[string]) }, escapedAllocs},
	} {
		assertAllocs(t, tc.allocs, func() {
			_, err := tc.decode()
			noError(t, err)
		})
	}
	refused := "[" + refusedLate() + "]"
	assertAllocs(t, 0, func() {
		if v, err := DecodeValue(refused, MaxDepth, numberOf[string]); v != nil || !is(err, ErrMalformed) {
			t.Fatalf("DecodeValue(%.12q) = %#v, %v, want nil, ErrMalformed", refused, v, err)
		}
	})
}

func BenchmarkDecodeValue(b *testing.B) {
	data, raw := valueDocument, []byte(valueDocument)
	want, _ := stdDecode(b, raw)
	fromString, stringErr := DecodeValue(data, MaxDepth, numberOf[string])
	fromBytes, bytesErr := DecodeValue(raw, MaxDepth, numberOf[[]byte])
	same := reflect.DeepEqual(fromString, want) && reflect.DeepEqual(fromBytes, want)
	if stringErr != nil || bytesErr != nil || !same {
		b.Fatalf("DecodeValue(%s) = %#v, %v, and %#v, %v from bytes; want %#v", data, fromString, stringErr,
			fromBytes, bytesErr, want)
	}
	b.Run("String", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(data)))
		for b.Loop() {
			if _, err := DecodeValue(data, MaxDepth, numberOf[string]); err != nil {
				b.Fatalf("DecodeValue = %v, want nil", err)
			}
		}
	})
	b.Run("Bytes", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(raw)))
		for b.Loop() {
			if _, err := DecodeValue(raw, MaxDepth, numberOf[[]byte]); err != nil {
				b.Fatalf("DecodeValue = %v, want nil", err)
			}
		}
	})
}

func ExampleDecodeValue() {
	v, err := DecodeValue(`{"id":7,"tags":["a","b"]}`, MaxDepth, func(raw string) any { return json.Number(raw) })
	fmt.Println(v, err)
	// Output: map[id:7 tags:[a b]] <nil>
}

func TestDecodeValueSeesTheNumbersBeforeAFault(t *testing.T) {
	for _, tc := range []struct {
		doc, seen string
		want      error
	}{
		{"[1,2,\"\xff\"]", "1 2", ErrMalformed},
		{`[1,2,3] x`, "1 2 3", ErrMalformed},
		{`[1,2,3`, "1 2 3", ErrMalformed},
		{`{"a":1,"a":2}`, "1", ErrDuplicateName},
		{`{"a":1,"a":2,"b":3} x`, "1", ErrMalformed},
	} {
		var seen []string
		_, err := DecodeValue(tc.doc, MaxDepth, func(raw string) any {
			seen = append(seen, raw)
			return json.Number(raw)
		})
		if got := strings.Join(seen, " "); !is(err, tc.want) || got != tc.seen {
			t.Errorf("DecodeValue(%q) = %v after numbers %q, want %v after %q "+
				"(godoc: number may see numbers before a fault)", tc.doc, err, got, tc.want, tc.seen)
		}
	}
}

// repeatAllocs is what DecodeValue allocates on repeatedName closed at once; the
// members past the repeat of the long document add nothing.
const repeatAllocs = 3

func TestDecodeValueStopsAtARepeatedName(t *testing.T) {
	short, long := repeatedName+repeatedTail(0), repeatedName+repeatedTail(tailMembers)
	for _, doc := range []string{short, long} {
		calls := 0
		_, err := DecodeValue(doc, MaxDepth, func(raw string) any {
			calls++
			return json.Number(raw)
		})
		if !is(err, ErrDuplicateName) || calls != 1 {
			t.Fatalf("DecodeValue of %d bytes = %v after %d number calls, want %v after 1, none past the repeat",
				len(doc), err, calls, ErrDuplicateName)
		}
		assertAllocs(t, repeatAllocs, func() {
			if _, err := DecodeValue(doc, MaxDepth, numberOf[string]); !is(err, ErrDuplicateName) {
				t.Fatalf("DecodeValue of %d bytes = %v, want %v", len(doc), err, ErrDuplicateName)
			}
		})
	}
}

func TestDecodeValueCostIsLinear(t *testing.T) {
	decode := func(doc []byte) func() {
		return func() {
			if _, err := DecodeValue(doc, MaxDepth, numberOf[[]byte]); err != nil {
				t.Fatalf("DecodeValue of %d bytes = %v, want nil", len(doc), err)
			}
		}
	}
	assertCost(t, decode(levelsDocument(costLevels)), decode(levelsDocument(costScale*costLevels)), costScale)
}
