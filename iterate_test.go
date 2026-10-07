package jsonfast

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// escapedRepeat is an object whose second name, escaped, repeats the first.
const escapedRepeat = `{"a":1,"\u0061":2}`

// objectMalformed returns texts that no object walk accepts.
func objectMalformed() []string {
	return []string{
		"", blank, `"string"`, `[7]`, `{`, `{ `, `{abc:"val"}`, `{"`, `{"key" "val"}`,
		`{"key":`, `{"key":}`, `{"key": }`, `{"a":1 "b":2}`, `{"a":1`, `{"a":1,`,
		`{"a":1,"`, objectTrail, `{"a":1, }`, `{,}`, `{"a":1} x`, `{"a":1}}`,
		`{"a":1}{"b":2}`, `{"a":"\q"}`, `{"\x":1}`, "{\"a\x01\":1}", `{"a":tru}`,
		`{"a":01}`, `{"a":{"b":1}`, `{"a":"b}`, `{"a":1;"b":2}`, `{"a";1}`, `{} x`,
		`{"a"`, `{"a" `, `{:1}`, "{\"\t:1}",
	}
}

func TestIterateFieldsReportsRawMembers(t *testing.T) {
	doc := ` {"a":"1", "b\u0062" : 2 ,"c":{"d":[true]}} ` + "\n"
	var keys, values []string
	err := IterateFields(doc, func(key, value string) error {
		keys, values = append(keys, key), append(values, value)
		return nil
	})
	if err != nil {
		t.Fatalf("IterateFields = %v, want nil", err)
	}
	if got, want := strings.Join(keys, " "), `"a" "b\u0062" "c"`; got != want {
		t.Errorf("keys = %s, want %s", got, want)
	}
	if got, want := strings.Join(values, " "), `"1" 2 {"d":[true]}`; got != want {
		t.Errorf("values = %s, want %s", got, want)
	}
	if err := IterateFields(" { } ", func(string, string) error { return errStop }); err != nil {
		t.Errorf("IterateFields({}) = %v, want nil without a call", err)
	}
}

func TestIterateFieldsRejectsMalformedObjects(t *testing.T) {
	for _, doc := range objectMalformed() {
		calls := 0
		err := IterateFields([]byte(doc), func(_, _ []byte) error {
			calls++
			return nil
		})
		if !is(err, ErrMalformed) {
			t.Errorf("IterateFields(%q) = %v after %d calls, want ErrMalformed", doc, err, calls)
		}
	}
}

func TestIterateFieldsReturnsTheCallbackErrorUnchanged(t *testing.T) {
	calls := 0
	err := IterateFields(`{"a":1,"b":2,"c":`, func(_, _ string) error {
		calls++
		if calls == 2 {
			return errStop
		}
		return nil
	})
	if !is(err, errStop) || calls != 2 {
		t.Fatalf("IterateFields = %v after %d calls, want errStop after 2", err, calls)
	}
}

func TestIterateFieldsViewsFollowTheInputType(t *testing.T) {
	doc := `{"name":"value"}`
	noError(t, IterateFields(doc, func(key, value string) error {
		if !aliases(key, doc, 1) || !aliases(value, doc, 8) {
			t.Errorf("views %q and %q are copies, want views of the input", key, value)
		}
		return nil
	}))
	data := []byte(`{"k":"v","n":1}`)
	var first []byte
	noError(t, IterateFields(data, func(key, value []byte) error {
		if !capped(key) || !capped(value) {
			t.Errorf("views %q and %q have room past their end, want capped views", key, value)
		}
		if first == nil {
			first = value
		}
		return nil
	}))
	_ = append(first, `,"injected":true`...)
	if string(data) != `{"k":"v","n":1}` {
		t.Fatalf("appending to a view rewrote the input to %s, want it unchanged", data)
	}
	noError(t, IterateFields(json.RawMessage(`{"r":[1]}`), func(_, value json.RawMessage) error {
		if string(value) != oneArray {
			t.Errorf("json.RawMessage value = %s, want [1]", value)
		}
		return nil
	}))
}

func TestIterateFieldsAllocs(t *testing.T) {
	data := []byte(`{"facility":23,"severity":3,"hostname":"FW01","app_name":"utm"}`)
	doc := string(data)
	assertAllocs(t, 0, func() {
		noError(t, IterateFields(data, func(_, _ []byte) error { return nil }))
		noError(t, IterateFields(doc, func(_, _ string) error { return nil }))
	})
}

// sameValues reports whether got holds the raw values of want, each without
// the whitespace encoding/json keeps around it.
func sameValues[T Text](got []T, want []json.RawMessage) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if !bytes.Equal(bytesOf(got[i]), bytes.TrimSpace(want[i])) {
			return false
		}
	}
	return true
}

// errName reports a key that DecodeString rejects.
var errName = errors.New("ill-formed key")

// decodedFields collects the decoded keys and raw values IterateFields
// reports.
func decodedFields(data []byte) (names []string, values [][]byte, err error) {
	err = IterateFields(data, func(key, value []byte) error {
		name, ok := DecodeString(key)
		if !ok {
			return errName
		}
		names, values = append(names, name), append(values, value)
		return nil
	})
	return names, values, err
}

// shallowItem is one item of a container: the name token and the value of a
// member, or the value of an element.
type shallowItem struct {
	key, value []byte
}

// shallowWalk reads the container that opener starts in data by the grammar of the
// container walks: its items up to the first fault, each once whole, and
// ErrMalformed unless it closed with only whitespace around it.
func shallowWalk(data []byte, opener byte) (items []shallowItem, err error) {
	closer := refCloser(opener)
	i := refSpace(data, 0)
	if !refAt(data, i, opener) {
		return nil, ErrMalformed
	}
	if i = refSpace(data, i+1); refAt(data, i, closer) {
		return nil, shallowEnd(data, i)
	}
	for {
		item, end := shallowItemAt(data, i, opener)
		if end < 0 {
			return items, ErrMalformed
		}
		items = append(items, item)
		if i = refSpace(data, end); refAt(data, i, closer) {
			return items, shallowEnd(data, i)
		}
		if !refAt(data, i, ',') {
			return items, ErrMalformed
		}
		i = refSpace(data, i+1)
	}
}

// shallowItemAt reads the item at data[i] of the container that opener starts and
// returns it and the index past it, or -1.
func shallowItemAt(data []byte, i int, opener byte) (item shallowItem, end int) {
	if opener == '{' {
		nameEnd := refString(data, i)
		if nameEnd < 0 {
			return shallowItem{}, -1
		}
		item.key, i = data[i:nameEnd], refSpace(data, nameEnd)
		if !refAt(data, i, ':') {
			return shallowItem{}, -1
		}
		i = refSpace(data, i+1)
	}
	if end = refValue(data, i); end < 0 {
		return shallowItem{}, -1
	}
	item.value = data[i:end]
	return item, end
}

// shallowEnd returns nil when only whitespace follows the closer data[i], and
// ErrMalformed otherwise.
func shallowEnd(data []byte, i int) error {
	if refSpace(data, i+1) != len(data) {
		return ErrMalformed
	}
	return nil
}

// stdName decodes the name token of item with encoding/json.
func stdName(t *testing.T, item shallowItem) string {
	t.Helper()
	var name string
	if err := json.Unmarshal(item.key, &name); err != nil {
		t.Fatalf("encoding/json reads the name %q with %v, want nil", item.key, err)
	}
	return name
}

// sameItems reports whether the names and values a walk reported are those of
// items; an empty names stands for the elements of an array.
func sameItems(names, values [][]byte, items []shallowItem) bool {
	if len(values) != len(items) || len(names) != 0 && len(names) != len(items) {
		return false
	}
	for k, item := range items {
		if !bytes.Equal(values[k], item.value) || len(names) != 0 && !bytes.Equal(names[k], item.key) {
			return false
		}
	}
	return true
}

func FuzzIterateFields(f *testing.F) {
	seeds := []string{`{"a":"1","b":2,"c":true}`, `{"a":{"b":1}}`, `{"\u0061":1}`, `{"n":[1,,2]}`, `[1]`}
	for _, s := range append(seeds, objectMalformed()...) {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var keys, values [][]byte
		err := IterateFields(data, func(key, value []byte) error {
			keys, values = append(keys, key), append(values, value)
			return nil
		})
		if items, want := shallowWalk(data, '{'); !is(err, want) || !sameItems(keys, values, items) {
			t.Fatalf("IterateFields(%q) = %q %q, %v; the shallow grammar = %q, %v",
				data, keys, values, err, items, want)
		}
		if !json.Valid(data) || !opens(data, '{') {
			return
		}
		names, _, nameErr := decodedFields(data)
		want, raws, derr := decoderMembers(data)
		if nameErr != nil || derr != nil || !slices.Equal(names, want) || !sameValues(values, raws) {
			t.Fatalf("IterateFields(%q) = %q, %v; json.Decoder = %q, %v", data, names, nameErr, want, derr)
		}
	})
}

// fixtureObject is a small log record for the scan benchmarks.
const fixtureObject = `{"facility":23,"severity":3,"hostname":"FW01","app_name":"utm",` +
	`"source":"10.0.0.1","message":"test"}`

func BenchmarkIterateFields(b *testing.B) {
	data := []byte(fixtureObject)
	gotNames, gotValues, err := decodedFields(data)
	wantNames, wantValues, derr := decoderMembers(data)
	if err != nil || derr != nil || !slices.Equal(gotNames, wantNames) || !sameValues(gotValues, wantValues) {
		b.Fatalf("IterateFields = %q, %v; json.Decoder = %q, %v", gotNames, err, wantNames, derr)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		if err := IterateFields(data, func(_, _ []byte) error { return nil }); err != nil {
			b.Fatalf("IterateFields = %v, want nil", err)
		}
	}
}

