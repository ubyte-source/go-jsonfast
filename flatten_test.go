package jsonfast

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// compact reports whether the JSON text out holds no whitespace between its
// tokens, as json.Compact writes it.
func compact(out []byte) bool {
	var c bytes.Buffer
	return json.Compact(&c, out) == nil && bytes.Equal(c.Bytes(), out)
}

// flattenDepth is the nesting FlattenObject documents.
const flattenDepth = 64

// flattenedNothing is what flatten returns when FlattenObject writes nothing.
const flattenedNothing = `{"n":1}`

// documentedPendingLeaves is how many leaf names an object holds before it takes
// a name set, as the documentation of FlattenObject states.
const documentedPendingLeaves = 32

// numberedLeaves returns the members "k0":0 to "k<n-1>":n-1, comma separated.
func numberedLeaves(n int) string {
	fields := make([]string, n)
	for k := range fields {
		fields[k] = fmt.Sprintf(`"k%d":%d`, k, k)
	}
	return strings.Join(fields, ",")
}

// deepObject returns inner as the value of levels objects nested in one
// another, each with the one member k.
func deepObject(inner string, levels int) string {
	return nest(`{"k":`, inner, "}", levels)
}

// flatten runs FlattenObject between a leading field and the closing brace
// of a fresh object and returns the output.
func flatten(data string) (string, error) {
	b := New(0)
	b.BeginObject()
	b.AddIntField("n", 1)
	err := FlattenObject(b, data)
	b.EndObject()
	return string(b.Bytes()), err
}

func TestFlattenObjectWritesLeaves(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{emptyObject, flattenedNothing},
		{`{"KV@123":{"action":"pass","srcip":"1.2.3.4"}}`, `{"n":1,"action":"pass","srcip":"1.2.3.4"}`},
		{`{"L1":{"L2":{"key":"deep"}}}`, `{"n":1,"key":"deep"}`},
		{`{"KV@1":{"a":"1"},"KV@2":{"b":"2","c":"3"}}`, `{"n":1,"a":"1","b":"2","c":"3"}`},
		{`{"key":"value","num":42,"list":[1,{"x":2}]}`, `{"n":1,"key":"value","num":42,"list":[1,{"x":2}]}`},
		{`{"aé":{"b\n":1}}`, `{"n":1,"b\n":1}`},
		{" {\"a\":1} \n", `{"n":1,"a":1}`},
		{"{ \"a\" : [ 1 ,\n2 ] , \"s\" : \"x y\" }", `{"n":1,"a":[1,2],"s":"x y"}`},
		{"{\"sd\":{\"list\":[\n{\"level\":\"info\"}\n]}}", `{"n":1,"list":[{"level":"info"}]}`},
		{`{"k\u00e9":"v"}`, `{"n":1,"k\u00e9":"v"}`},
		{`{"a":{"x":1},"b":{"y":[{"x":2},{"x":3}]}}`, `{"n":1,"x":1,"y":[{"x":2},{"x":3}]}`},
		{`"just a string"`, flattenedNothing},
		{litNull, flattenedNothing},
		{` [ {"a":1} ] `, flattenedNothing},
		{"-1.5", flattenedNothing},
		{deepObject(`"ok"`, flattenDepth), `{"n":1,"k":"ok"}`},
		{deepObject(arrays(1), flattenDepth-1), `{"n":1,"k":[]}`},
		{`{"a":` + arrays(flattenDepth-1) + `}`, `{"n":1,"a":` + arrays(flattenDepth-1) + `}`},
		{`{"a":[{"x":{"x":1}},{"x":2}]}`, `{"n":1,"a":[{"x":{"x":1}},{"x":2}]}`},
		{`{"a":1,"b":{"a":{"x":2}}}`, `{"n":1,"a":1,"x":2}`},
		{`{"a":{"k":{"x":1}},"b":{"k":{"y":2}}}`, `{"n":1,"x":1,"y":2}`},
		{`{"a":{"x":1},"b":[{"a":{},"b":[{"a":1}]}]}`, `{"n":1,"x":1,"b":[{"a":{},"b":[{"a":1}]}]}`},
	} {
		if got, err := flatten(tc.in); err != nil || got != tc.want {
			t.Errorf("FlattenObject(%.40q) = %.60s, %v, want %.60s", tc.in, got, err, tc.want)
		}
	}
}

