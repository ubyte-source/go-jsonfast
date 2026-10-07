package jsonfast

import (
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"reflect"
	"runtime/debug"
	"slices"
	"strings"
	"testing"
)

// Floats that the value writers write in exponent notation, above and below the plain
// range, and one inside it.
const (
	wideFloat    = maxPlainFloat
	narrowFloat  = minPlainFloat / decimalBase
	negativeHalf = -0.5
)

// Keys of the cycle and sharing fixtures.
const (
	keyA = "a"
	keyB = "b"
)

// nullElement is an array whose one element the value writers write as null.
const nullElement = "[null]"

// numberText reports the text of a json.Number, the number type of the tests.
func numberText(v any) (string, bool) {
	n, ok := v.(json.Number)
	return string(n), ok
}

func TestBuilderAddValueElementWritesEachKind(t *testing.T) {
	for _, tc := range []struct {
		v    any
		want string
	}{
		{nil, litNull}, {"a\"<\n", `"a\"<\n"`}, {json.Number("-1.50e+3"), "-1.50e+3"}, {json.Number("01"), `"01"`},
		{json.Number(""), quotedEmpty}, {true, litTrue}, {false, litFalse},
		{int64(math.MinInt64), "-9223372036854775808"}, {wideFloat, "1e+21"}, {narrowFloat, "1e-7"},
		{negativeHalf, "-0.5"}, {math.NaN(), litNull}, {-1, "-1"}, {uint64(math.MaxUint64), "18446744073709551615"},
		{float32(1), litNull}, {map[string]any(nil), litNull}, {[]any(nil), litNull},
		{map[string]any{}, emptyObject}, {[]any{}, emptyArray},
		{map[string]any{"\xfe": true, "\xff": false}, `{"` + replacement + `":false}`},
		{map[string]any{"b": []any{true, nil, map[string]any{"z": "x", "a": json.Number("2")}}, "a": map[string]any{}},
			`{"a":{},"b":[true,null,{"a":2,"z":"x"}]}`},
	} {
		b := New(0)
		b.AddValueElement(tc.v, numberText)
		if got := string(b.Bytes()); got != tc.want {
			t.Errorf("AddValueElement(%#v) wrote %s, want %s", tc.v, got, tc.want)
		}
	}
}

func TestBuilderAddValueElementWritesNullForUnreportedValues(t *testing.T) {
	calls := 0
	count := func(v any) (string, bool) {
		calls++
		return numberText(v)
	}
	for _, v := range []any{nil, int32(1), json.Number("7")} {
		b := New(0)
		b.AddValueElement([]any{v}, nil)
		if got := string(b.Bytes()); got != nullElement {
			t.Errorf("AddValueElement(%#v) with no text function wrote %s, want %s", v, got, nullElement)
		}
	}
	b := New(0)
	b.AddValueElement([]any{nil, "s", true, int64(1), 1, uint64(1), 1.5, []any{}, map[string]any{}, int32(1)}, count)
	if got, want := string(b.Bytes()), `[null,"s",true,1,1,1,1.5,[],{},null]`; got != want || calls != 1 {
		t.Fatalf("AddValueElement wrote %s calling text %d times, want %s calling it once", got, calls, want)
	}
}

func TestBuilderAddValueElementPutsItsComma(t *testing.T) {
	b := New(0)
	b.BeginArray()
	b.AddValueElement(int64(1), nil)
	b.AddValueElement(json.Number("2"), numberText)
	b.AddInt64Element(1)
	b.AddValueElement([]any{}, nil)
	b.AddValueElement(nil, nil)
	b.BeginObjectElement()
	b.AddValueField("v", []any{"x"}, numberText)
	b.AddBoolField("w", true)
	b.AddValueField("a\"", map[string]any{}, nil)
	b.EndObject()
	b.EndArray()
	if got, want := string(b.Bytes()), `[1,2,1,[],null,{"v":["x"],"w":true,"a\"":{}}]`; got != want {
		t.Fatalf("the value writers among other writers wrote %s, want %s", got, want)
	}
}