func ExampleIterateFields() {
	var found []string
	err := IterateFields(`{"id":7,"tags":["a"]}`, func(key, value string) error {
		if EqualString(key, "tags") {
			found = append(found, key+"="+value)
		}
		return nil
	})
	fmt.Println(found, err)
	// Output: ["tags"=["a"]] <nil>
}

// members collects the decoded names and raw values IterateMembers reports.
func members[T Text](data T) (got map[string]T, order []string, err error) {
	got = map[string]T{}
	err = IterateMembers(data, func(name, value T) error {
		got[string(name)] = value
		order = append(order, string(name))
		return nil
	})
	return got, order, err
}

func TestIterateMembersDecodesNames(t *testing.T) {
	got, order, err := members(`{"plain":1,"caf\u00e9":"x","\ud83d\ude80":[],"":null,"x\"y":{"c":1}}`)
	if err != nil {
		t.Fatalf("IterateMembers = %v, want nil", err)
	}
	if got, want := strings.Join(order, "|"), "plain|café|🚀||x\"y"; got != want {
		t.Errorf("names = %q, want %q", got, want)
	}
	want := map[string]string{plainText: "1", cafe: `"x"`, "🚀": emptyArray, "": litNull, `x"y`: `{"c":1}`}
	if !maps.Equal(got, want) {
		t.Errorf("members = %q, want %q", got, want)
	}
}

func TestIterateMembersRejectsNamesEqualAfterDecoding(t *testing.T) {
	for _, tc := range []struct {
		doc    string
		before int
	}{
		{repeatedA, 1},
		{escapedRepeat, 1},
		{`{"\u00e9":1,"é":2}`, 1},
		{`{"\ud800":1,"\udbff":2}`, 1},
		{`{"\ud800":1,"\ufffd":2}`, 1},
		{"{\"\xff\":1,\"\\ufffd\":2}", 1},
		{"{\"\xff\":1,\"\xfe\":2}", 1},
		{`{"x":{"a":1},"y":2,"x":3}`, 2},
	} {
		calls := 0
		err := IterateMembers(tc.doc, func(_, _ string) error {
			calls++
			return nil
		})
		if !is(err, ErrDuplicateName) || calls != tc.before {
			t.Errorf("IterateMembers(%q) = %v after %d calls, want ErrDuplicateName after %d",
				tc.doc, err, calls, tc.before)
		}
	}
	if _, _, err := members(`{"a":{"a":1,"b":2},"b":{"a":3}}`); err != nil {
		t.Errorf("IterateMembers of names repeated only in nested objects = %v, want nil", err)
	}
}

// manyMembers returns an object of n members named k0, k1, ... whose last
// name repeats the name at index dup when dup is not negative.
func manyMembers(n, dup int) string {
	var sb strings.Builder
	sb.WriteByte('{')
	for i := range n {
		if i > 0 {
			sb.WriteByte(',')
		}
		name := i
		if i == n-1 && dup >= 0 {
			name = dup
		}
		sb.WriteString(`"k` + strconv.Itoa(name) + `":` + strconv.Itoa(i))
	}
	sb.WriteByte('}')
	return sb.String()
}

func TestIterateMembersFindsDuplicatesAcrossTheSpill(t *testing.T) {
	for _, tc := range []struct {
		want   error
		n, dup int
	}{
		{nil, inlineNames, -1},
		{nil, inlineNames + 1, -1},
		{nil, inlineNames + 2, -1},
		{nil, manyNames, -1},
		{ErrDuplicateName, inlineNames, 0},
		{ErrDuplicateName, inlineNames, inlineNames - 2},
		{ErrDuplicateName, inlineNames + 1, 0},
		{ErrDuplicateName, inlineNames + 1, inlineNames - 1},
		{ErrDuplicateName, inlineNames + 2, inlineNames},
		{ErrDuplicateName, manyNames, manyNames / 2},
		{ErrDuplicateName, manyNames, 2},
	} {
		got, _, err := members(manyMembers(tc.n, tc.dup))
		if !is(err, tc.want) {
			t.Errorf("%d members, duplicate of %d: err = %v, want %v", tc.n, tc.dup, err, tc.want)
		}
		if tc.want == nil && len(got) != tc.n {
			t.Errorf("%d members: collected %d, want %d", tc.n, len(got), tc.n)
		}
	}
}

func TestIterateMembersKeepsEveryNameIntact(t *testing.T) {
	doc := `{"a\u00e9":1,"plain":2,"b\n":3,"` + strings.Repeat("c", longRun) + `\t":4,"\u0064":5}`
	want := []string{"aé", plainText, "b\n", strings.Repeat("c", longRun) + "\t", "d"}
	kept := make([]string, 0, len(want))
	noError(t, IterateMembers(doc, func(name, _ string) error {
		kept = append(kept, name)
		return nil
	}))
	if !slices.Equal(kept, want) {
		t.Fatalf("names kept past the callback = %q, want %q", kept, want)
	}
	if !aliases(kept[1], doc, strings.Index(doc, plainText)) {
		t.Fatalf("the clean name %q is a copy, want a view of the input", kept[1])
	}
	data := []byte(doc)
	var views [][]byte
	noError(t, IterateMembers(data, func(name, _ []byte) error {
		views = append(views, name)
		return nil
	}))
	for i, v := range views {
		if !capped(v) {
			t.Errorf("name %d, %q, has capacity %d, want %d", i, v, cap(v), len(v))
		}
		_ = append(v, 'X')
	}
	for i, v := range views {
		if string(v) != want[i] || string(data) != doc {
			t.Fatalf("appending to a name view rewrote name %d to %q or the input to %s, want %q and %s",
				i, v, data, want[i], doc)
		}
	}
}

func TestIterateMembersFollowsTheIterateFieldsContract(t *testing.T) {
	for _, doc := range objectMalformed() {
		if _, _, err := members(doc); !is(err, ErrMalformed) {
			t.Errorf("IterateMembers(%q) = %v, want ErrMalformed", doc, err)
		}
	}
	for _, doc := range []string{repeatedA, `{"a":1,"b":}`} {
		if err := IterateMembers(doc, func(_, _ string) error { return errStop }); !is(err, errStop) {
			t.Errorf("IterateMembers(%q) = %v, want the callback error", doc, err)
		}
	}
	if err := IterateMembers(`{"a":1,"a":}`, func(_, _ string) error { return nil }); !is(err, ErrMalformed) {
		t.Errorf("IterateMembers = %v, want ErrMalformed: a member is read whole before its name counts", err)
	}
}

// Name counts IterateMembers documents: the names it holds inline; manyNames is
// well past them.
const (
	documentedInlineNames = 32
	manyNames             = 100
)

// sameView reports whether got, ok is want, wantOK, and got nil when ok is false.
func sameView(got []byte, ok bool, want []byte, wantOK bool) bool {
	return ok == wantOK && (ok && bytes.Equal(got, want) || !ok && got == nil)
}

func TestIterateMembersAllocs(t *testing.T) {
	inline := []byte(manyMembers(documentedInlineNames, -1))
	inlineDoc := string(inline)
	escaped := []byte(`{"a\u00e9":1,"b\n":2}`)
	fn := func(_, _ []byte) error { return nil }
	assertAllocs(t, 0, func() { noError(t, IterateMembers(inline, fn)) })
	assertAllocs(t, 0, func() { noError(t, IterateMembers(inlineDoc, func(_, _ string) error { return nil })) })
	assertAllocs(t, 1, func() { noError(t, IterateMembers(escaped, fn)) })
	for _, n := range []int{documentedInlineNames + 1, 2 * documentedInlineNames} {
		spill := []byte(manyMembers(n, -1))
		assertAllocs(t, spillAllocs, func() { noError(t, IterateMembers(spill, fn)) })
	}
}

func TestIterateMembersDecodesEscapedNamesIntoOneChunk(t *testing.T) {
	data := escapedObject()
	assertAllocs(t, escapedChunks, func() { noError(t, IterateMembers(data, func(_, _ []byte) error { return nil })) })
}

// Two names of longName escapes each, and the arena chunks their decoded bytes take:
// 4 KiB, which holds the first, then 8 KiB, twice the last.
const (
	longName       = 3 << 10
	longNameChunks = 2
)

func TestIterateMembersDecodesLongNamesIntoChunksUpToTheRoom(t *testing.T) {
	data := []byte(`{"` + strings.Repeat(`\u0078`, longName) + `":0,"` + strings.Repeat(`\u0079`, longName) + `":0}`)
	assertAllocs(t, longNameChunks, func() { noError(t, IterateMembers(data, func(_, _ []byte) error { return nil })) })
}

func FuzzIterateMembers(f *testing.F) {
	for _, s := range []string{
		escapedRepeat, "{\"\xff\":1,\"\\ufffd\":2}", `{"x":{"x":1}}`, `{"a":1,"a":[}`, `{"m":[,],"m":1}`,
		manyMembers(manyNames, documentedInlineNames), oneArray, quotedHello,
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		got, order, err := members(data)
		items, want := shallowWalk(data, '{')
		seen := map[string]bool{}
		var wantOrder []string
		for _, item := range items {
			name := stdName(t, item)
			if seen[name] {
				want = ErrDuplicateName
				break
			}
			if seen[name], wantOrder = true, append(wantOrder, name); !bytes.Equal(got[name], item.value) {
				t.Fatalf("IterateMembers(%q) gave %q the value %q, want %q", data, name, got[name], item.value)
			}
		}
		if !is(err, want) || !slices.Equal(order, wantOrder) {
			t.Fatalf("IterateMembers(%q) = %q, %v; the shallow grammar and encoding/json = %q, %v",
				data, order, err, wantOrder, want)
		}
	})
}