func TestFlattenObjectLeavesTheBuilderAsItWasOnFailure(t *testing.T) {
	malformed := []string{
		`{broken`, `{"a":1} junk`, `{abc:"val"}`, `{"`, `{"key" "val"}`, `{"key":`,
		`{"key": }`, `{"a":1 "b":2}`, `{"a":`, `{"a":1`, `{"a":1,`, `{"k":{bad}}`,
		`{"a":1,"b":{"c":2,}}`, `{"a":[1,,2]}`, `{"a":[}]}`, `{"a":{"b":[:]}}`, `{"a":[tru]}`,
		`{"a":[{]}`, `{"a":[1 2]}`, `{"a":{[}]}`, `{"x":1,"a":[{"b":[}]}]}`, "", blank, "xyz", "]",
		`"unterminated`, `[1,]`, "01", "tru", "{}\x00", "[1] x",
		"{\"k\":\"v\xff\"}", "{\"k\xfe\":1}", `{"k":"\ud800"}`, `{"a":["\udc00"]}`,
		`{"a",1}`, `{"a"=1}`, `{"a"x"b"}`, `{"a"`, `{"a" `, "\"\xff\"", `["\ud800"]`,
		"{", `{"a":{`, `{"x":1,"x":2,`, `{"x":1,"x":2} junk`, `{"a":[{"x":1,"x":2}],"b":[1,]}`,
		`{"x":1,"x":2,"d":` + deepObject("0", flattenDepth) + "}", "{\"a\":\"}\t",
		deepObject(`"deep"`, flattenDepth+1),
		`{"a":` + arrays(flattenDepth) + `}`,
		arrays(flattenDepth + 1),
	}
	repeated := []string{
		`{"a":{"x":1},"b":{"x":2},"x":3}`, `{"x":1,"\u0078":2}`, `{"a":{"k":1},"b":{"c":{"k":{}}},"k":[]}`,
		`{"a":[{"x":1,"x":2}]}`, `{"b":{"c":[{"y":"y","y":0}]}}`, `{"a":[[{"k":1,"\u006b":2}]]}`,
		`{"a":[{"o":{"z":1,"z":2}}]}`, `{"a":[1,{"p":[],"p":{}}]}`, `{"x":1,"x":2,"d":[1]}`,
		`{"a":[{"p":{"q":1},"p":1}]}`, `{"a":{"x":1},"a":{"y":2}}`, `{"a":1,"a":{"x":2}}`, `{"a":{},"a":{}}`,
		`{"a":{"x":1},"\u0061":[]}`, `{"o":{"b":{},"c":1,"b":[]}}`, `{"o":{"k":{"q":1},"k":{"r":2}},"z":0}`,
	}
	for want, inputs := range map[error][]string{ErrMalformed: malformed, ErrDuplicateName: repeated} {
		for _, in := range inputs {
			if got, err := flatten(in); !is(err, want) || got != flattenedNothing {
				t.Errorf("FlattenObject(%.40q) = %.60s, %v, want the builder untouched and %v", in, got, err, want)
			}
		}
	}
}

func TestFlattenObjectRefusesAnObjectNamedAsAnEarlierLeaf(t *testing.T) {
	leaves := "{" + numberedLeaves(documentedPendingLeaves+documentedPendingLeaves/4)
	for _, tc := range []struct {
		in   string
		want error
	}{
		{leaves + `,"o":{}}`, nil}, {leaves + `,"k0":{}}`, ErrDuplicateName},
		{leaves + `,"k31":{}}`, ErrDuplicateName}, {leaves + `,"k32":{}}`, ErrDuplicateName},
		{leaves + `,"k39":{}}`, ErrDuplicateName},
	} {
		if _, err := flatten(tc.in); !is(err, tc.want) {
			t.Errorf("FlattenObject(%s) = %v, want %v", tc.in[len(leaves)-8:], err, tc.want)
		}
	}
}