func TestBuilderAddValueElementWritesNullPastMaxDepth(t *testing.T) {
	deepest := any(map[string]any{})
	for range MaxDepth - 1 {
		deepest = []any{deepest}
	}
	chain := any(nil)
	for range MaxDepth + 1 {
		chain = map[string]any{plainText: chain}
	}
	whole := inArrays(emptyObject, MaxDepth-1)
	cut := inArrays(litNull, MaxDepth)
	nested := nest(`{"`+plainText+`":`, litNull, "}", MaxDepth)
	for _, tc := range []struct {
		v    any
		want string
	}{{deepest, whole}, {[]any{deepest}, cut}, {chain, nested}} {
		b := New(0)
		b.AddValueElement(tc.v, numberText)
		if got := string(b.Bytes()); got != tc.want {
			t.Errorf("AddValueElement wrote %d bytes, want the %d of the reference, from byte %d on",
				len(got), len(tc.want), firstDifference(got, tc.want))
		}
	}
}

// Nesting of the cycle fixtures: a cycle past the frames appendValue keeps inline.
const deepCycle = documentedOpenValues + 4

// cycle is a value that holds itself and the text the value writers write for it.
type cycle struct {
	v    any
	want string
}

// cycles returns values that hold themselves: first those that hold themselves
// once, which a missed cycle writes to MaxDepth, then those that hold themselves
// twice, which a missed cycle never finishes.
func cycles() []cycle {
	slice := []any{nil}
	slice[0] = slice
	loop := map[string]any{}
	loop[plainText] = loop
	inner := []any{nil}
	outer := map[string]any{keyA: inner}
	inner[0] = outer
	ring := map[string]any{}
	at := ring
	for range deepCycle - 1 {
		next := map[string]any{}
		at[plainText], at = next, next
	}
	at[plainText] = ring
	fork := []any{nil, nil}
	fork[0], fork[1] = fork, fork
	twin := map[string]any{}
	twin[keyA], twin[keyB] = twin, twin
	deep := map[string]any{}
	at = deep
	for range deepCycle - 1 {
		next := map[string]any{}
		at[plainText], at = []any{next, deep}, next
	}
	at[plainText] = deep
	link := nest(`{"`+plainText+`":[`, "", "", deepCycle-1)
	return []cycle{
		{slice, nullElement}, {loop, `{"plain":null}`}, {outer, `{"a":[null]}`},
		{ring, nest(`{"`+plainText+`":`, litNull, "}", deepCycle)},
		{fork, "[null,null]"}, {twin, `{"a":null,"b":null}`},
		{deep, link + `{"plain":null}` + strings.Repeat(",null]}", deepCycle-1)},
	}
}

func TestBuilderAddValueElementWritesNullForCycles(t *testing.T) {
	for _, c := range cycles() {
		b := New(0)
		b.AddValueElement(c.v, numberText)
		if got := string(b.Bytes()); got != c.want {
			t.Fatalf("AddValueElement of a cycle wrote %.200s, want %s", got, c.want)
		}
	}
}

func TestBuilderAddValueElementWritesSharedContainersEachTime(t *testing.T) {
	shared := []any{int64(1)}
	twice := []any{shared, map[string]any{keyA: shared, keyB: shared}, shared}
	deep := any(twice)
	for range deepCycle {
		deep = []any{deep}
	}
	wantTwice := `[[1],{"a":[1],"b":[1]},[1]]`
	for _, tc := range []struct {
		v    any
		want string
	}{{twice, wantTwice}, {deep, inArrays(wantTwice, deepCycle)}} {
		b := New(0)
		b.AddValueElement(tc.v, numberText)
		if got := string(b.Bytes()); got != tc.want {
			t.Errorf("AddValueElement of a shared container wrote %s, want %s", got, tc.want)
		}
	}
}

func TestBuilderAddValueElementKeepsTheGoroutineStackFlat(t *testing.T) {
	arrayChain, objectChain := any(nil), any(nil)
	for range MaxDepth {
		arrayChain, objectChain = []any{arrayChain}, map[string]any{plainText: objectChain}
	}
	defer debug.SetMaxStack(debug.SetMaxStack(flatStack))
	b := New(0)
	b.AddValueElement([]any{arrayChain, objectChain}, numberText)
	want := inArrays(inArrays(litNull, MaxDepth-1)+","+nest(`{"`+plainText+`":`, litNull, "}", MaxDepth-1), 1)
	if got := string(b.Bytes()); got != want {
		t.Fatalf("AddValueElement wrote %d bytes, want the %d of the reference, from byte %d on",
			len(got), len(want), firstDifference(got, want))
	}
}