// checkMembers fails tb unless IterateMembers reports, in order, the member names
// and values of the object data that json.Decoder reads.
func checkMembers(tb testing.TB, data []byte) {
	tb.Helper()
	wantNames, wantValues, err := decoderMembers(data)
	noError(tb, err)
	i := 0
	err = IterateMembers(data, func(gotName, gotValue []byte) error {
		if i >= len(wantValues) || string(gotName) != wantNames[i] || !rawEqual(gotValue, wantValues[i]) {
			return errStop
		}
		i++
		return nil
	})
	if err != nil || i != len(wantNames) {
		tb.Fatalf("IterateMembers(%s) = %v after %d members, want nil after the %d json.Decoder reads",
			data, err, i, len(wantNames))
	}
}

func BenchmarkIterateMembers(b *testing.B) {
	for _, bc := range []struct{ name, doc string }{
		{"Plain", fixtureObject},
		{"Escaped", `{"caf\u00e9":1,"line\nbreak":2,"plain":3,"tab\tbed":4}`},
		{"Names32", manyMembers(inlineNames, -1)},
	} {
		data := []byte(bc.doc)
		checkMembers(b, data)
		b.Run(bc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			for b.Loop() {
				if err := IterateMembers(data, func(_, _ []byte) error { return nil }); err != nil {
					b.Fatalf("IterateMembers = %v, want nil", err)
				}
			}
		})
	}
}

func ExampleIterateMembers() {
	doc := `{"name":"alice","role":"api","\u006eame":"mallory"}`
	var names []string
	err := IterateMembers(doc, func(name, _ string) error {
		names = append(names, name)
		return nil
	})
	fmt.Println(names, errors.Is(err, ErrDuplicateName), errors.Is(err, ErrMalformed))
	// Output: [name role] true true
}

func TestIterateObjectWalksOnlyWhatValidUTF8Accepts(t *testing.T) {
	cases := append(validityCases(), utf8Cases()...)
	for _, s := range objectMalformed() {
		cases = append(cases, validityCase{in: s})
	}
	for _, tc := range cases {
		data := []byte(tc.in)
		calls := 0
		err := IterateObject(data, MaxDepth, func(_, _ []byte) error {
			calls++
			return nil
		})
		names, _ := objectMembers(data)
		want := objectFault(data, MaxDepth, names)
		if !is(err, want) || (want == nil && calls != len(names)) {
			t.Errorf("IterateObject(%q) = %v after %d calls, want %v", tc.in, err, calls, want)
		}
		if got := IterateObject(tc.in, MaxDepth, func(_, _ string) error { return nil }); !is(got, err) {
			t.Errorf("IterateObject(%q) = %v from a string, want %v as from bytes", tc.in, got, err)
		}
	}
}

// objectMembers lists the members of the object data as json.Decoder reads
// them, or none when it reads no object.
func objectMembers(data []byte) (names []string, values []json.RawMessage) {
	names, values, err := decoderMembers(data)
	if err != nil {
		return nil, nil
	}
	return names, values
}

// objectFault returns what IterateObject must return at maxDepth for data, whose
// member names json.Decoder reads as names: ErrMalformed, then ErrDuplicateName
// when two names repeat, and nil otherwise, by encoding/json.
func objectFault(data []byte, maxDepth int, names []string) error {
	if depth, err := nesting(data); !jsonUTF8(data) || !opens(data, '{') || err != nil || depth > maxDepth {
		return ErrMalformed
	}
	if len(slices.Compact(slices.Sorted(slices.Values(names)))) != len(names) {
		return ErrDuplicateName
	}
	return nil
}

func TestIterateObjectReportsMembersInOrder(t *testing.T) {
	doc := " \t{\"a\":\"x\" , \"b\\u0063\":[1,{\"n\":null}],\"\":{},\"caf\u00e9\":-1.5e3}\n"
	names, values := []string{}, []string{}
	err := IterateObject(doc, MaxDepth, func(name, value string) error {
		names, values = append(names, name), append(values, value)
		return nil
	})
	wantNames := []string{"a", "bc", "", cafe}
	wantValues := []string{quoted("x"), `[1,{"n":null}]`, emptyObject, "-1.5e3"}
	if err != nil || !slices.Equal(names, wantNames) || !slices.Equal(values, wantValues) {
		t.Fatalf("IterateObject = %q, %q, %v, want %q, %q", names, values, err, wantNames, wantValues)
	}
	if !aliases(values[1], doc, strings.Index(doc, "[1,")) || !aliases(names[0], doc, strings.Index(doc, "a")) {
		t.Fatalf("IterateObject copied the name %q or the value %q, want views of the input", names[0], values[1])
	}
}

func TestIterateObjectHonorsTheDepthBound(t *testing.T) {
	for _, tc := range []struct {
		doc      string
		maxDepth int
		want     error
	}{
		{emptyObject, 0, ErrMalformed}, {emptyObject, -1, ErrMalformed}, {emptyObject, 1, nil},
		{objectA1, 1, nil}, {`{"a":[]}`, 1, ErrMalformed}, {`{"a":{}}`, 1, ErrMalformed},
		{`{"a":[]}`, 2, nil}, {`{"a":[[]]}`, 2, ErrMalformed},
		{leveled(inlineLevels+1, func(int) bool { return true }), inlineLevels + 1, nil},
		{leveled(inlineLevels+1, func(int) bool { return true }), inlineLevels, ErrMalformed},
	} {
		if err := IterateObject(tc.doc, tc.maxDepth, func(_, _ string) error { return nil }); !is(err, tc.want) {
			t.Errorf("IterateObject(%.40q, %d) = %v, want %v", tc.doc, tc.maxDepth, err, tc.want)
		}
	}
}

func TestIterateObjectAnswersABoundBelowOneAtOnce(t *testing.T) {
	refuse := func(doc string) func() {
		return func() {
			if err := IterateObject(doc, 0, func(_, _ string) error { return nil }); !is(err, ErrMalformed) {
				t.Fatalf("IterateObject(%.12q, 0) = %v, want ErrMalformed", doc, err)
			}
		}
	}
	assertCost(t, refuse(emptyObject), refuse(emptyObject+strings.Repeat(" ", decidedSpaces)), 1)
}

func TestIterateObjectFindsTheMalformedTailAfterARepeat(t *testing.T) {
	for _, tc := range []struct {
		doc   string
		calls int
		want  error
	}{
		{`{"a":1,"a":2,"b":3}`, 1, ErrDuplicateName},
		{escapedRepeat, 1, ErrDuplicateName},
		{`{"x":{"a":1},"y":2,"x":3}`, 2, ErrDuplicateName},
		{`{"a":1,"a":2,`, 1, ErrMalformed},
		{`{"a":1,"a":2,"b":tru}`, 1, ErrMalformed},
		{"{\"a\":1,\"a\":2,\"b\":\"\xff\"}", 1, ErrMalformed},
		{`{"a":1,"a":2} x`, 1, ErrMalformed},
		{`{"a":1,"b":[1,]}`, 1, ErrMalformed},
		{`{"a":1,"a":2}`, 1, ErrDuplicateName},
		{`{"a":1,"a":2 , "b" : [3] }`, 1, ErrDuplicateName},
		{`{"a":1,"a":2,"\ud800":3}`, 1, ErrMalformed},
		{"{\"a\":1,\"a\":2,\"\xff\":3}", 1, ErrMalformed},
		{`{"a":1,"a":2,"b" 3}`, 1, ErrMalformed},
		{`{"a":1,"a":2,"b"`, 1, ErrMalformed},
		{`{"a":1,"a":2,3:3}`, 1, ErrMalformed},
		{`{"a":1,"a":2,"b":3,}`, 1, ErrMalformed},
		{`{"a":1,"a":2 "b":3}`, 1, ErrMalformed},
		{`{"a":1,"a":2,"b":"\udc00"}`, 1, ErrMalformed},
	} {
		calls := 0
		err := IterateObject(tc.doc, MaxDepth, func(_, _ string) error {
			calls++
			return nil
		})
		if !is(err, tc.want) || calls != tc.calls {
			t.Errorf("IterateObject(%q) = %v after %d calls, want %v after %d", tc.doc, err, calls, tc.want, tc.calls)
		}
	}
}

func TestIterateObjectReturnsTheCallbackErrorUnchanged(t *testing.T) {
	for _, doc := range []string{`{"a":1,"b":2}`, `{"a":1,"b":}`, repeatedA} {
		calls := 0
		err := IterateObject(doc, MaxDepth, func(_, _ string) error {
			calls++
			return errStop
		})
		if !is(err, errStop) || calls != 1 {
			t.Errorf("IterateObject(%q) = %v after %d calls, want errStop after 1", doc, err, calls)
		}
	}
}

func TestIterateObjectAllocs(t *testing.T) {
	inline := []byte(manyMembers(documentedInlineNames, -1))
	inlineDoc := string(inline)
	nested := []byte(`{"a":[1,{"b":"c"}],"d":{"e":[true,null]},"f":"g"}`)
	escaped := escapedObject()
	fn := func(_, _ []byte) error { return nil }
	assertAllocs(t, 0, func() { noError(t, IterateObject(inline, MaxDepth, fn)) })
	assertAllocs(t, 0, func() { noError(t, IterateObject(nested, MaxDepth, fn)) })
	assertAllocs(t, 0, func() {
		noError(t, IterateObject(inlineDoc, MaxDepth, func(_, _ string) error { return nil }))
	})
	assertAllocs(t, escapedChunks, func() { noError(t, IterateObject(escaped, MaxDepth, fn)) })
	spill := []byte(manyMembers(documentedInlineNames+1, -1))
	assertAllocs(t, spillAllocs, func() { noError(t, IterateObject(spill, MaxDepth, fn)) })
	// After a repeat no name is decoded, and one walker checks every value.
	afterRepeat := []byte(`{"a":1,"a":1,` + strings.TrimPrefix(string(escaped), "{"))
	assertAllocs(t, 0, func() {
		if err := IterateObject(afterRepeat, MaxDepth, fn); !is(err, ErrDuplicateName) {
			t.Fatalf("IterateObject after a repeat = %v, want ErrDuplicateName", err)
		}
	})
	fields := make([]string, documentedInlineNames)
	for i := range fields {
		fields[i] = `"k` + strconv.Itoa(i) + `":` + arrays(inlineLevels+1)
	}
	deep := []byte("{" + strings.Join(fields, ",") + "}")
	assertAllocs(t, 1, func() { noError(t, IterateObject(deep, MaxDepth, fn)) })
	refused := []byte(`{` + refusedLate() + `:1}`)
	assertAllocs(t, 0, func() {
		if err := IterateObject(refused, MaxDepth, fn); !is(err, ErrMalformed) {
			t.Fatalf("IterateObject(%.12q) = %v, want ErrMalformed", refused, err)
		}
	})
}