func TestFlattenObjectRefusesARepeatPastTheRoomItGaveBack(t *testing.T) {
	// Each pair repeats a name past an object whose names gave their room back: one
	// inside a leaf, one outside, one named by an escape, one named by a view, and the
	// escaped name of an object itself, once a later name took the room it moved from.
	for _, tc := range []struct {
		in   string
		want error
	}{
		{`{"\u0078":1,"a":[{"\u0079":1}],"\u007a":2,"\u0078":3}`, ErrDuplicateName},
		{`{"\u0078":1,"a":[{"\u0079":1}],"\u007a":2}`, nil},
		{`{"\u0061":{"k":1},"b":{"\u0063":{"l":2}},"\u0064":{"m":3},"\u0061":{"n":4}}`, ErrDuplicateName},
		{`{"\u0061":{"k":1},"b":{"\u0063":{"l":2}},"\u0064":{"m":3}}`, nil},
		{`{"\u0061":1,"\u0062":{"c":1},"\u0064":2,"\u0061":3}`, ErrDuplicateName},
		{`{"\u0061":1,"\u0062":{"c":1},"\u0064":2}`, nil},
		{`{"\u0061":1,"b":{"c":1},"\u0064":2,"\u0061":3}`, ErrDuplicateName},
		{`{"\u0061":1,"b":{"c":1},"\u0064":2}`, nil},
		{`{"\u0061":{},"\u0062":1,"\u0061":{}}`, ErrDuplicateName},
		{`{"\u0061":{},"\u0062":1,"\u0063":{}}`, nil},
	} {
		if _, err := flatten(tc.in); !is(err, tc.want) {
			t.Errorf("FlattenObject(%s) = %v, want %v", tc.in, err, tc.want)
		}
	}
}

func TestFlattenObjectKeepsTheSeparatorStateOnFailure(t *testing.T) {
	repeat := `{"a":{"x":1},"x":2}`
	b := New(0)
	b.BeginObject()
	if err := FlattenObject(b, repeat); !is(err, ErrDuplicateName) || b.needSep || string(b.Bytes()) != "{" {
		t.Fatalf("a failed first field = %v and left %s and the separator state %v, want ErrDuplicateName, { and false",
			err, b.Bytes(), b.needSep)
	}
	b.AddIntField("n", 1)
	if err := FlattenObject(b, repeat); !is(err, ErrDuplicateName) || !b.needSep || string(b.Bytes()) != `{"n":1` {
		t.Fatalf("a failed later field = %v and left %s and the separator state %v, want %v, %s and true",
			err, b.Bytes(), b.needSep, ErrDuplicateName, `{"n":1`)
	}
}

// Allocations of FlattenObject for 63 or 64 objects that each take a set and a mark:
// the sets grow through setStacks heap stacks, the names of the leaves and of the
// innermost object spill to two maps, and every mark stays inline.
const (
	setStacks  = 5
	deepAllocs = setStacks + 2*spillAllocs
)

func TestFlattenObjectAllocs(t *testing.T) {
	sd := []byte(`{"KV@32473":{"action":"pass","srcip":"1.2.3.4","dstip":"5.6.7.8","ports":[ 80, 443 ]}}`)
	escaped := escapedObject()
	objects := []byte(`{"list":[{"level":"info"},{"level":"warn","tags":[{"k":1}]}]}`)
	// Each object but the innermost holds an object, so it takes a name set and a mark.
	inline, spilled := []byte(deepObject("1", documentedNameSets+1)), []byte(deepObject("1", documentedNameSets+2))
	// The innermost object of these holds its leaf names with no set of its own.
	pending := []byte(deepObject("{"+numberedLeaves(documentedPendingLeaves)+"}", documentedNameSets))
	// The innermost object of these holds spilledLeaves leaves, so it takes a mark too.
	leaves := leafSets("k")[0]
	marked, allMarked := []byte(deepObject(leaves, flattenDepth-2)), []byte(deepObject(leaves, flattenDepth-1))
	b := New(0)
	for _, tc := range []struct {
		data   []byte
		allocs float64
	}{
		{sd, 0}, {escaped, escapedChunks}, {objects, 0}, {inline, 0}, {spilled, 1}, {pending, 0},
		{marked, deepAllocs}, {allMarked, deepAllocs},
	} {
		assertAllocs(t, tc.allocs, func() {
			b.Reset()
			b.BeginObject()
			if err := FlattenObject(b, tc.data); err != nil {
				t.Fatalf("FlattenObject(%s) = %v, want nil", tc.data, err)
			}
			b.EndObject()
		})
	}
	refused := []byte(`{` + refusedLate() + `:1}`)
	assertAllocs(t, 0, func() {
		b.Reset()
		if err := FlattenObject(b, refused); !is(err, ErrMalformed) || b.Len() != 0 {
			t.Fatalf("FlattenObject(%.12q) = %v with %d bytes written, want ErrMalformed and none",
				refused, err, b.Len())
		}
	})
}

// spilledLeaves is how many leaves past the pending ones an object of
// TestFlattenObjectSpillsOneMapPerLevel holds, so it spills to a map.
const spilledLeaves = documentedPendingLeaves + documentedPendingLeaves/4