// firstDifference returns the index of the first byte at which a and b differ, or
// the length of the shorter when it is a prefix of the other.
func firstDifference(a, b string) int {
	n := min(len(a), len(b))
	for i := range n {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

// Sizes of TestValueEncoderBucketSpreadsContainers: as many containers as
// buckets, and the fewest buckets they may take, a quarter of them.
const (
	spreadBuckets = 64
	fewestBuckets = spreadBuckets / 4
)

func TestValueEncoderBucketSpreadsContainers(t *testing.T) {
	e := valueEncoder{buckets: make([]int, spreadBuckets)}
	used, kept := map[uint64]bool{}, make([]any, spreadBuckets)
	for i := range kept {
		kept[i] = []any{nil}
		used[e.bucket(containerID{ptr: reflect.ValueOf(kept[i]).Pointer(), length: 1})] = true
	}
	if len(used) < fewestBuckets {
		t.Fatalf("%d containers took %d buckets, want at least %d", spreadBuckets, len(used), fewestBuckets)
	}
}

// chainedFrames is how many open frames share the one bucket of
// TestValueEncoderOnPathWalksTheWholeBucketChain.
const chainedFrames = 3

func TestValueEncoderOnPathWalksTheWholeBucketChain(t *testing.T) {
	e := valueEncoder{buckets: make([]int, 1)}
	for ptr := range uintptr(chainedFrames) {
		e.open = append(e.open, encodeFrame{id: containerID{ptr: ptr + 1}})
		e.file(len(e.open) - 1)
	}
	for ptr := range uintptr(chainedFrames + 1) {
		id := containerID{ptr: ptr + 1}
		if got, filed := e.onPath(id), ptr < chainedFrames; got != filed {
			t.Errorf("onPath(%+v) with %d frames in one bucket = %t, want %t", id, chainedFrames, got, filed)
		}
	}
}

// Keys and buckets of the value writers as their documentation states: the keys
// of open objects they sort inline, and the buckets per frame past the inline frames.
const (
	documentedValueKeys       = 32
	documentedBucketsPerFrame = 2
)

// filledBuckets is the most frames that the buckets of the first refile, past the
// inline frames, hold at documentedBucketsPerFrame buckets per frame.
const filledBuckets = documentedBucketsPerFrame * (documentedOpenValues + 1)

// filledAllocs is how many times appendValue allocates for filledBuckets frames:
// once for the frames past the inline ones and once for their buckets.
const filledAllocs = 2

func TestBuilderAddValueElementRefilesItsBucketsBelowTwoPerFrame(t *testing.T) {
	var full any
	for range filledBuckets {
		full = []any{full}
	}
	past := []any{full}
	b := New(0)
	// One frame more than the buckets hold refiles them once.
	for _, tc := range []struct {
		v      any
		allocs float64
	}{{full, filledAllocs}, {past, filledAllocs + 1}} {
		assertAllocs(t, tc.allocs, func() {
			b.Reset()
			b.AddValueElement(tc.v, nil)
		})
	}
}

func TestBuilderAddValueElementAllocs(t *testing.T) {
	v, err := DecodeValue(`{"id":"abc","page":2,"tags":["a",true,null],"filter":{"site":"x"}}`, MaxDepth,
		numberOf[string])
	noError(t, err)
	full, wide := keyedMap(documentedValueKeys), keyedMap(documentedValueKeys+1)
	fits := any(map[string]any{})
	for range documentedOpenValues - 1 {
		fits = []any{fits}
	}

	b := New(0)
	// Keys past the inline ones spill once; frames past theirs spill once and take
	// their buckets.
	for _, tc := range []struct {
		v      any
		allocs float64
	}{{v, 0}, {fits, 0}, {full, 0}, {wide, 1}, {[]any{fits}, 2}} {
		assertAllocs(t, tc.allocs, func() {
			b.Reset()
			b.AddValueElement(tc.v, numberText)
		})
		assertAllocs(t, tc.allocs, func() {
			b.Reset()
			b.AddValueField(plainText, tc.v, numberText)
		})
	}
}

// typedNumber is the number conversion of FuzzBuilderAddValueElement: an int or a
// uint64 where the text is an integer that fits one, else a json.Number.
func typedNumber(raw []byte) any {
	if n, ok := DecodeInt64(raw); ok && int64(int(n)) == n {
		return int(n)
	}
	if n, ok := DecodeUint64(raw); ok {
		return n
	}
	return json.Number(raw)
}

func FuzzBuilderAddValueElement(f *testing.F) {
	for _, s := range []string{
		`{"b":[1,{"a":null}],"a":"\u2028<&>","":-0.0e-5}`, `[1.5e3,-0,"\ud83d\ude00",true,false]`,
		"\"\xfe\"", arrays(seedNesting), `{"x":{"y":{"z":[]}}}`, `[-1,18446744073709551615,1e2]`,
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		v, err := DecodeValue(data, MaxDepth, typedNumber)
		if err != nil {
			return
		}
		b := New(0)
		b.AddValueElement(v, numberText)
		again, err := DecodeValue(b.Bytes(), MaxDepth, typedNumber)
		if err != nil || !reflect.DeepEqual(again, v) {
			t.Fatalf("AddValueElement(%q) wrote %s, which decodes to %#v, %v, want %#v, nil",
				data, b.Bytes(), again, err, v)
		}
		field := New(0)
		field.BeginObject()
		field.AddValueField(plainText, v, numberText)
		field.EndObject()
		if got, want := string(field.Bytes()), `{"`+plainText+`":`+string(b.Bytes())+"}"; got != want {
			t.Fatalf("AddValueField(%q) wrote %s, want %s", data, got, want)
		}
		if got, want := encodingJSONForm(b.Bytes()), stdJSON(t, v); got != want {
			t.Fatalf("AddValueElement(%q) wrote %s, encoding/json %s", data, b.Bytes(), want)
		}
		strictlyRead(t, b.Bytes())
		strictlyRead(t, field.Bytes())
	})
}

func BenchmarkBuilderAddValueElement(b *testing.B) {
	v, err := DecodeValue(valueDocument, MaxDepth, numberOf[string])
	noError(b, err)
	want, err := json.Marshal(v)
	noError(b, err)
	benchWrite(b, string(want), func(builder *Builder) { builder.AddValueElement(v, numberText) })
}

func BenchmarkBuilderAddValueField(b *testing.B) {
	v, err := DecodeValue(valueDocument, MaxDepth, numberOf[string])
	noError(b, err)
	want, err := json.Marshal(map[string]any{plainText: v})
	noError(b, err)
	benchWrite(b, string(want), func(builder *Builder) {
		builder.BeginObject()
		builder.AddValueField(plainText, v, numberText)
		builder.EndObject()
	})
}

// keyedMap returns a map of n keys k00, k01, ... to their index.
func keyedMap(n int) map[string]any {
	m := make(map[string]any, n)
	for i := range n {
		m[fmt.Sprintf("k%02d", i)] = i
	}
	return m
}

// manyKeys is a key count well past the keys appendValue sorts on the stack.
const manyKeys = 3 * inlineValueKeys

func TestAppendSortedKeysSortsOnTheStackUpToItsSize(t *testing.T) {
	for _, n := range []int{0, 1, inlineValueKeys, inlineValueKeys + 1, manyKeys} {
		m := keyedMap(n)
		var stack [inlineValueKeys]string
		keys := appendSortedKeys(stack[:0], m)
		if !slices.IsSorted(keys) || len(keys) != n || !slices.Equal(keys, slices.Sorted(maps.Keys(m))) {
			t.Errorf("appendSortedKeys of %d keys = %q, want %q", n, keys, slices.Sorted(maps.Keys(m)))
		}
		onStack := n > 0 && &keys[0] == &stack[0]
		if onStack != (n <= inlineValueKeys) && n > 0 {
			t.Errorf("appendSortedKeys of %d keys used the stack: %v, want %v", n, onStack, n <= inlineValueKeys)
		}
	}
}

func TestAppendSortedKeysKeepsOneKeyPerWrittenName(t *testing.T) {
	const (
		head    = "z"
		low     = "a"
		highest = "\xff"
	)
	for _, tc := range []struct {
		m    map[string]any
		want []string
	}{
		{map[string]any{"b": true, low: true}, []string{head, low, "b"}},
		{
			map[string]any{highest: true, "\xfe": true, "\x80": true, replacement: true, low: true},
			[]string{head, low, highest},
		},
		{
			map[string]any{"\xef\xbf\xbe": true, highest: true, "y\xff": true},
			[]string{head, "y\xff", highest, "\xef\xbf\xbe"},
		},
	} {
		if got := appendSortedKeys([]string{head}, tc.m); !slices.Equal(got, tc.want) {
			t.Errorf("appendSortedKeys(%v) = %q, want %q", tc.m, got, tc.want)
		}
	}
}

func TestCompareWrittenReadsRejectedBytesAsReplacement(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"a\xff", "a" + replacement, 0}, {"\x80", "\xff", 0}, {"\xff", "\xef\xbf\xbe", -1}, {"", keyA, -1},
		{keyA, "a\xff", -1}, {"é", "e", 1}, {"a~", "a\x7f", -1}, {plainText, plainText, 0},
		{"\xc3\xa9", "\xc3A", -1}, {"\xe2\x82\xac", "\xe2\x82\xad", -1}, {"x\xff\xfey", "x" + replacement + "\x80y", 0},
		{"\xe2\x82\xac", "\xe2\x82", -1}, {"\xe2\x82", "\xef\xbf\xbd\xef\xbf\xbd", 0}, {"az", "za", -1},
	} {
		if got := compareWritten(tc.a, tc.b); got != tc.want {
			t.Errorf("compareWritten(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
		if got := compareWritten(tc.b, tc.a); got != -tc.want {
			t.Errorf("compareWritten(%q, %q) = %d, want %d", tc.b, tc.a, got, -tc.want)
		}
	}
}

func FuzzCompareWritten(f *testing.F) {
	f.Add("a\xff", "a"+replacement)
	f.Add("\xc3\xa9", "\xc3A")
	f.Add("\xe2\x82\xac", "\xe2\x82")
	f.Fuzz(func(t *testing.T, a, b string) {
		want := strings.Compare(written(t, a), written(t, b))
		if got := compareWritten(a, b); got != want {
			t.Fatalf("compareWritten(%q, %q) = %d, encoding/json orders them %d", a, b, got, want)
		}
	})
}

// written returns s as encoding/json writes and reads it back.
func written(t *testing.T, s string) string {
	t.Helper()
	out, err := marshalDecoded(s)
	if err != nil {
		t.Fatalf("encoding/json round trip of %q = %v, want nil", s, err)
	}
	return out
}

// ratioFrames is how many frames TestValueEncoderIndexKeepsTwoToFourBucketsPerFrame
// opens, past several refiles.
const ratioFrames = 200

// refiledBucketsPerFrame is how many buckets per frame a refile makes, twice the
// documented least, so a refile comes once each time the frames double.
const refiledBucketsPerFrame = 2 * documentedBucketsPerFrame

func TestValueEncoderIndexKeepsTwoToFourBucketsPerFrame(t *testing.T) {
	var e valueEncoder
	kept := make([][]any, ratioFrames)
	for k := range kept {
		kept[k] = []any{nil}
		e.open = append(e.open, encodeFrame{id: containerID{ptr: reflect.ValueOf(kept[k]).Pointer(), length: 1}})
		e.index()
		n := len(e.open)
		if n > documentedOpenValues && (len(e.buckets) < documentedBucketsPerFrame*n ||
			len(e.buckets) > refiledBucketsPerFrame*n) {
			t.Fatalf("%d frames in %d buckets, want from %d to %d buckets per frame "+
				"(README: at least twice as many as the frames)",
				n, len(e.buckets), documentedBucketsPerFrame, refiledBucketsPerFrame)
		}
	}
}