func TestIterateObjectChecksTheValuesAfterARepeatUnderItsBound(t *testing.T) {
	const bound = 3
	for _, tc := range []struct {
		doc  string
		want error
	}{
		{`{"a":1,"a":2,"b":[[]]}`, ErrDuplicateName},
		{`{"a":1,"a":2,"b":[[[]]]}`, ErrMalformed},
		{`{"a":[[[]]],"a":2}`, ErrMalformed},
	} {
		if err := IterateObject(tc.doc, bound, func(_, _ string) error { return nil }); !is(err, tc.want) {
			t.Errorf("IterateObject(%s, %d) = %v, want %v", tc.doc, bound, err, tc.want)
		}
	}
}

func FuzzIterateObject(f *testing.F) {
	for _, s := range []string{
		escapedRepeat, "{\"\xff\":1}", `{"x":{"x":1}}`, `{"a":1,"a":2,`, `{"a":[[[]]]}`,
		manyMembers(manyNames, documentedInlineNames), `[1]`, `{"a":"\ud800"}`,
	} {
		f.Add([]byte(s), uint8(seedNesting))
		f.Add([]byte(s), uint8(2))
	}
	f.Fuzz(func(t *testing.T, data []byte, depth uint8) {
		maxDepth := int(depth) - 1
		var gotNames []string
		var gotValues []json.RawMessage
		err := IterateObject(data, maxDepth, func(name, value []byte) error {
			gotNames, gotValues = append(gotNames, string(name)), append(gotValues, value)
			return nil
		})
		names, values := objectMembers(data)
		want := objectFault(data, maxDepth, names)
		if !is(err, want) {
			t.Fatalf("IterateObject(%q, %d) = %v, want %v", data, maxDepth, err, want)
		}
		if want == nil && (!slices.Equal(gotNames, names) || !slices.EqualFunc(gotValues, values, rawEqual)) {
			t.Fatalf("IterateObject(%q) = %q, %q; json.Decoder %q, %q", data, gotNames, gotValues, names, values)
		}
	})
}

func BenchmarkIterateObject(b *testing.B) {
	for _, bc := range []struct{ name, doc string }{
		{"Plain", fixtureObject},
		{"Nested", validityDocument},
		{"Names32", manyMembers(inlineNames, -1)},
	} {
		data := []byte(bc.doc)
		names, _, err := decoderMembers(data)
		noError(b, err)
		if err := objectFault(data, MaxDepth, names); err != nil {
			b.Fatalf("%s: encoding/json and the depth bound say %v, want nil", bc.name, err)
		}
		b.Run(bc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			for b.Loop() {
				if err := IterateObject(data, MaxDepth, func(_, _ []byte) error { return nil }); err != nil {
					b.Fatalf("IterateObject = %v, want nil", err)
				}
			}
		})
	}
}

// escapedMember is a member whose name holds escapes, which skipName checks and
// strictName decodes.
const escapedMember = `"caf\u00e9 cr\u00e8me":1`

// benchmarkName times name on escapedMember, whose name token ends at its colon.
func benchmarkName(b *testing.B, name func(data []byte, i int) (member, bool)) {
	b.Helper()
	data := []byte(escapedMember)
	colon := bytes.IndexByte(data, ':')
	if m, ok := name(data, 0); !ok || m != (member{nameEnd: colon, valueStart: colon + 1}) {
		b.Fatalf("the name of %s = %+v, %v, want its token to end at %d", data, m, ok, colon)
	}
	b.ReportAllocs()
	b.SetBytes(int64(colon))
	for b.Loop() {
		_, _ = name(data, 0)
	}
}

func BenchmarkSkipName(b *testing.B) {
	benchmarkName(b, skipName)
}

func BenchmarkSkipNameSharedForm(b *testing.B) {
	benchmarkName(b, func(data []byte, i int) (member, bool) {
		var a arena
		_, m, ok := a.strictName(data, i)
		return m, ok
	})
}

// errBadRequest is the caller's error of ExampleIterateObject.
var errBadRequest = errors.New("bad request")

func ExampleIterateObject() {
	const maxDepth = 32
	body := []byte(`{"user":"alice","count":2,"user":"mallory"}`)
	var (
		user  string
		count int64
	)
	err := IterateObject(body, maxDepth, func(name, value []byte) error {
		var ok bool
		switch string(name) {
		case "user":
			user, ok = DecodeString(value)
		case "count":
			count, ok = DecodeInt64(value)
		default:
			return nil
		}
		if !ok {
			return errBadRequest
		}
		return nil
	})
	fmt.Println(user, count, errors.Is(err, ErrDuplicateName), errors.Is(err, ErrMalformed))
	// Output: alice 2 true true
}

func TestIterateDocumentWalksOnlyWhatValidUTF8Accepts(t *testing.T) {
	cases := append(validityCases(), utf8Cases()...)
	for _, s := range objectMalformed() {
		cases = append(cases, validityCase{in: s})
	}
	for _, tc := range cases {
		data := []byte(tc.in)
		calls := 0
		err := IterateDocument(data, MaxDepth, func(_, _ []byte) error {
			calls++
			return nil
		})
		names, _ := objectMembers(data)
		if want := documentFault(data, MaxDepth); !is(err, want) || (want == nil && calls != len(names)) {
			t.Errorf("IterateDocument(%q) = %v after %d calls, want %v", tc.in, err, calls, want)
		}
	}
}

func TestIterateDocumentRefusesARepeatInAnyObject(t *testing.T) {
	deep := leveled(documentedNameSets+2, func(int) bool { return true })
	spill := manyMembers(documentedInlineNames+2, 0)
	for _, tc := range []struct {
		doc   string
		calls int
		want  error
	}{
		{`{"a":{"b":1,"b":2}}`, 0, ErrDuplicateName},
		{`{"a":1,"b":[{"c":1},{"c":1,"\u0063":2}]}`, 1, ErrDuplicateName},
		{`{"a":[[{"b":{"c":1,"c":1}}]],"d":1}`, 0, ErrDuplicateName},
		{`{"a":{"b":{"c":{"d":{"e":1,"e":2}}}}}`, 0, ErrDuplicateName},
		{`{"a":` + spill + `}`, 0, ErrDuplicateName},
		{`{"a":{"\u0078":1,"b":{"\u0079":1},"\u007a":2,"\u0078":3}}`, 0, ErrDuplicateName},
		{escapedRepeat, 1, ErrDuplicateName},
		{`{"a":{"b":1,"b":2,}}`, 0, ErrMalformed},
		{`{"a":{"b":1,"b":2},"c":tru}`, 0, ErrMalformed},
		{`{"a":{"b":1,"b":2}} x`, 0, ErrMalformed},
		{`{"a":{"a":1},"b":[{"x":1},{"x":2},{"x":{"y":1},"z":{"y":2}}]}`, 2, nil},
		{`{"a":{"x":{"y":1},"y":2}}`, 1, nil},
		{`{"a":[{"x":{"y":1},"y":2}]}`, 1, nil},
		{deep, 1, nil},
	} {
		calls := 0
		err := IterateDocument(tc.doc, MaxDepth, func(_, _ string) error {
			calls++
			return nil
		})
		if !is(err, tc.want) || calls != tc.calls {
			t.Errorf("IterateDocument(%.60q) = %v after %d calls, want %v after %d",
				tc.doc, err, calls, tc.want, tc.calls)
		}
	}
}

func TestIterateDocumentAllocs(t *testing.T) {
	fn := func(_, _ []byte) error { return nil }
	inlineSets := []byte(leveled(documentedNameSets+1, func(int) bool { return true }))
	heapSets := []byte(leveled(documentedNameSets+2, func(int) bool { return true }))
	assertAllocs(t, 0, func() { noError(t, IterateDocument(inlineSets, MaxDepth, fn)) })
	assertAllocs(t, 2, func() { noError(t, IterateDocument(heapSets, MaxDepth, fn)) })
}

func TestIterateDocumentAllocatesForTheOpenObjectsOnly(t *testing.T) {
	fn := func(_, _ []byte) error { return nil }
	for _, tc := range []struct {
		name, object string
		sizes        []int
		want         float64
	}{
		{"EscapedNames", `{"\u0078":0}`, []int{someSiblings, manySiblings}, 1},
		{"SpilledSets", manyMembers(documentedInlineNames+1, -1), []int{fewSiblings, someSiblings}, spillAllocs},
	} {
		for _, n := range tc.sizes {
			data := siblings(n, tc.object)
			t.Run(tc.name+strconv.Itoa(n), func(t *testing.T) {
				assertAllocs(t, tc.want, func() { noError(t, IterateDocument(data, MaxDepth, fn)) })
			})
		}
	}
}

// documentedDocumentRoom is the most room the first chunks of IterateDocument take
// beyond the names they hold, as the documentation states: 8 KiB.
const documentedDocumentRoom = 8 << 10