// leafSets returns one object per prefix, each holding spilledLeaves leaves
// named after its prefix.
func leafSets(prefixes ...string) []string {
	objects := make([]string, len(prefixes))
	for k, prefix := range prefixes {
		fields := make([]string, spilledLeaves)
		for i := range fields {
			fields[i] = fmt.Sprintf(`"%s%d":%d`, prefix, i, i)
		}
		objects[k] = fmt.Sprintf("{%s}", strings.Join(fields, ","))
	}
	return objects
}

func TestFlattenObjectSpillsOneMapPerLevel(t *testing.T) {
	inLeaf := func(n int) []byte {
		objects := leafSets(slices.Repeat([]string{"k"}, n)...)
		return []byte(`{"a":[` + strings.Join(objects, ",") + `]}`)
	}
	outside := func(prefixes ...string) []byte {
		named := make([]string, 0, len(prefixes))
		for _, prefix := range prefixes {
			named = append(named, quoted(prefix)+":"+strings.Join(leafSets(prefix), ""))
		}
		return []byte(fmt.Sprintf("{%s}", strings.Join(named, ",")))
	}
	b := New(1 << 16)
	for _, tc := range []struct {
		data   []byte
		allocs float64
	}{
		{inLeaf(1), spillAllocs}, {inLeaf(documentedNameSets * documentedNameSets), spillAllocs},
		{outside("p"), 2 * spillAllocs}, {outside("p", "q"), 2 * spillAllocs},
	} {
		assertAllocs(t, tc.allocs, func() {
			b.Reset()
			if err := FlattenObject(b, tc.data); err != nil {
				t.Fatalf("FlattenObject(%.40s) = %v, want nil", tc.data, err)
			}
		})
	}
}

func TestFlattenObjectAllocatesForTheOpenObjectsOnly(t *testing.T) {
	b := New(0)
	for _, n := range []int{someSiblings, manySiblings} {
		data := siblings(n, `{"\u0078":0}`)
		b.Grow(2 * len(data))
		assertAllocs(t, escapedChunks, func() {
			b.Reset()
			if err := FlattenObject(b, data); err != nil {
				t.Fatalf("FlattenObject of %d sibling objects = %v, want nil", n, err)
			}
		})
	}
}

// Sizes of objectTree: treeLevels levels of up to treeFanout members, fewer than a
// set holds inline, so that with the objects inside them more than documentedNameSets
// objects are open at once.
const (
	treeLevels = 3
	treeFanout = 16
)

// objectTree returns treeLevels levels of objects, each with fanout members "m0" to
// "m<fanout-1>", around the objects {name:{}}: fanout^treeLevels objects named name
// close one at a time, and no set holds more than fanout names.
func objectTree(fanout int, name string) []byte {
	tree := fmt.Sprintf("{%s:{}}", name)
	for range treeLevels {
		fields := make([]string, fanout)
		for k := range fields {
			fields[k] = fmt.Sprintf(`"m%d":%s`, k, tree)
		}
		tree = fmt.Sprintf("{%s}", strings.Join(fields, ", "))
	}
	return []byte(tree)
}

// treeAllocs is what FlattenObject allocates for an objectTree whose name it decodes:
// the heap stack of sets, and one chunk in each of its two arenas.
const treeAllocs = 3

func TestFlattenObjectGivesBackTheRoomOfTheObjectsItLeaves(t *testing.T) {
	b := New(0)
	for _, fanout := range []int{1, treeFanout} {
		data := objectTree(fanout, `"objectname\u0078"`)
		assertAllocs(t, treeAllocs, func() {
			b.Reset()
			if err := FlattenObject(b, data); err != nil || b.Len() != 0 {
				t.Fatalf("FlattenObject of a tree of fanout %d = %v and wrote %d bytes, want nil and none",
					fanout, err, b.Len())
			}
		})
	}
}

func TestFlattenObjectSizesTheChunksOfObjectNamesByTheRestOfData(t *testing.T) {
	for _, tc := range []struct {
		doc  string
		want int
	}{
		{`{"\u0078":{"a":1}}`, len(`"a":1}}`)},
		{`{"\u0078":{"a":1},"pad":"` + strings.Repeat("p", documentedRoom) + `"}`, documentedRoom},
	} {
		data := []byte(tc.doc)
		f := flattener{b: New(0), data: data}
		if err := f.run(newWalker(data, flattenDepth)); err != nil || cap(f.nested.buf) != tc.want {
			t.Errorf("run of %.20s = %v with its object names in a chunk of %d, want nil and one of %d",
				tc.doc, err, cap(f.nested.buf), tc.want)
		}
	}
}