func TestIterateDocumentDecodesNestedNamesIntoChunksOfTheirOwn(t *testing.T) {
	fn := func(_, _ []byte) error { return nil }
	pad := `,"pad":"` + strings.Repeat("p", documentedRoom) + `"}`
	for _, tc := range []struct {
		name, doc string
		chunks    float64
		room      uint64
	}{
		{"MemberName", `{"\u0078":1` + pad, 1, documentedRoom},
		{"NestedName", `{"\u0078":{"\u0079":1}` + pad, 2, documentedDocumentRoom},
	} {
		data := []byte(tc.doc)
		t.Run(tc.name, func(t *testing.T) {
			walk := func() { noError(t, IterateDocument(data, MaxDepth, fn)) }
			assertAllocs(t, tc.chunks, walk)
			if raceEnabled() {
				return
			}
			_, allocated := costOf(func() {
				for range countRuns {
					walk()
				}
			})
			if got := allocated / countRuns; got != tc.room {
				t.Errorf("IterateDocument allocated %d bytes per call, want %d", got, tc.room)
			}
		})
	}
}

func FuzzIterateDocument(f *testing.F) {
	for _, s := range []string{
		escapedRepeat, `{"x":{"x":1}}`, `{"x":[{"y":1,"\u0079":2}]}`, `{"x":{"y":{"z":{"w":1,"w":2}}}}`,
		`{"a":{"b":1,"b":2},"c":tru}`, `{"a":[[[]]]}`, "{\"\xff\":1}", `{"a":{"x":{"y":1},"y":2}}`,
		`{"a":[{"x":{"y":1},"y":2}]}`, `{"\u0061":{"\u0062":{"\u0063":1},"\u0063":2},"\u0064":3}`,
	} {
		f.Add([]byte(s), uint8(seedNesting))
		f.Add([]byte(s), uint8(2))
	}
	f.Fuzz(func(t *testing.T, data []byte, depth uint8) {
		maxDepth := int(depth) - 1
		var gotNames, gotValues [][]byte
		err := IterateDocument(data, maxDepth, func(name, value []byte) error {
			gotNames, gotValues = append(gotNames, name), append(gotValues, value)
			return nil
		})
		names, values := objectMembers(data)
		want := documentFault(data, maxDepth)
		if !is(err, want) {
			t.Fatalf("IterateDocument(%q, %d) = %v, want %v", data, maxDepth, err, want)
		}
		kept := make([]string, len(gotNames))
		for k, name := range gotNames {
			kept[k] = string(name)
		}
		raw := make([]json.RawMessage, len(gotValues))
		for k, value := range gotValues {
			raw[k] = value
		}
		if want == nil && (!slices.Equal(kept, names) || !slices.EqualFunc(raw, values, rawEqual)) {
			t.Fatalf("IterateDocument(%q) kept %q, %q; json.Decoder %q, %q", data, kept, raw, names, values)
		}
	})
}

// documentFault returns what IterateDocument must return at maxDepth for data:
// ErrMalformed, then ErrDuplicateName when an object in it repeats a name, and nil
// otherwise, by encoding/json.
func documentFault(data []byte, maxDepth int) error {
	if err := objectFault(data, maxDepth, nil); err != nil {
		return err
	}
	return tokenFault(data, maxDepth)
}

func BenchmarkIterateDocument(b *testing.B) {
	for _, bc := range []struct{ name, doc string }{
		{"Flat", fixtureObject},
		{"Nested", valueDocument},
	} {
		data := []byte(bc.doc)
		if err := documentFault(data, MaxDepth); err != nil {
			b.Fatalf("%s: encoding/json and the depth bound say %v, want nil", bc.name, err)
		}
		b.Run(bc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			for b.Loop() {
				if err := IterateDocument(data, MaxDepth, func(_, _ []byte) error { return nil }); err != nil {
					b.Fatalf("IterateDocument = %v, want nil", err)
				}
			}
		})
	}
}

// namedToken is one call of fn by WalkTokens: the name and the token it gets.
type namedToken struct {
	name, token string
}

// tokensOf returns what WalkTokens hands fn for data at maxDepth, read once the walk
// returns, so a name that did not stay valid shows.
func tokensOf[T Text](data T, maxDepth int) ([]namedToken, error) {
	var calls [][2]T
	err := WalkTokens(data, maxDepth, func(name, token T) error {
		calls = append(calls, [2]T{name, token})
		return nil
	})
	got := make([]namedToken, 0, len(calls))
	for _, call := range calls {
		got = append(got, namedToken{string(call[0]), string(call[1])})
	}
	return got, err
}

// decoderTokens lists every scalar, string, opener and closer of data as json.Decoder
// reads it, as data holds it, with the decoded name of the member it starts.
func decoderTokens(data []byte) ([]namedToken, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var (
		out  []namedToken
		name string
		key  bool
		last int64
	)
	objects := []bool{}
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, fmt.Errorf("token: %w", err)
		}
		raw := bytes.TrimLeft(data[last:dec.InputOffset()], jsonSpace+",:")
		last = dec.InputOffset()
		if s, ok := tok.(string); ok && key {
			name, key = s, false
			continue
		}
		out, name = append(out, namedToken{name, string(raw)}), ""
		switch tok {
		case json.Delim('{'), json.Delim('['):
			objects = append(objects, tok == json.Delim('{'))
		case json.Delim('}'), json.Delim(']'):
			objects = objects[:len(objects)-1]
		}
		key = len(objects) > 0 && objects[len(objects)-1]
	}
}

// tokenFault returns what WalkTokens must return at maxDepth for data: ErrMalformed,
// then ErrDuplicateName when an object in it repeats a name, and nil otherwise, by
// encoding/json.
func tokenFault(data []byte, maxDepth int) error {
	if depth, err := nesting(data); !jsonUTF8(data) || err != nil || depth > maxDepth {
		return ErrMalformed
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	_, repeated, err := stdValue(dec)
	switch {
	case err != nil:
		return err
	case repeated:
		return ErrDuplicateName
	}
	return nil
}

func TestWalkTokensHandsOutEveryTokenInOrder(t *testing.T) {
	doc := " {\"k\":\"v\" , \"b\\u0063\":[1,{\"n\":null}],\"\":{},\"caf\u00e9\":-2.5e3,\"t\":[true,false]}\n"
	for _, tc := range []struct{ doc, want string }{
		{doc, `"":{ "k":"v" "bc":[ "":1 "":{ "n":null "":} "":] "":{ "":} ` +
			`"café":-2.5e3 "t":[ "":true "":false "":] "":}`},
		{` "a\nb" `, `"":"a\nb"`},
		{"[]", `"":[ "":]`},
		{"-0", `"":-0`},
	} {
		got, err := tokensOf(tc.doc, MaxDepth)
		bytesGot, bytesErr := tokensOf([]byte(tc.doc), MaxDepth)
		if err != nil || bytesErr != nil || rendered(got) != tc.want || rendered(bytesGot) != tc.want {
			t.Errorf("WalkTokens(%q) = %s, %v and of the bytes %s, %v, want %s, nil",
				tc.doc, rendered(got), err, rendered(bytesGot), bytesErr, tc.want)
		}
	}
}

// rendered returns tokens one space apart, each as its quoted name, a colon and the
// token.
func rendered(tokens []namedToken) string {
	var out strings.Builder
	for k, tok := range tokens {
		if k > 0 {
			out.WriteByte(' ')
		}
		fmt.Fprintf(&out, "%q:%s", tok.name, tok.token)
	}
	return out.String()
}

// seenTokens returns the tokens WalkTokens hands fn for doc, joined by spaces, and
// its result, with fn failing on the token stop.
func seenTokens(doc, stop string) (string, error) {
	var seen []string
	err := WalkTokens(doc, MaxDepth, func(_, token string) error {
		seen = append(seen, token)
		if token == stop {
			return errStop
		}
		return nil
	})
	return strings.Join(seen, " "), err
}

func TestWalkTokensKeepsEveryNameIntact(t *testing.T) {
	doc := []byte(`[{"\u0065":1},{"\u0066":{"\u0067":2}},{"\u0068":3}]`)
	got, err := tokensOf(doc, MaxDepth)
	var names []string
	for _, tok := range got {
		if tok.name != "" {
			names = append(names, tok.name)
		}
	}
	if joined := strings.Join(names, ""); err != nil || joined != "efgh" {
		t.Fatalf("WalkTokens(%s) named %q, %v once it returned, want efgh, nil", doc, joined, err)
	}
}

func TestWalkTokensRefusesWhatValidUTF8RefusesAndRepeatedNames(t *testing.T) {
	for _, tc := range []struct {
		doc, seen string
		want      error
	}{
		{`{"p":1,"p":2}`, "{ 1", ErrDuplicateName},
		{escapedRepeat, "{ 1", ErrDuplicateName},
		{`[{"q":1,"\u0071":2}]`, "[ { 1", ErrDuplicateName},
		{`{"a":{"b":{"c":{"d":1,"d":2}}}}`, "{ { { { 1", ErrDuplicateName},
		{`{"a":[{"b":1,"b":2}],"c":tru}`, "{ [ { 1", ErrMalformed},
		{`{"a":{"b":1,"b":2,}}`, "{ { 1", ErrMalformed},
		{`{"a":1,"a":2} x`, "{ 1", ErrMalformed},
		{`[1,]`, "[ 1", ErrMalformed},
		{"[\"\xc0\"]", "[", ErrMalformed},
		{`[{"\ud800":1}]`, "[ {", ErrMalformed},
		{`{"a":1}{}`, "{ 1 }", ErrMalformed},
		{`{"a":{"x":{"y":1},"y":2}}`, "{ { { 1 } 2 } }", nil},
		{`[{"x":1},{"x":2},{"x":{"x":3}}]`, "[ { 1 } { 2 } { { 3 } } ]", nil},
	} {
		seen, err := seenTokens(tc.doc, "")
		if !is(err, tc.want) || seen != tc.seen {
			t.Errorf("WalkTokens(%q) = %v after %s, want %v after %s", tc.doc, err, seen, tc.want, tc.seen)
		}
	}
}

func TestWalkTokensHonorsTheDepthBound(t *testing.T) {
	deep := leveled(seedNesting, func(level int) bool { return level%2 == 1 })
	for _, tc := range []struct {
		doc      string
		maxDepth int
		want     error
	}{
		{deep, seedNesting, nil},
		{deep, seedNesting - 1, ErrMalformed},
		{litNull, 0, nil},
		{emptyObject, 0, ErrMalformed},
		{litNull, -1, ErrMalformed},
	} {
		if _, err := tokensOf(tc.doc, tc.maxDepth); !is(err, tc.want) {
			t.Errorf("WalkTokens(%q, %d) = %v, want %v", tc.doc, tc.maxDepth, err, tc.want)
		}
	}
}

func TestWalkTokensAnswersABoundBelowZeroAtOnce(t *testing.T) {
	refuse := func(doc string) func() {
		return func() {
			if _, err := tokensOf(doc, -1); !is(err, ErrMalformed) {
				t.Fatalf("WalkTokens(%.12q, -1) = %v, want ErrMalformed", doc, err)
			}
		}
	}
	assertCost(t, refuse(emptyObject), refuse(emptyObject+strings.Repeat(" ", decidedSpaces)), 1)
}

func TestWalkTokensReturnsTheCallbackErrorUnchanged(t *testing.T) {
	if seen, err := seenTokens(`[1,{"a":2},3]`, "2"); !is(err, errStop) || seen != "[ 1 { 2" {
		t.Fatalf("WalkTokens = %v after %s, want errStop after [ 1 { 2", err, seen)
	}
}

func TestWalkTokensAllocs(t *testing.T) {
	fn := func(_, _ []byte) error { return nil }
	inlineSets := []byte(leveled(documentedNameSets, func(int) bool { return true }))
	heapSets := []byte(leveled(documentedNameSets+1, func(int) bool { return true }))
	escaped := []byte(`{"\u0078":1}`)
	assertAllocs(t, 0, func() { noError(t, WalkTokens(inlineSets, MaxDepth, fn)) })
	assertAllocs(t, 1, func() { noError(t, WalkTokens(heapSets, MaxDepth, fn)) })
	assertAllocs(t, escapedChunks, func() { noError(t, WalkTokens(escaped, MaxDepth, fn)) })
}

// fewSiblings is the smaller sibling count of the tests of walks that spill their
// name sets.
const fewSiblings = 2

func TestWalkTokensReusesTheSetOfAClosedObject(t *testing.T) {
	fn := func(_, _ []byte) error { return nil }
	for _, n := range []int{fewSiblings, someSiblings} {
		data := siblings(n, manyMembers(documentedInlineNames+1, -1))
		assertAllocs(t, spillAllocs, func() { noError(t, WalkTokens(data, MaxDepth, fn)) })
	}
}

func TestWalkTokensCostIsLinear(t *testing.T) {
	small, large := costRuns(t, levelsDocument, func(data []byte) error {
		return WalkTokens(data, MaxDepth, func(_, _ []byte) error { return nil })
	})
	assertCost(t, small, large, costScale)
}

func FuzzWalkTokens(f *testing.F) {
	for _, s := range []string{
		escapedRepeat, `{"y":{"y":1}}`, `[{"y":1,"\u0079":2}]`, `{"a":[{"b":1,"b":2}],"c":tru}`, `[[[]]]`,
		"[\"\xfe\"]", `{"a":[{"x":{"y":1},"y":2}]}`, `"\u0061"`, `-2.5e3`, `[{"\u0065":1},{"\u0066":2}]`,
	} {
		f.Add([]byte(s), uint8(seedNesting))
		f.Add([]byte(s), uint8(2))
	}
	f.Fuzz(func(t *testing.T, data []byte, depth uint8) {
		maxDepth := int(depth) - 1
		got, err := tokensOf(data, maxDepth)
		want := tokenFault(data, maxDepth)
		if !is(err, want) {
			t.Fatalf("WalkTokens(%q, %d) = %v, want %v", data, maxDepth, err, want)
		}
		if want != nil {
			return
		}
		expected, derr := decoderTokens(data)
		ofString, serr := tokensOf(string(data), maxDepth)
		if derr != nil || serr != nil || !slices.Equal(got, expected) || !slices.Equal(ofString, expected) {
			t.Fatalf("WalkTokens(%q) = %q and of the string %q, %v; json.Decoder %q, %v",
				data, got, ofString, serr, expected, derr)
		}
	})
}

func BenchmarkWalkTokens(b *testing.B) {
	for _, bc := range []struct{ name, doc string }{
		{"Flat", fixtureObject},
		{"Nested", valueDocument},
	} {
		data := []byte(bc.doc)
		want, derr := decoderTokens(data)
		if got, err := tokensOf(data, MaxDepth); err != nil || derr != nil || !slices.Equal(got, want) {
			b.Fatalf("%s: WalkTokens = %q, %v; json.Decoder %q, %v", bc.name, got, err, want, derr)
		}
		b.Run(bc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			for b.Loop() {
				if err := WalkTokens(data, MaxDepth, func(_, _ []byte) error { return nil }); err != nil {
					b.Fatalf("WalkTokens = %v, want nil", err)
				}
			}
		})
	}
}

func ExampleWalkTokens() {
	var out strings.Builder
	err := WalkTokens(`{"host":"db1","tags":["a","b"],"owner":{"team":"ops"}}`, MaxDepth,
		func(name, token string) error {
			if out.Len() > 0 {
				out.WriteByte(' ')
			}
			if name != "" {
				out.WriteString(name + ":")
			}
			out.WriteString(token)
			return nil
		})
	fmt.Println(out.String(), err)
	// Output: { host:"db1" tags:[ "a" "b" ] owner:{ team:"ops" } } <nil>
}

// arrayMalformed returns texts that no array walk accepts.
func arrayMalformed() []string {
	return []string{
		"", blank, objectA1, `[`, `[ `, `[1,2`, arrayGap, `["`, arrayTrail, `[,1]`, `[,]`,
		`[1] x`, `[1][2]`, `[1]]`, `["\q"]`, `[tru]`, `[01]`, `[[1]`, `[1,`, `[1;2]`, `[] x`,
	}
}

func TestIterateArrayReportsRawElements(t *testing.T) {
	var got []string
	err := IterateArray(`  [ 1, "two" ,true,null,{"k":"v"},[3] ]  `, func(elem string) error {
		got = append(got, elem)
		return nil
	})
	if want := `1|"two"|true|null|{"k":"v"}|[3]`; err != nil || strings.Join(got, "|") != want {
		t.Fatalf("IterateArray = %q, %v, want %s", got, err, want)
	}
	if err := IterateArray(" [ ] ", func(string) error { return errStop }); err != nil {
		t.Fatalf("IterateArray([]) = %v, want nil without a call", err)
	}
}

func TestIterateArrayRejectsMalformedArrays(t *testing.T) {
	for _, doc := range arrayMalformed() {
		if err := IterateArray([]byte(doc), func([]byte) error { return nil }); !is(err, ErrMalformed) {
			t.Errorf("IterateArray(%q) = %v, want ErrMalformed", doc, err)
		}
	}
}

func TestIterateArrayReturnsTheCallbackErrorUnchanged(t *testing.T) {
	var seen []string
	err := IterateArray(`[1,2,3,4,x`, func(elem string) error {
		seen = append(seen, elem)
		if elem == "3" {
			return errStop
		}
		return nil
	})
	if got := strings.Join(seen, ","); !is(err, errStop) || got != "1,2,3" {
		t.Fatalf("IterateArray = %v after %s, want errStop after 1,2,3", err, got)
	}
}

func TestIterateArrayViewsAreCapped(t *testing.T) {
	data := []byte(twoArray)
	noError(t, IterateArray(data, func(elem []byte) error {
		if !capped(elem) {
			t.Errorf("element %q has capacity %d, want %d", elem, cap(elem), len(elem))
		}
		_ = append(elem, '9')
		return nil
	}))
	if string(data) != twoArray {
		t.Fatalf("appending to an element rewrote the input to %s, want %s", data, twoArray)
	}
}

func TestIterateArrayAllocs(t *testing.T) {
	data := []byte(`["alpha","bravo",{"c":[1,2]},4,null]`)
	doc := string(data)
	assertAllocs(t, 0, func() {
		noError(t, IterateArray(data, func([]byte) error { return nil }))
		noError(t, IterateArray(doc, func(string) error { return nil }))
	})
}

func FuzzIterateArray(f *testing.F) {
	for _, s := range append([]string{`[1,2,3]`, `["a",{"b":[]}]`, `[[1,,2]]`, objectA1}, arrayMalformed()...) {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var got [][]byte
		err := IterateArray(data, func(elem []byte) error {
			got = append(got, elem)
			return nil
		})
		if items, want := shallowWalk(data, '['); !is(err, want) || !sameItems(nil, got, items) {
			t.Fatalf("IterateArray(%q) = %q, %v; the shallow grammar = %q, %v", data, got, err, items, want)
		}
		// encoding/json decodes null into a nil slice, which is no array.
		null := bytes.Equal(bytes.TrimSpace(data), []byte(litNull))
		var std []json.RawMessage
		if !json.Valid(data) || null || json.Unmarshal(data, &std) != nil {
			return
		}
		if err != nil || !sameValues(got, std) {
			t.Fatalf("IterateArray(%q) = %q, %v; encoding/json = %q", data, got, err, std)
		}
	})
}