// flatLeaves lists the leaves FlattenObject writes for the object that dec
// reads, each value compacted, as json.Decoder reads them.
func flatLeaves(dec *json.Decoder) (names []string, values []json.RawMessage, err error) {
	if _, err = dec.Token(); err != nil {
		return nil, nil, fmt.Errorf("open: %w", err)
	}
	for dec.More() {
		name, terr := dec.Token()
		var raw json.RawMessage
		if derr := dec.Decode(&raw); terr != nil || derr != nil {
			return nil, nil, errors.Join(terr, derr)
		}
		if opens(raw, '{') {
			inner, innerValues, ierr := flatLeaves(json.NewDecoder(bytes.NewReader(raw)))
			if ierr != nil {
				return nil, nil, ierr
			}
			names, values = append(names, inner...), append(values, innerValues...)
			continue
		}
		var c bytes.Buffer
		if cerr := json.Compact(&c, raw); cerr != nil {
			return nil, nil, fmt.Errorf("compact: %w", cerr)
		}
		names, values = append(names, fmt.Sprint(name)), append(values, c.Bytes())
	}
	return names, values, nil
}

func FuzzFlattenObject(f *testing.F) {
	for _, s := range []string{
		`{"KV@123":{"action":"pass"}}`, `{"a":{"b":{"c":"deep"}}}`, `{"x":123}`, objectTrail,
		`{"a":[1,,2]}`, `{"a":[}]}`, `{"a":{"b":[:]}}`, `{"a":[tru]}`, "{\"a\":[\n1 ]}", "xyz", "1e1000",
		`{"a":{"x":1},"x":2}`, `{"x":1,"\u0078":2}`, "{\"s\":\"\xff\"}", `{"s":"\ud800"}`,
		`{"a":[{"x":1,"x":2}]}`, `{"a":[{"x":{"x":1}},{"x":2}]}`, `{"a":{"x":1},"a":{"y":2}}`,
		`{"a":1,"b":{"a":{"x":2}}}`, `{"a":[1e1000],"b":{"c":-1e999}}`,
		`{"\u0078":1,"a":[{"\u0079":1}],"\u007a":2,"\u0078":3}`,
		`{"\u0061":{"k":1},"b":{"\u0063":{"l":2}},"\u0064":{"m":3},"\u0061":{"n":4}}`,
		`{"\u0061":{},"\u0062":1,"\u0061":{}}`,
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		b := New(0)
		b.BeginObject()
		err := FlattenObject(b, data)
		b.EndObject()
		switch out := b.Bytes(); {
		case !is(err, flattenFault(data)):
			t.Fatalf("FlattenObject(%q) = %v; encoding/json and the depth bound say %v", data, err, flattenFault(data))
		case err != nil && string(out) != emptyObject:
			t.Fatalf("FlattenObject(%q) failed and left %s, want {}", data, out)
		case err == nil && !compact(out):
			t.Fatalf("FlattenObject(%q) = nil and wrote %q, want compact JSON", data, out)
		case err == nil && opens(data, '{'):
			assertLeaves(t, data, out)
		}
		strictlyRead(t, b.Bytes())
	})
}

// flattenFault returns what FlattenObject must return for data, by encoding/json:
// ErrMalformed, then ErrDuplicateName when an object repeats a name or two leaves
// share one, and nil otherwise.
func flattenFault(data []byte) error {
	depth, err := nesting(data)
	if !jsonUTF8(data) || err != nil || depth > flattenDepth {
		return ErrMalformed
	}
	if !opens(data, '{') {
		return nil
	}
	names, _, err := flatLeaves(json.NewDecoder(bytes.NewReader(data)))
	if err != nil || !strictJSON(data) || len(names) != len(slices.Compact(slices.Sorted(slices.Values(names)))) {
		return ErrDuplicateName
	}
	return nil
}

// assertLeaves fails tb unless the object out holds the leaves of data as
// json.Decoder reads them.
func assertLeaves(tb testing.TB, data, out []byte) {
	tb.Helper()
	names, values, err := flatLeaves(json.NewDecoder(bytes.NewReader(data)))
	got, gotValues, gerr := decoderMembers(out)
	if err != nil || gerr != nil || !slices.Equal(got, names) || !slices.EqualFunc(gotValues, values, rawEqual) {
		tb.Fatalf("FlattenObject(%q) = %s; json.Decoder leaves %q, %v", data, out, names, err)
	}
}