func BenchmarkIterateArray(b *testing.B) {
	data := []byte(`[1,"hello",true,null,{"k":"v"},[1,2,3]]`)
	var want []json.RawMessage
	noError(b, json.Unmarshal(data, &want))
	var got [][]byte
	err := IterateArray(data, func(elem []byte) error {
		got = append(got, elem)
		return nil
	})
	if err != nil || !sameValues(got, want) {
		b.Fatalf("IterateArray = %q, %v; encoding/json = %q", got, err, want)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		if err := IterateArray(data, func([]byte) error { return nil }); err != nil {
			b.Fatalf("IterateArray = %v, want nil", err)
		}
	}
}

// errFound is the sentinel ExampleIterateArray stops its search with, and the
// error of a lookup that finds what the cost tests look for in vain.
var errFound = errors.New("found")

func ExampleIterateArray() {
	err := IterateArray(`[{"id":1},{"id":2,"hit":true},{"id":3}]`, func(elem string) error {
		if v, ok := FindMember(elem, "hit"); ok && v == litTrue {
			return errFound
		}
		return nil
	})
	fmt.Println(errors.Is(err, errFound))
	// Output: true
}

// stringArray collects what IterateStringArray reports.
func stringArray[T Text](data T) ([]T, error) {
	got := []T{}
	err := IterateStringArray(data, func(s T) error {
		got = append(got, s)
		return nil
	})
	return got, err
}

func TestIterateStringArrayDecodesElements(t *testing.T) {
	got, err := stringArray(` ["p\"q", "caf\u00e9","\ud83d\ude80","","\ud800", "plain"] `)
	want := []string{`p"q`, cafe, "🚀", "", "\uFFFD", plainText}
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("IterateStringArray = %q, %v, want %q", got, err, want)
	}
	if got, err := stringArray(emptyArray); err != nil || len(got) != 0 {
		t.Fatalf("IterateStringArray([]) = %q, %v, want nothing", got, err)
	}
}

func TestIterateStringArrayRejectsNonStringElements(t *testing.T) {
	nonStrings := []string{`["a",1]`, `[null]`, `[["a"]]`, `["a",{"b":"c"}]`, `["\x"]`}
	for _, doc := range append(nonStrings, arrayMalformed()...) {
		if _, err := stringArray(doc); !is(err, ErrMalformed) {
			t.Errorf("IterateStringArray(%q) = %v, want ErrMalformed", doc, err)
		}
	}
	calls := 0
	err := IterateStringArray(`["a","b",3]`, func(string) error {
		calls++
		return errStop
	})
	if !is(err, errStop) || calls != 1 {
		t.Errorf("IterateStringArray = %v after %d calls, want errStop after 1", err, calls)
	}
	calls = 0
	err = IterateStringArray(`[1,"a"]`, func(string) error {
		calls++
		return nil
	})
	if !is(err, ErrMalformed) || calls != 0 {
		t.Errorf("IterateStringArray([1,\"a\"]) = %v after %d calls, want ErrMalformed first", err, calls)
	}
}

func TestIterateStringArrayKeepsEveryElementIntact(t *testing.T) {
	doc := `["x\ny","plain","` + strings.Repeat("z", longRun) + `\t","\u0041"]`
	got, err := stringArray(doc)
	want := []string{"x\ny", plainText, strings.Repeat("z", longRun) + "\t", "A"}
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("IterateStringArray = %q, %v, want %q", got, err, want)
	}
	if !aliases(got[1], doc, strings.Index(doc, plainText)) {
		t.Fatalf("the clean element %q is a copy, want a view of the input", got[1])
	}
	data := []byte(doc)
	views, err := stringArray(data)
	noError(t, err)
	for _, v := range views {
		_ = append(v, "OVERWRITE"...)
	}
	for i, v := range views {
		if string(v) != want[i] || string(data) != doc {
			t.Fatalf("an append rewrote element %d to %q or the input, want %q and %s", i, v, want[i], doc)
		}
	}
}

func TestIterateStringArrayAllocs(t *testing.T) {
	data := []byte(`["alpha","bravo","charlie","delta","echo"]`)
	escaped := []byte(`["al\npha","br\u00e1vo","charlie"]`)
	fn := func([]byte) error { return nil }
	assertAllocs(t, 0, func() { noError(t, IterateStringArray(data, fn)) })
	assertAllocs(t, 1, func() { noError(t, IterateStringArray(escaped, fn)) })
	many := escapedArray()
	assertAllocs(t, escapedChunks, func() { noError(t, IterateStringArray(many, fn)) })
}

// A long array of the escapedTokens and the arena chunks its 15000 decoded bytes
// take: 4 KiB, then 8 KiB and 16 KiB, each twice the last.
const (
	longTokens = 5000
	longChunks = 3
)

func TestIterateStringArrayDecodesALongDocumentIntoDoublingChunks(t *testing.T) {
	tokens := escapedTokens()
	var sb strings.Builder
	sb.WriteByte('[')
	for k := range longTokens {
		if k > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(tokens[k%len(tokens)])
	}
	sb.WriteByte(']')
	data := []byte(sb.String())
	assertAllocs(t, longChunks, func() { noError(t, IterateStringArray(data, func([]byte) error { return nil })) })
}

func FuzzIterateStringArray(f *testing.F) {
	for _, s := range []string{
		`["a","b"]`, `["\u00e9",""]`, `["a",1]`, `[null]`, "[\"\xff\"]", `["a",[}]`, objectA1, quotedHello,
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		got, err := stringArray(string(data))
		items, want := shallowWalk(data, '[')
		var std []string
		for _, item := range items {
			var s string
			if !opens(item.value, '"') {
				want = ErrMalformed
				break
			}
			noError(t, json.Unmarshal(item.value, &s))
			std = append(std, s)
		}
		if !is(err, want) || !slices.Equal(got, std) {
			t.Fatalf("IterateStringArray(%q) = %q, %v; the shallow grammar and encoding/json = %q, %v",
				data, got, err, std, want)
		}
	})
}

func BenchmarkIterateStringArray(b *testing.B) {
	var sb strings.Builder
	sb.WriteByte('[')
	for i := range manyNames {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(`"id-` + strconv.Itoa(i) + `-item"`)
	}
	sb.WriteByte(']')
	data := []byte(sb.String())
	var want []string
	noError(b, json.Unmarshal(data, &want))
	var got []string
	noError(b, IterateStringArray(data, func(s []byte) error { got = append(got, string(s)); return nil }))
	if !slices.Equal(got, want) {
		b.Fatalf("IterateStringArray decoded %q, want %q", got, want)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		if err := IterateStringArray(data, func([]byte) error { return nil }); err != nil {
			b.Fatalf("IterateStringArray = %v, want nil", err)
		}
	}
}

func ExampleIterateStringArray() {
	var tags []string
	err := IterateStringArray(`["api","caf\u00e9"]`, func(s string) error {
		tags = append(tags, s)
		return nil
	})
	fmt.Println(tags, err)
	// Output: [api café] <nil>
}

func TestFindMemberMatchesDecodedNames(t *testing.T) {
	doc := []byte(`{"simple":1,"quoted\"name":2,"slash\\path":3,"tabbed\tkey":4,"caf\u00e9":5,` +
		`"\ud83d\ude80":6,"\u0041":7,"\u00FF":8,"\ud800":9,"` + "\xff" + `x":10,"a":11,"a":12}`)
	for _, tc := range []struct{ key, want string }{
		{"simple", "1"},
		{"quoted\"name", "2"},
		{"slash\\path", "3"},
		{"tabbed\tkey", "4"},
		{cafe, "5"},
		{"🚀", "6"},
		{"A", "7"},
		{"\u00ff", "8"},
		{"\uFFFD", "9"},
		{"\uFFFDx", "10"},
		{"a", "11"},
	} {
		if got, ok := FindMember(doc, tc.key); !ok || string(got) != tc.want {
			t.Errorf("FindMember(%q) = %s, %v, want %s", tc.key, got, ok, tc.want)
		}
	}
	for _, key := range []string{`quoted\"name`, "quoted\"na", "B", "x", "\xffx", "\xed\xa0\x80", "simplex", ""} {
		if got, ok := FindMember(doc, key); !sameView(got, ok, nil, false) {
			t.Errorf("FindMember(%q) = %s, %v, want nil, false", key, got, ok)
		}
	}
}

func TestFindMemberStopsAtTheMatch(t *testing.T) {
	for _, tc := range []struct{ doc, key, want string }{
		{`{"first":7,}`, "first", "7"},
		{`{"x":10,"target":"found","z":30}`, "target", `"found"`},
		{` {"k" : [1] } `, "k", oneArray},
	} {
		if got, ok := FindMember(tc.doc, tc.key); !ok || got != tc.want {
			t.Errorf("FindMember(%q, %q) = %q, %v, want %q", tc.doc, tc.key, got, ok, tc.want)
		}
	}
	for _, doc := range append([]string{emptyObject, objectA1}, objectMalformed()...) {
		if got, ok := FindMember(doc, "b"); !sameScalar(got, ok, "", false) {
			t.Errorf("FindMember(%q, b) = %q, %v, want \"\", false", doc, got, ok)
		}
	}
	for _, doc := range []string{`{x":1}`, `{"\x":1}`, "{\"\x01\":1}", "{\"\t:1,\"\":2}"} {
		if got, ok := FindMember(doc, ""); !sameScalar(got, ok, "", false) {
			t.Errorf("FindMember(%q, \"\") = %q, %v, want \"\", false for a malformed name", doc, got, ok)
		}
	}
	data := []byte(`{"k":[1,2]}`)
	if v, _ := FindMember(data, "k"); !capped(v) {
		t.Errorf("FindMember view has capacity %d, want %d", cap(v), len(v))
	}
}

func TestFindMemberAllocs(t *testing.T) {
	data := []byte(`{"facility":23,"severity":3,"hostname":"FW01","app\u005fname":"utm"}`)
	doc := string(data)
	assertAllocs(t, 0, func() {
		_, _ = FindMember(data, "app_name")
		_, _ = FindMember(doc, "hostname")
	})
}