func TestFlattenObjectCompactsArrayLeaves(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{" { \"a b\" : [ 1 ,\t2 ] ,\r\n\"c\" : { } } ", `{"a b":[1,2],"c":{}}`},
		{"\n{\"level\":\"info\"}\n", `{"level":"info"}`},
		{` "\" x" `, `"\" x"`},
		{" -1.5e+3 ", "-1.5e+3"},
		{"[true , false , null]", "[true,false,null]"},
		{" [ [ ] , { } ] ", "[[],{}]"},
	} {
		got, err := flatten(`{"v":[` + tc.in + `]}`)
		if want := `{"n":1,"v":[` + tc.want + `]}`; err != nil || got != want {
			t.Errorf("FlattenObject of the leaf [%s] = %s, %v, want %s", tc.in, got, err, want)
		}
	}
}

// deepRecord returns levels objects nested in one another, each with a string
// and an array leaf of its own, around an empty one.
func deepRecord(levels int) string {
	var sb strings.Builder
	for n := range levels {
		id := strconv.Itoa(n)
		sb.WriteString(`{"s` + id + `":"x","v` + id + `":[1,2],"k":`)
	}
	sb.WriteString(emptyObject + strings.Repeat("}", levels))
	return sb.String()
}

func BenchmarkFlattenObject(b *testing.B) {
	flat := []byte(`{"KV@32473":{"action":"pass","srcip":"1.2.3.4",` +
		`"dstip":"5.6.7.8","service":"HTTP","srcport":"54321","dstport":"80"}}`)
	deep := []byte(deepRecord(flattenDepth - 1))
	for _, bc := range []struct {
		name string
		data []byte
	}{{"Record", flat}, {"Deep64", deep}} {
		b.Run(bc.name, func(b *testing.B) {
			builder := New(0)
			builder.BeginObject()
			if err := FlattenObject(builder, bc.data); err != nil {
				b.Fatalf("FlattenObject(%s) = %v, want nil", bc.data, err)
			}
			builder.EndObject()
			assertLeaves(b, bc.data, builder.Bytes())
			b.ReportAllocs()
			b.SetBytes(int64(len(bc.data)))
			for b.Loop() {
				builder.Reset()
				builder.BeginObject()
				if err := FlattenObject(builder, bc.data); err != nil {
					b.Fatalf("FlattenObject = %v, want nil", err)
				}
				builder.EndObject()
			}
		})
	}
}

func ExampleFlattenObject() {
	b := New(0)
	b.BeginObject()
	b.AddStringField("device", "fw01")
	err := FlattenObject(b, `{"sd@1":{"action":"pass"},"sd@2":{"ports":[ 80, 443 ]}}`)
	b.EndObject()
	fmt.Println(string(b.Bytes()), err)
	fmt.Println(FlattenObject(b, `{"a":{"x":1},"x":2}`), FlattenObject(b, `{"a":1,}`))
	// Output:
	// {"device":"fw01","action":"pass","ports":[80,443]} <nil>
	// jsonfast: malformed JSON: duplicate member name jsonfast: malformed JSON
}

func TestFlattenObjectStopsWritingAtARepeatedName(t *testing.T) {
	refused := func(b *Builder, doc string) {
		if err := FlattenObject(b, doc); !is(err, ErrDuplicateName) || b.Len() != 0 {
			t.Fatalf("FlattenObject of %d bytes = %v with %d bytes left, want %v with none",
				len(doc), err, b.Len(), ErrDuplicateName)
		}
	}
	short, long := repeatedName+repeatedTail(0), repeatedName+repeatedTail(tailMembers)
	shortB, longB := New(0), New(0)
	refused(shortB, short)
	refused(longB, long)
	if cap(longB.buf) != cap(shortB.buf) {
		t.Fatalf("FlattenObject grew the Builder to %d bytes on %d bytes past the repeat, want %d as without them",
			cap(longB.buf), len(long)-len(short), cap(shortB.buf))
	}
	for _, doc := range []string{short, long} {
		b := New(0)
		assertAllocs(t, 0, func() {
			b.Reset()
			refused(b, doc)
		})
	}
}

func TestFlattenObjectCostIsLinear(t *testing.T) {
	run := func(doc []byte) func() {
		return func() { noError(t, FlattenObject(New(0), doc)) }
	}
	assertCost(t, run(levelsDocument(costLevels)), run(levelsDocument(costScale*costLevels)), costScale)
}