// firstMatchOracle returns the raw value of the first member named key of the
// object in data, reading it with json.Decoder.
func firstMatchOracle(data []byte, key string) (value json.RawMessage, found bool, err error) {
	names, values, err := decoderMembers(data)
	if err != nil {
		return nil, false, err
	}
	if i := slices.Index(names, key); i >= 0 {
		return values[i], true, nil
	}
	return nil, false, nil
}

func FuzzFindMember(f *testing.F) {
	f.Add([]byte(`{"k":1,"j":2}`), "k")
	f.Add([]byte(`{"hostname":"FW01"}`), "hostname")
	f.Add([]byte(`{"caf\u00e9":1}`), cafe)
	f.Add([]byte("{\"\xff\":1}"), "\uFFFD")
	f.Add([]byte(emptyObject), "missing")
	f.Add([]byte(`{"a":[1,,2],"q":3,`), "q")
	f.Add([]byte(`{"a":1,"q"}`), "q")
	f.Add([]byte(`[{"m":1}]`), "m")
	f.Add([]byte(quotedHello), "hello")
	f.Fuzz(func(t *testing.T, data []byte, key string) {
		got, ok := FindMember(data, key)
		items, walkErr := shallowWalk(data, '{')
		k := slices.IndexFunc(items, func(item shallowItem) bool { return stdName(t, item) == key })
		var want []byte
		if k >= 0 {
			want = items[k].value
		}
		if !sameView(got, ok, want, k >= 0) {
			t.Fatalf("FindMember(%q, %q) = %s, %v; the shallow grammar has it at item %d of %q, then %v",
				data, key, got, ok, k, items, walkErr)
		}
		if !ok || !json.Valid(data) {
			return
		}
		want, found, err := firstMatchOracle(data, key)
		if err != nil || !found || !bytes.Equal(got, bytes.TrimSpace(want)) {
			t.Fatalf("FindMember(%q, %q) = %s, %v; json.Decoder = %s, %v, %v", data, key, got, ok, want, found, err)
		}
	})
}

func BenchmarkFindMember(b *testing.B) {
	data := []byte(`{"facility":23,"severity":3,"hostname":"FW01","app_name":"utm","source":"10.0.0.1"}`)
	if got, ok := FindMember(data, "source"); string(got) != `"10.0.0.1"` || !ok {
		b.Fatalf("FindMember(%s, source) = %s, %v, want \"10.0.0.1\", true", data, got, ok)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		_, _ = FindMember(data, "source")
	}
}

func ExampleFindMember() {
	data := []byte(`{"hostname":"fw01","severity":4,"message":"ok"}`)
	if raw, ok := FindMember(data, "severity"); ok {
		severity, _ := DecodeInt64(raw)
		fmt.Println(severity)
	}
	// Output: 4
}

func TestFindElementReturnsTheElementAtIndex(t *testing.T) {
	elements := []string{"10", `"b"`, `{"c":[1]}`}
	doc := " [ " + strings.Join(elements, " , ") + " ] "
	for i, want := range elements {
		if got, ok := FindElement(doc, i); got != want || !ok {
			t.Errorf("FindElement(%q, %d) = %q, %v, want %q", doc, i, got, ok, want)
		}
	}
	for _, index := range []int{len(elements), -1} {
		if got, ok := FindElement(doc, index); got != "" || ok {
			t.Errorf("FindElement(%q, %d) = %q, %v, want nothing", doc, index, got, ok)
		}
	}
	if v, _ := FindElement([]byte(twoArray), 0); !capped(v) {
		t.Errorf("FindElement view has capacity %d, want %d", cap(v), len(v))
	}
}

func TestFindElementStopsAtItsElement(t *testing.T) {
	for _, doc := range []string{"", `{"0":1}`, `[,1]`, `"[1]"`, `[`, emptyArray, arrayGap} {
		if got, ok := FindElement(doc, 1); !sameScalar(got, ok, "", false) {
			t.Errorf("FindElement(%q, 1) = %q, %v, want \"\", false", doc, got, ok)
		}
	}
	if got, ok := FindElement(`[1,2,]`, 1); !ok || got != "2" {
		t.Errorf("FindElement([1,2,], 1) = %q, %v, want 2, true", got, ok)
	}
}

func TestFindElementAnswersANegativeIndexAtOnce(t *testing.T) {
	refuse := func(doc string) func() {
		return func() {
			if v, ok := FindElement(doc, -1); ok || v != "" {
				t.Fatalf("FindElement(%.12q, -1) = %q, %v, want \"\", false", doc, v, ok)
			}
		}
	}
	assertCost(t, refuse(`[1]`), refuse(`[1]`+strings.Repeat(" ", decidedSpaces)), 1)
}

func TestFindElementAllocs(t *testing.T) {
	data := []byte(`[1,"two",{"three":3},[4]]`)
	assertAllocs(t, 0, func() { _, _ = FindElement(data, 3) })
}

func FuzzFindElement(f *testing.F) {
	f.Add([]byte(`[1,"two",{"3":3}]`), 2)
	f.Add([]byte(emptyArray), 0)
	f.Add([]byte(oneArray), -1)
	f.Add([]byte(`[1,2,]`), 1)
	f.Add([]byte(`[[1,,2],3`), 1)
	f.Add([]byte(objectA1), 0)
	f.Fuzz(func(t *testing.T, data []byte, index int) {
		got, ok := FindElement(data, index)
		items, walkErr := shallowWalk(data, '[')
		inRange := index >= 0 && index < len(items)
		var want []byte
		if inRange {
			want = items[index].value
		}
		if !sameView(got, ok, want, inRange) {
			t.Fatalf("FindElement(%q, %d) = %s, %v; the shallow grammar has %d elements, then %v",
				data, index, got, ok, len(items), walkErr)
		}
	})
}

func BenchmarkFindElement(b *testing.B) {
	elements := []string{`{"a":1}`, `{"b":2}`, `{"c":3}`, `{"d":4}`, `{"e":5}`}
	data := []byte("[" + strings.Join(elements, ",") + "]")
	if got, ok := FindElement(data, len(elements)-1); string(got) != elements[len(elements)-1] || !ok {
		b.Fatalf("FindElement(%s, %d) = %s, %v, want %s, true", data, len(elements)-1, got, ok,
			elements[len(elements)-1])
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		_, _ = FindElement(data, len(elements)-1)
	}
}

func ExampleFindElement() {
	second, ok := FindElement(`["a","b","c"]`, 1)
	fmt.Println(second, ok)
	// Output: "b" true
}

// costRuns returns runs of walk over doc(costLevels) and doc(costScale*costLevels)
// that fail t when walk does.
func costRuns(t *testing.T, doc func(n int) []byte, walk func(data []byte) error) (small, large func()) {
	t.Helper()
	run := func(data []byte) func() {
		return func() { noError(t, walk(data)) }
	}
	return run(doc(costLevels)), run(doc(costScale * costLevels))
}

// membersPerLevel scales membersObject, so both documents of a cost test hold more
// names than a name set keeps inline and its memory grows with them.
const membersPerLevel = 8

// membersObject returns an object of n*membersPerLevel members "m<k>", each holding
// a string of levelBody.
func membersObject(n int) []byte {
	var doc strings.Builder
	doc.WriteByte('{')
	for k := range n * membersPerLevel {
		if k > 0 {
			doc.WriteByte(',')
		}
		fmt.Fprintf(&doc, `"m%d":"%s"`, k, levelBody())
	}
	doc.WriteByte('}')
	return []byte(doc.String())
}

// stringsArray returns an array of n strings, each of levelBody.
func stringsArray(n int) []byte {
	text := quoted(levelBody())
	return []byte("[" + strings.Repeat(text+",", n-1) + text + "]")
}

func TestIterateFieldsCostIsLinear(t *testing.T) {
	small, large := costRuns(t, membersObject, func(data []byte) error {
		return IterateFields(data, func(_, _ []byte) error { return nil })
	})
	assertCost(t, small, large, costScale)
}

func TestIterateMembersCostIsLinear(t *testing.T) {
	small, large := costRuns(t, membersObject, func(data []byte) error {
		return IterateMembers(data, func(_, _ []byte) error { return nil })
	})
	assertCost(t, small, large, costScale)
}

func TestIterateObjectCostIsLinear(t *testing.T) {
	small, large := costRuns(t, levelsDocument, func(data []byte) error {
		return IterateObject(data, MaxDepth, func(_, _ []byte) error { return nil })
	})
	assertCost(t, small, large, costScale)
}

func TestIterateDocumentCostIsLinear(t *testing.T) {
	small, large := costRuns(t, levelsDocument, func(data []byte) error {
		return IterateDocument(data, MaxDepth, func(_, _ []byte) error { return nil })
	})
	assertCost(t, small, large, costScale)
}

func TestIterateArrayCostIsLinear(t *testing.T) {
	small, large := costRuns(t, stringsArray, func(data []byte) error {
		return IterateArray(data, func([]byte) error { return nil })
	})
	assertCost(t, small, large, costScale)
}

func TestIterateStringArrayCostIsLinear(t *testing.T) {
	small, large := costRuns(t, stringsArray, func(data []byte) error {
		return IterateStringArray(data, func([]byte) error { return nil })
	})
	assertCost(t, small, large, costScale)
}

func TestFindMemberCostIsLinear(t *testing.T) {
	small, large := costRuns(t, membersObject, func(data []byte) error {
		if v, ok := FindMember(data, "missing"); ok || v != nil {
			return errFound
		}
		return nil
	})
	assertCost(t, small, large, costScale)
}

func TestFindElementCostIsLinear(t *testing.T) {
	small, large := costRuns(t, stringsArray, func(data []byte) error {
		if v, ok := FindElement(data, costScale*costLevels); ok || v != nil {
			return errFound
		}
		return nil
	})
	assertCost(t, small, large, costScale)
}
