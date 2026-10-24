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

// Texts the cursor tests read; unread stands for the raw value of a Cursor whose Raw
// fails.
const (
	unread        = "!"
	trailer       = " x"
	cutArray      = "[1,"
	unclosedArray = "[1"
	openMember    = `{"a":`
	cutMember     = `{"a":1`
	bracketsOnly  = `[{]`
)

// toolHits is how many records the large tool response carries.
const toolHits = 100

// zeroCursor reports whether c is the zero Cursor.
func zeroCursor[T Text](c Cursor[T]) bool {
	return c.data == nil && c.at == 0
}

// rawOf returns the raw value at c, or unread when Raw fails.
func rawOf(c Cursor[string]) string {
	if raw, ok := c.Raw(); ok {
		return raw
	}
	return unread
}

// sawItems is what a walk handed fn, in order: the names, the raw values, unread where
// Raw fails at a Cursor, and the walk's error.
type sawItems struct {
	names, raws []string
	err         error
}

// cursorArray runs IterateArray of the Cursor at doc.
func cursorArray(doc string) sawItems {
	var s sawItems
	s.err = CursorOf(doc).IterateArray(func(elem Cursor[string]) error {
		s.raws = append(s.raws, rawOf(elem))
		return nil
	})
	return s
}

// cursorMembers runs IterateMembers of the Cursor at doc.
func cursorMembers(doc string) sawItems {
	var s sawItems
	s.err = CursorOf(doc).IterateMembers(func(name string, value Cursor[string]) error {
		s.names, s.raws = append(s.names, name), append(s.raws, rawOf(value))
		return nil
	})
	return s
}

func TestCursorOfStartsAtTheValuePastWhitespace(t *testing.T) {
	for _, doc := range []string{objectA1, " \t\r\n" + objectA1, " " + objectA1 + trailer} {
		c := CursorOf(doc)
		if got, ok := c.Raw(); got != objectA1 || !ok || c.Kind() != KindObject {
			t.Errorf("CursorOf(%q) Raw, Kind = %q, %v, %v, want %q, true, KindObject", doc, got, ok,
				c.Kind(), objectA1)
		}
	}
}

// noValueCursors returns the zero Cursor and Cursors at blank documents.
func noValueCursors() []Cursor[string] {
	return []Cursor[string]{{}, CursorOf(""), CursorOf(blank)}
}

func TestCursorHoldingNoValueIteratesNothing(t *testing.T) {
	for _, c := range noValueCursors() {
		calls := 0
		arrayErr := c.IterateArray(func(Cursor[string]) error { calls++; return nil })
		membersErr := c.IterateMembers(func(string, Cursor[string]) error { calls++; return nil })
		if !is(arrayErr, ErrMalformed) || !is(membersErr, ErrMalformed) || calls != 0 {
			t.Errorf("%+v IterateArray, IterateMembers = %v, %v after %d calls, want ErrMalformed twice "+
				"after 0", c, arrayErr, membersErr, calls)
		}
	}
}

func TestCursorHoldingNoValueReadsNothing(t *testing.T) {
	for _, c := range noValueCursors() {
		m, mOK := c.FindMember("")
		e, eOK := c.FindElement(0)
		if mOK || eOK || !zeroCursor(m) || !zeroCursor(e) {
			t.Errorf("%+v FindMember, FindElement = %+v, %v, %+v, %v, want the zero Cursor and false twice",
				c, m, mOK, e, eOK)
		}
		if got, ok := c.Raw(); got != "" || ok || c.Kind() != KindInvalid {
			t.Errorf("%+v Raw, Kind = %q, %v, %v, want \"\", false, KindInvalid", c, got, ok, c.Kind())
		}
	}
}

func TestCursorKindReadsOnlyTheFirstByte(t *testing.T) {
	for doc, want := range map[string]Kind{
		openMember: KindObject, cutArray: KindArray, `"x`: KindString, `n`: KindNull, `tx`: KindBool,
		`fx`: KindBool, `-x`: KindNumber, `9x`: KindNumber, `x`: KindInvalid, `}`: KindInvalid,
	} {
		if got := CursorOf(" " + doc).Kind(); got != want {
			t.Errorf("CursorOf(%q).Kind() = %v, want %v", " "+doc, got, want)
		}
	}
}

func TestCursorFindMemberDescendsIntoNestedValues(t *testing.T) {
	doc := `{"skip":{"x":"}"},"result":{"content":[{"text":"a\nb"}],"isError":false}}`
	result, _ := CursorOf(doc).FindMember("result")
	content, _ := result.FindMember("content")
	if got, want := rawOf(content), `[{"text":"a\nb"}]`; got != want {
		t.Fatalf("FindMember(result).FindMember(content) reads %q, want %q", got, want)
	}
	first, _ := content.FindElement(0)
	text, ok := first.FindMember("text")
	if got := rawOf(text); got != `"a\nb"` || !ok || text.Kind() != KindString {
		t.Errorf("FindElement(0).FindMember(text) reads %q, %v, %v, want %q, true, KindString", got, ok,
			text.Kind(), `"a\nb"`)
	}
}

func TestCursorFindMemberTakesTheFirstOfRepeatedNames(t *testing.T) {
	if m, ok := CursorOf(repeatedA).FindMember("a"); rawOf(m) != "1" || !ok {
		t.Errorf("CursorOf(%q).FindMember(a) reads %q, %v, want 1, true", repeatedA, rawOf(m), ok)
	}
}

func TestCursorFindMemberMatchesTheDecodedName(t *testing.T) {
	doc := "{\"caf\\u00e9\":1,\"b\\\"\":2,\"\xff\":3}"
	for name, want := range map[string]string{cafe: "1", `b"`: "2", replacement: "3", "\xff": unread} {
		if m, ok := CursorOf(doc).FindMember(name); rawOf(m) != want || ok != (want != unread) {
			t.Errorf("CursorOf(%q).FindMember(%q) reads %q, %v, want %q", doc, name, rawOf(m), ok, want)
		}
	}
}

func TestCursorFindMemberReadsNothingPastTheMatch(t *testing.T) {
	for _, doc := range []string{`{"a":{"b":2},"c":`, `{"a":{"b":2,`, `{"a":{"b":2}} x`, `{"a":{"b":2 x`} {
		a, _ := CursorOf(doc).FindMember("a")
		if b, ok := a.FindMember("b"); rawOf(b) != "2" || !ok {
			t.Errorf("CursorOf(%q).FindMember(a).FindMember(b) reads %q, %v, want 2, true", doc, rawOf(b), ok)
		}
	}
	for _, doc := range []string{`{"a":"x`, `{"a":tru`, `{"a":[`, `{"a":}`} {
		if m, ok := CursorOf(doc).FindMember("a"); rawOf(m) != unread || !ok {
			t.Errorf("CursorOf(%q).FindMember(a) reads %q, %v, want a Cursor whose Raw fails, true", doc,
				rawOf(m), ok)
		}
	}
}

func TestCursorFindMemberLeavesTheValueItFindsUnread(t *testing.T) {
	find := func(doc string) func() {
		return func() {
			if m, ok := CursorOf(doc).FindMember("a"); m.Kind() != KindString || !ok {
				t.Fatalf("CursorOf(%.12q).FindMember(a) = %v, %v, want KindString, true", doc, m.Kind(), ok)
			}
		}
	}
	assertCost(t, find(`{"a":"x"}`), find(`{"a":"`+strings.Repeat("x", decidedSpaces)+`"}`), 1)
}

func TestCursorFindMemberRefusesAFaultBeforeTheMatch(t *testing.T) {
	docs := []string{"", emptyArray, `"a"`, `{`, `{"x":}`, `{"x" 1,"a":1}`, `{"x":1 "a":1}`, `{"x":1}`,
		`{x:1,"a":1}`, `{"x":[1,"a":1}`, emptyObject, `{"x":1,}`, "{\"x\n\":1,\"a\":1}", openMember, `{"a": `}
	for _, doc := range docs {
		if m, ok := CursorOf(doc).FindMember("a"); ok || !zeroCursor(m) {
			t.Errorf("CursorOf(%q).FindMember(a) = %+v, %v, want the zero Cursor, false", doc, m, ok)
		}
	}
}

func TestCursorFindElementReturnsTheElementAtIndex(t *testing.T) {
	elements := []string{"10", `"b"`, `{"c":[1]}`}
	doc := " [ " + strings.Join(elements, " , ") + " ] x"
	for i, want := range elements {
		if e, ok := CursorOf(doc).FindElement(i); rawOf(e) != want || !ok {
			t.Errorf("CursorOf(%q).FindElement(%d) reads %q, %v, want %q, true", doc, i, rawOf(e), ok, want)
		}
	}
	for _, index := range []int{len(elements), -1} {
		if e, ok := CursorOf(doc).FindElement(index); ok || !zeroCursor(e) {
			t.Errorf("CursorOf(%q).FindElement(%d) = %+v, %v, want the zero Cursor, false", doc, index, e, ok)
		}
	}
	if e, ok := CursorOf(`[1,[`).FindElement(1); rawOf(e) != unread || !ok || e.Kind() != KindArray {
		t.Errorf("CursorOf([1,[).FindElement(1) reads %q, %v, %v, want a Cursor at the opener, true", rawOf(e),
			ok, e.Kind())
	}
}

func TestCursorFindElementRefusesAFaultBeforeTheIndex(t *testing.T) {
	for _, doc := range []string{arrayGap, `[:1]`, `{"1":1}`, cutArray, `[1, `, unclosedArray, `[x,1]`} {
		if e, ok := CursorOf(doc).FindElement(1); ok || !zeroCursor(e) {
			t.Errorf("CursorOf(%q).FindElement(1) = %+v, %v, want the zero Cursor, false", doc, e, ok)
		}
	}
}

func TestCursorFindElementAnswersANegativeIndexAtOnce(t *testing.T) {
	refuse := func(doc string) func() {
		return func() {
			if _, ok := CursorOf(doc).FindElement(-1); ok {
				t.Fatalf("CursorOf(%.12q).FindElement(-1) ok = true, want false", doc)
			}
		}
	}
	assertCost(t, refuse(oneArray), refuse("[1"+strings.Repeat(" ", decidedSpaces)+"]"), 1)
}

func TestCursorRawChecksScalarsAndCountsBrackets(t *testing.T) {
	for doc, want := range map[string]string{
		bracketsOnly: bracketsOnly, `1 x`: "1", `"a\"b"]`: `"a\"b"`, `[1 2] ,`: arrayGap, `null}`: litNull,
		unclosedArray: unread, `"cut`: unread, `fals`: unread, `-`: unread, cutMember: unread,
		`"\x"`: unread, `x`: unread,
	} {
		if got := rawOf(CursorOf(doc)); got != want {
			t.Errorf("CursorOf(%q).Raw() reads %q, want %q", doc, got, want)
		}
	}
	if got, ok := CursorOf([]byte(twoArray + " ")).Raw(); !capped(got) || !ok {
		t.Errorf("Raw view has capacity %d for length %d, want them equal", cap(got), len(got))
	}
}

func TestCursorIterateArrayVisitsEachElementInOrder(t *testing.T) {
	doc := ` [ 1 , "two" , {"t":[3]} , [] ] x`
	got := cursorArray(doc)
	if want := []string{"1", `"two"`, `{"t":[3]}`, emptyArray}; got.err != nil || !slices.Equal(got.raws, want) {
		t.Errorf("CursorOf(%q).IterateArray saw %q, %v, want %q, nil", doc, got.raws, got.err, want)
	}
}

func TestCursorIterateArrayCallsFnBeforeItSkips(t *testing.T) {
	calls := 0
	err := CursorOf(`[{"a":`).IterateArray(func(Cursor[string]) error {
		calls++
		return errStop
	})
	if !is(err, errStop) || calls != 1 {
		t.Errorf("IterateArray = %v after %d calls, want errStop after 1", err, calls)
	}
}

func TestCursorIterateArrayRefusesAFaultUpToTheCloser(t *testing.T) {
	for _, tc := range []struct {
		doc  string
		saw  []string
		kind error
	}{
		{"", nil, ErrMalformed}, {emptyObject, nil, ErrMalformed}, {`[`, []string{unread}, ErrMalformed},
		{unclosedArray, []string{"1"}, ErrMalformed}, {cutArray, []string{"1", unread}, ErrMalformed},
		{arrayGap, []string{"1"}, ErrMalformed}, {`[1,,2]`, []string{"1", unread}, ErrMalformed},
		{`[x]`, []string{unread}, ErrMalformed}, {`[1}`, []string{"1"}, ErrMalformed},
	} {
		if got := cursorArray(tc.doc); !is(got.err, tc.kind) || !slices.Equal(got.raws, tc.saw) {
			t.Errorf("CursorOf(%q).IterateArray saw %q, %v, want %q, %v", tc.doc, got.raws, got.err, tc.saw,
				tc.kind)
		}
	}
}

func TestCursorIterateArrayReadsNothingPastTheCloser(t *testing.T) {
	for _, doc := range []string{emptyArray + trailer, oneArray + trailer, `[1]]`} {
		if err := cursorArray(doc).err; err != nil {
			t.Errorf("CursorOf(%q).IterateArray = %v, want nil", doc, err)
		}
	}
	a, _ := CursorOf(`{"a":[1],`).FindMember("a")
	if err := a.IterateArray(func(Cursor[string]) error { return nil }); err != nil {
		t.Errorf("IterateArray of the member a of {\"a\":[1], = %v, want nil", err)
	}
}

func TestCursorIterateMembersDecodesNames(t *testing.T) {
	doc := `{"plain":1,"caf\u00e9":"x","\ud83d\ude80":[],"":null,"x\"y":{"c":1}} x`
	got := cursorMembers(doc)
	names, raws, err := got.names, got.raws, got.err
	wantNames := []string{plainText, cafe, "🚀", "", `x"y`}
	wantRaws := []string{"1", quoted("x"), emptyArray, litNull, `{"c":1}`}
	if err != nil || !slices.Equal(names, wantNames) || !slices.Equal(raws, wantRaws) {
		t.Errorf("CursorOf(%q).IterateMembers saw %q, %q, %v, want %q, %q, nil", doc, names, raws, err,
			wantNames, wantRaws)
	}
	data := []byte(`{"name":1}`)
	err = CursorOf(data).IterateMembers(func(name []byte, _ Cursor[[]byte]) error {
		if &name[0] != &data[2] || !capped(name) {
			t.Errorf("name %q is not a capped view of the document", name)
		}
		return nil
	})
	noError(t, err)
}

func TestCursorIterateMembersRefusesARepeatAtItsLevel(t *testing.T) {
	for _, tc := range []struct {
		doc    string
		before int
	}{
		{repeatedA, 1}, {escapedRepeat, 1}, {`{"\u00e9":1,"é":2}`, 1}, {"{\"\xff\":1,\"\\ufffd\":2}", 1},
		{`{"x":{"a":1},"y":2,"x":3}`, 2}, {`{"a":1,"a":[`, 1},
	} {
		got := cursorMembers(tc.doc)
		names, err := got.names, got.err
		if !is(err, ErrDuplicateName) || len(names) != tc.before {
			t.Errorf("CursorOf(%q).IterateMembers = %v after %d calls, want ErrDuplicateName after %d",
				tc.doc, err, len(names), tc.before)
		}
	}
	if err := cursorMembers(`{"a":{"a":1,"a":2},"b":{"a":3}}`).err; err != nil {
		t.Errorf("IterateMembers of names repeated only in nested objects = %v, want nil", err)
	}
}

func TestCursorIterateMembersCallsFnBeforeItSkips(t *testing.T) {
	calls := 0
	err := CursorOf(`{"a":[`).IterateMembers(func(string, Cursor[string]) error {
		calls++
		return errStop
	})
	if !is(err, errStop) || calls != 1 {
		t.Errorf("IterateMembers = %v after %d calls, want errStop after 1", err, calls)
	}
}

func TestCursorIterateMembersRefusesAFaultUpToTheCloser(t *testing.T) {
	for _, tc := range []struct {
		doc string
		saw []string
	}{
		{"", nil}, {emptyArray, nil}, {`{`, nil}, {`{"a"`, nil}, {`{"a" 1}`, nil}, {`{a:1}`, nil},
		{openMember, []string{unread}}, {cutMember, []string{"1"}}, {`{"a":1,}`, []string{"1"}},
		{`{"a":1 "c":2}`, []string{"1"}}, {`{"a":x}`, []string{unread}}, {`{"\x":1}`, nil},
	} {
		if got := cursorMembers(tc.doc); !is(got.err, ErrMalformed) || !slices.Equal(got.raws, tc.saw) {
			t.Errorf("CursorOf(%q).IterateMembers saw %q, %v, want %q, ErrMalformed", tc.doc, got.raws, got.err,
				tc.saw)
		}
	}
}

func TestCursorIterateMembersReadsNothingPastTheCloser(t *testing.T) {
	for _, doc := range []string{emptyObject + trailer, objectA1 + "}", `{"a":1} {`} {
		if err := cursorMembers(doc).err; err != nil {
			t.Errorf("CursorOf(%q).IterateMembers = %v, want nil", doc, err)
		}
	}
}

func TestCursorAllocs(t *testing.T) {
	data := []byte(`{"a":1,"result":{"content":[{"type":"text","text":"clean"}]}}`)
	c := CursorOf(data)
	result, _ := c.FindMember("result")
	content, _ := result.FindMember("content")
	assertAllocs(t, 0, func() { _ = CursorOf(data) })
	assertAllocs(t, 0, func() { _ = content.Kind() })
	assertAllocs(t, 0, func() { _, _ = c.FindMember("result") })
	assertAllocs(t, 0, func() { _, _ = content.FindElement(0) })
	assertAllocs(t, 0, func() { _, _ = content.Raw() })
	assertAllocs(t, 0, func() { noError(t, content.IterateArray(func(Cursor[[]byte]) error { return nil })) })
	assertAllocs(t, 0, func() {
		if got, ok := toolTextCursor(data); string(got) != "clean" || !ok {
			t.Errorf("toolTextCursor(%s) = %q, %v, want clean, true", data, got, ok)
		}
	})
}

func TestCursorIterateMembersAllocs(t *testing.T) {
	fn := func([]byte, Cursor[[]byte]) error { return nil }
	walk := func(data []byte) func() { return func() { noError(t, CursorOf(data).IterateMembers(fn)) } }
	assertAllocs(t, 0, walk([]byte(manyMembers(documentedInlineNames, -1))))
	assertAllocs(t, 1, walk([]byte(`{"a\u00e9":1,"b\n":2}`)))
	assertAllocs(t, escapedChunks, walk(escapedObject()))
	for _, n := range []int{documentedInlineNames + 1, 2 * documentedInlineNames} {
		assertAllocs(t, spillAllocs, walk([]byte(manyMembers(n, -1))))
	}
	inlineDoc := manyMembers(documentedInlineNames, -1)
	assertAllocs(t, 0, func() {
		noError(t, CursorOf(inlineDoc).IterateMembers(func(string, Cursor[string]) error { return nil }))
	})
}

func TestCursorCostIsLinear(t *testing.T) {
	elements := func(n int) string { return "[" + strings.Repeat(`{"a":"x"},`, n) + "1]" }
	run := func(doc string, read func(c Cursor[string]) bool) func() {
		return func() {
			if !read(CursorOf(doc)) {
				t.Fatalf("a read of %.20q failed", doc)
			}
		}
	}
	reads := map[string]func(c Cursor[string]) bool{
		"FindElement": func(c Cursor[string]) bool { _, ok := c.FindElement(tailMembers); return ok },
		"Raw":         func(c Cursor[string]) bool { _, ok := c.Raw(); return ok },
		"IterateArray": func(c Cursor[string]) bool {
			return c.IterateArray(func(Cursor[string]) error { return nil }) == nil
		},
	}
	for name, read := range reads {
		t.Run(name, func(t *testing.T) {
			assertCost(t, run(elements(tailMembers), read), run(elements(costScale*tailMembers), read), costScale)
		})
	}
	find := func(c Cursor[string]) bool { _, ok := c.FindMember("z"); return !ok }
	assertCost(t, run(manyMembers(tailMembers, -1), find), run(manyMembers(costScale*tailMembers, -1), find),
		costScale)
	iterate := func(c Cursor[string]) bool {
		return c.IterateMembers(func(string, Cursor[string]) error { return nil }) == nil
	}
	assertCost(t, run(manyMembers(tailMembers, -1), iterate),
		run(manyMembers(costScale*tailMembers, -1), iterate), costScale)
}

func ExampleCursor() {
	doc := `{"id":7,"items":[{"name":"a","tags":["x"]},{"name":"b\u00e9","tags":["y","z"]}]}`
	if !ValidUTF8(doc, MaxDepth) {
		return
	}
	items, _ := CursorOf(doc).FindMember("items")
	var names []string
	err := items.IterateArray(func(item Cursor[string]) error {
		tags, _ := item.FindMember("tags")
		if _, ok := tags.FindElement(1); !ok {
			return nil
		}
		name, _ := item.FindMember("name")
		raw, _ := name.Raw()
		s, _ := DecodeStringView(raw)
		names = append(names, s)
		return nil
	})
	fmt.Println(names, err)
	// Output: [bé] <nil>
}

func ExampleCursor_IterateMembers() {
	doc := `{"level":"warn","count":3,"extra":{"k":"v"}}`
	var objects []string
	err := CursorOf(doc).IterateMembers(func(name string, value Cursor[string]) error {
		if value.Kind() == KindObject {
			objects = append(objects, name)
		}
		return nil
	})
	fmt.Println(objects, err)
	// Output: [extra] <nil>
}

// refKind returns the kind leadKinds gives the byte at data[at], or KindInvalid where
// no byte is; at -1 stands for no value.
func refKind(kinds map[byte]Kind, data []byte, at int) Kind {
	if at < 0 || at >= len(data) {
		return KindInvalid
	}
	return kinds[data[at]]
}

// refRaw returns the value the shallow grammar reads at data[at] and whether it reads
// one; at -1 stands for no value.
func refRaw(data []byte, at int) ([]byte, bool) {
	if at < 0 {
		return nil, false
	}
	end := refValue(data, at)
	if end < 0 {
		return nil, false
	}
	return data[at:end], true
}

// shallowMember returns where the shallow grammar starts the value of the first member
// of the object at data[at] whose name decodes to name, reading the members before it,
// or -1; at -1 stands for no value.
func shallowMember(t *testing.T, data []byte, at int, name string) int {
	t.Helper()
	if at < 0 {
		return -1
	}
	found := -1
	_, err := shallowScan(data, at, '{', func(key []byte, value, _ int) error {
		if stdName(t, key) != name {
			return nil
		}
		found = value
		return errStop
	})
	return foundAt(data, found, err)
}

// shallowElement returns where the shallow grammar starts the element at index of the
// array at data[at], reading the elements before it, or -1.
func shallowElement(data []byte, at, index int) int {
	found, n := -1, 0
	_, err := shallowScan(data, at, '[', func(_ []byte, value, _ int) error {
		if n != index {
			n++
			return nil
		}
		found = value
		return errStop
	})
	return foundAt(data, found, err)
}

// foundAt returns found when the lookup that ended with err stopped there and a byte of
// data is there, or -1.
func foundAt(data []byte, found int, err error) int {
	if !is(err, errStop) || found >= len(data) {
		return -1
	}
	return found
}

// checkAt fails t unless the Cursor c that a lookup returned with ok sits at want, the
// zero Cursor for -1, and Raw at c reads what refRaw reads there.
func checkAt(t *testing.T, lookup string, data []byte, c Cursor[[]byte], ok bool, want int) {
	t.Helper()
	raw, rawOK := c.Raw()
	wantRaw, wantOK := refRaw(data, want)
	placed := ok && c.at == want || !ok && want < 0 && zeroCursor(c)
	if !placed || !sameView(raw, rawOK, wantRaw, wantOK) {
		t.Fatalf("%s = a Cursor at %d, %v reading %q, %v; the shallow grammar finds %d reading %q, %v", lookup,
			c.at, ok, raw, rawOK, want, wantRaw, wantOK)
	}
}

// checkMember fails t unless FindMember(name) of c, a Cursor at data[at], finds what
// the shallow grammar finds; it returns that Cursor and the index of the value, or -1.
func checkMember(t *testing.T, data []byte, c Cursor[[]byte], at int, name string) (m Cursor[[]byte], want int) {
	t.Helper()
	m, ok := c.FindMember(name)
	want = shallowMember(t, data, at, name)
	checkAt(t, fmt.Sprintf("FindMember(%q) at %d of %q", name, at, data), data, m, ok, want)
	return m, want
}

// checkStdMember fails t unless FindMember(name) of c, a Cursor at the object raw that
// encoding/json accepts, reads the value json.Decoder has first for name.
func checkStdMember(t *testing.T, c Cursor[[]byte], raw []byte, name string) {
	t.Helper()
	names, values, err := decoderMembers(raw)
	if err != nil {
		t.Fatalf("json.Decoder reads the valid object %q with %v, want nil", raw, err)
	}
	m, ok := c.FindMember(name)
	got, gotOK := m.Raw()
	if k := slices.Index(names, name); ok != (k >= 0) || k >= 0 && (!gotOK || !rawEqual(got, values[k])) {
		t.Fatalf("FindMember(%q) in %q reads %q, %v; json.Decoder has %q, %q", name, raw, got, gotOK, names,
			values)
	}
}

// FuzzCursorKind checks Kind at the top value and at a member's value against the kind
// of the byte the shallow grammar starts the value with, and against encoding/json.
func FuzzCursorKind(f *testing.F) {
	for _, s := range []string{objectA1, ` "x`, `n`, "\t-", `{"a": [`, `{"a":}`, blank, `}`} {
		f.Add([]byte(s), "a")
	}
	kinds := leadKinds()
	f.Fuzz(func(t *testing.T, data []byte, name string) {
		top := refSpace(data, 0)
		m, _ := CursorOf(data).FindMember(name)
		for _, c := range []struct {
			got Kind
			at  int
		}{{CursorOf(data).Kind(), top}, {m.Kind(), shallowMember(t, data, top, name)}} {
			if want := refKind(kinds, data, c.at); c.got != want {
				t.Fatalf("Kind() at %d of %q = %v, want %v", c.at, data, c.got, want)
			}
		}
		var v any
		if json.Unmarshal(data, &v) == nil && CursorOf(data).Kind() != stdKind(v) {
			t.Fatalf("CursorOf(%q).Kind() = %v, encoding/json decodes a %v", data, CursorOf(data).Kind(), stdKind(v))
		}
	})
}

// FuzzCursorFindMember checks FindMember, one level down too, and Raw at what it finds
// against the shallow grammar read from the Cursor's position on any input, and against
// encoding/json at each object it accepts.
func FuzzCursorFindMember(f *testing.F) {
	f.Add([]byte(`{"a":{"b":"x\n"},"c":1}`), "a", "b")
	f.Add([]byte(repeatedA), "a", "a")
	f.Add([]byte(`{"a":{"b":1,`), "a", "b")
	f.Add([]byte(`{"café":[{"b":2}]}`), cafe, "b")
	f.Add([]byte(`{"a":"\ud800"}`), "a", "")
	f.Add([]byte(`{"a": `), "a", "")
	f.Add([]byte(`[[0]]`), "[", "")
	f.Fuzz(func(t *testing.T, data []byte, outer, inner string) {
		m, at := checkMember(t, data, CursorOf(data), refSpace(data, 0), outer)
		checkMember(t, data, m, at, inner)
		if json.Valid(data) && opens(data, '{') {
			checkStdMember(t, CursorOf(data), data, outer)
		}
		if raw, ok := m.Raw(); ok && json.Valid(raw) && opens(raw, '{') {
			checkStdMember(t, m, raw, inner)
		}
	})
}

// FuzzCursorFindElement checks FindElement and Raw at what it finds against the shallow
// grammar on any input, and against encoding/json on valid input.
func FuzzCursorFindElement(f *testing.F) {
	f.Add([]byte(`[1,"two",{"3":3}]`), 2)
	f.Add([]byte(emptyArray), 0)
	f.Add([]byte(oneArray), -1)
	f.Add([]byte(`[1,2,]`), 1)
	f.Add([]byte(`[[1,,2],3`), 1)
	f.Add([]byte(`[1, `), 1)
	f.Fuzz(func(t *testing.T, data []byte, index int) {
		e, ok := CursorOf(data).FindElement(index)
		checkAt(t, fmt.Sprintf("FindElement(%d) of %q", index, data), data, e, ok,
			shallowElement(data, refSpace(data, 0), index))
		var std []json.RawMessage
		if !json.Valid(data) || json.Unmarshal(data, &std) != nil {
			return
		}
		raw, rawOK := e.Raw()
		if inRange := index >= 0 && index < len(std); rawOK != inRange || inRange && !rawEqual(raw, std[index]) {
			t.Fatalf("FindElement(%d) of %q reads %q, %v; encoding/json has %q", index, data, raw, rawOK, std)
		}
	})
}

// FuzzCursorRaw checks Raw against the value the shallow grammar reads at the top
// value, and against encoding/json on valid input.
func FuzzCursorRaw(f *testing.F) {
	for _, s := range []string{
		bracketsOnly, ` 1 x`, `"a\"b"]`, unclosedArray, `fals`, `{"a":[1,"]"]}`, blank, "\"\xc3\"",
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		got, ok := CursorOf(data).Raw()
		if want, wantOK := refRaw(data, refSpace(data, 0)); !sameView(got, ok, want, wantOK) || !capped(got) {
			t.Fatalf("CursorOf(%q).Raw() = %q, %v; the shallow grammar reads %q, %v", data, got, ok, want, wantOK)
		}
		if json.Valid(data) && (!ok || !bytes.Equal(got, bytes.TrimSpace(data))) {
			t.Fatalf("CursorOf(%q).Raw() = %q, %v, want the whole valid value", data, got, ok)
		}
	})
}

// shallowIteration is what a Cursor iteration of the container that opener starts at
// data[at] sees by the shallow grammar: each item before its value is read, unread
// where that fails, its name decoded, and a repeated name refused.
func shallowIteration(t *testing.T, data []byte, at int, opener byte) sawItems {
	t.Helper()
	var s sawItems
	seen := map[string]bool{}
	_, s.err = shallowScan(data, at, opener, func(key []byte, value, end int) error {
		if opener == '{' {
			name := stdName(t, key)
			if seen[name] {
				return ErrDuplicateName
			}
			seen[name], s.names = true, append(s.names, name)
		}
		raw := unread
		if end >= 0 {
			raw = string(data[value:end])
		}
		s.raws = append(s.raws, raw)
		return nil
	})
	return s
}

// checkIteration fails t unless got, what a Cursor iteration of the container that
// opener starts in data saw, is what the shallow grammar has it see.
func checkIteration(t *testing.T, data string, opener byte, got sawItems) {
	t.Helper()
	d := []byte(data)
	want := shallowIteration(t, d, refSpace(d, 0), opener)
	if !is(got.err, want.err) || !slices.Equal(got.names, want.names) || !slices.Equal(got.raws, want.raws) {
		t.Fatalf("iteration of %q saw %q, %q, %v; the shallow grammar %q, %q, %v", data, got.names, got.raws,
			got.err, want.names, want.raws, want.err)
	}
}

// FuzzCursorIterateArray checks IterateArray against the shallow grammar on any input,
// and against encoding/json on valid input.
func FuzzCursorIterateArray(f *testing.F) {
	for _, s := range []string{
		`[1,"two",{"3":3}]`, emptyArray + trailer, `[1,,2]`, oneArray + trailer, `[[1,2],"\ud800"`, `[`,
		`[{[}]`, `[{]}`,
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, data string) {
		got := cursorArray(data)
		checkIteration(t, data, '[', got)
		var std []json.RawMessage
		if !json.Valid([]byte(data)) || json.Unmarshal([]byte(data), &std) != nil {
			return
		}
		if got.err != nil || !slices.EqualFunc(got.raws, std, func(g string, s json.RawMessage) bool {
			return rawEqual([]byte(g), s)
		}) {
			t.Fatalf("IterateArray of %q saw %q, %v; encoding/json %q", data, got.raws, got.err, std)
		}
	})
}

// FuzzCursorIterateMembers checks IterateMembers against the shallow grammar on any
// input, and against encoding/json on valid input.
func FuzzCursorIterateMembers(f *testing.F) {
	for _, s := range []string{
		escapedRepeat, `{"x":{"y":1}}`, `{"a":1,"a":[}`, `{"m":[,],"m":1}`,
		manyMembers(manyNames, documentedInlineNames), objectA1 + trailer, openMember, `{"a":0,"a":`, `{"":[}]`,
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, data string) {
		got := cursorMembers(data)
		checkIteration(t, data, '{', got)
		if !json.Valid([]byte(data)) || !strings.HasPrefix(strings.TrimSpace(data), "{") {
			return
		}
		wantNames, wantValues, err := decoderMembers([]byte(data))
		if err != nil {
			return
		}
		k := firstRepeat(wantNames)
		same := slices.Equal(got.names, wantNames[:k]) && slices.EqualFunc(got.raws, wantValues[:k],
			func(g string, s json.RawMessage) bool { return rawEqual([]byte(g), s) })
		var wantErr error
		if k < len(wantNames) {
			wantErr = ErrDuplicateName
		}
		if !same || !is(got.err, wantErr) {
			t.Fatalf("IterateMembers of %q saw %q, %q, %v; json.Decoder %q, %q", data, got.names, got.raws,
				got.err, wantNames, wantValues)
		}
	})
}

// firstRepeat returns the index of the first name of names that repeats an earlier
// one, or len(names).
func firstRepeat(names []string) int {
	for i := range names {
		if slices.Contains(names[:i], names[i]) {
			return i
		}
	}
	return len(names)
}

// toolEnvelope returns a response whose result holds a content array with one text
// item: hits records of escaped JSON.
func toolEnvelope(tb testing.TB, hits int) []byte {
	tb.Helper()
	rows := make([]string, 0, hits)
	for n := range hits {
		rows = append(rows, `{"n_total":1000,"d_logid":`+strconv.Itoa(n)+`,"p_logid":950,`+
			`"message":"`+logMessage+`"}`)
	}
	text, err := json.Marshal(`{"hits":[` + strings.Join(rows, ",") + `]}`)
	if err != nil {
		tb.Fatalf("json.Marshal = %v, want nil", err)
	}
	return []byte(`{"id":1,"result":{"content":[{"type":"text","text":` + string(text) + `}],"isError":false}}`)
}

// toolTextChain reads the first content text of body with one lookup per level,
// each skipping the value it returns, and reports whether it found one.
func toolTextChain(body []byte) ([]byte, bool) {
	result, _ := FindMember(body, "result")
	content, _ := FindMember(result, "content")
	var out []byte
	err := IterateArray(content, func(item []byte) error {
		t, ok := FindMember(item, "text")
		if !ok {
			return nil
		}
		out, _ = DecodeStringView(t)
		return errStop
	})
	return out, is(err, errStop)
}

// toolTextCursor is toolTextChain with a Cursor, which skips no value it enters.
func toolTextCursor(body []byte) ([]byte, bool) {
	result, _ := CursorOf(body).FindMember("result")
	content, _ := result.FindMember("content")
	var out []byte
	err := content.IterateArray(func(item Cursor[[]byte]) error {
		t, ok := item.FindMember("text")
		if !ok {
			return nil
		}
		raw, _ := t.Raw()
		out, _ = DecodeStringView(raw)
		return errStop
	})
	return out, errors.Is(err, errStop)
}

// stdContent is an item of the content array of toolEnvelope as encoding/json reads it.
type stdContent struct {
	Text string `json:"text"`
}

// stdToolResult is the result of toolEnvelope as encoding/json reads it.
type stdToolResult struct {
	Content []stdContent `json:"content"`
}

// stdToolResponse is the response of toolEnvelope as encoding/json reads it.
type stdToolResponse struct {
	Result stdToolResult `json:"result"`
}

func BenchmarkCursorToolText(b *testing.B) {
	for _, hits := range []int{1, toolHits} {
		body := toolEnvelope(b, hits)
		var std stdToolResponse
		if err := json.Unmarshal(body, &std); err != nil {
			b.Fatalf("json.Unmarshal = %v, want nil", err)
		}
		for _, read := range []struct {
			name string
			text func([]byte) ([]byte, bool)
		}{{"chain", toolTextChain}, {"cursor", toolTextCursor}} {
			if got, ok := read.text(body); string(got) != std.Result.Content[0].Text || !ok {
				b.Fatalf("%s text = %.40q, %v, want %.40q, true", read.name, got, ok, std.Result.Content[0].Text)
			}
			b.Run(fmt.Sprintf("hits%d/%s", hits, read.name), func(b *testing.B) {
				b.ReportAllocs()
				b.SetBytes(int64(len(body)))
				for b.Loop() {
					_, _ = read.text(body)
				}
			})
		}
	}
}

// benchCursor checks that read gives want on data once, then times it.
func benchCursor(b *testing.B, data []byte, want string, read func(c Cursor[[]byte]) (Cursor[[]byte], bool)) {
	b.Helper()
	c := CursorOf(data)
	if got, ok := read(c); !ok || !bytes.HasPrefix(got.data[got.at:], []byte(want)) {
		b.Fatalf("read of %s = %+v, %v, want a Cursor at %s", data, got, ok, want)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		_, _ = read(c)
	}
}

func BenchmarkCursorOf(b *testing.B) {
	benchCursor(b, []byte(" \n"+objectA1), objectA1, func(c Cursor[[]byte]) (Cursor[[]byte], bool) {
		return CursorOf(c.data), true
	})
}

func BenchmarkCursorKind(b *testing.B) {
	data := []byte(fixtureObject)
	c := CursorOf(data)
	if got := c.Kind(); got != KindObject {
		b.Fatalf("Kind() of %s = %v, want KindObject", data, got)
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = c.Kind()
	}
}

func BenchmarkCursorFindMember(b *testing.B) {
	benchCursor(b, []byte(fixtureObject), `"10.0.0.1"`, func(c Cursor[[]byte]) (Cursor[[]byte], bool) {
		return c.FindMember("source")
	})
}

// benchElements returns the elements of the array the element benchmarks read.
func benchElements() []string {
	return []string{`{"a":1}`, `{"b":2}`, `{"c":3}`, `{"d":4}`, `{"e":5}`}
}

func BenchmarkCursorFindElement(b *testing.B) {
	elements := benchElements()
	last := len(elements) - 1
	benchCursor(b, []byte("["+strings.Join(elements, ",")+"]"), elements[last],
		func(c Cursor[[]byte]) (Cursor[[]byte], bool) { return c.FindElement(last) })
}

func BenchmarkCursorRaw(b *testing.B) {
	data := []byte(`{"a":[1,2,3],"b":"x"}`)
	c := CursorOf(data)
	if got, ok := c.Raw(); !bytes.Equal(got, data) || !ok {
		b.Fatalf("Raw() of %s = %s, %v, want the whole value", data, got, ok)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		_, _ = c.Raw()
	}
}

func BenchmarkCursorIterateArray(b *testing.B) {
	elements := benchElements()
	data := []byte("[" + strings.Join(elements, ",") + "]")
	c := CursorOf(data)
	n := 0
	count := func(Cursor[[]byte]) error { n++; return nil }
	if err := c.IterateArray(count); n != len(elements) || err != nil {
		b.Fatalf("IterateArray of %s saw %d elements, %v, want %d, nil", data, n, err, len(elements))
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		if err := c.IterateArray(count); err != nil {
			b.Fatalf("IterateArray of %s = %v, want nil", data, err)
		}
	}
}

func BenchmarkCursorIterateMembers(b *testing.B) {
	for _, bc := range []struct{ name, doc string }{
		{"Clean", fixtureObject},
		{"Escaped", `{"caf\u00e9":1,"line\nbreak":2,"plain":3,"tab\tbed":4}`},
	} {
		data := []byte(bc.doc)
		c := CursorOf(data)
		got := cursorMembers(bc.doc)
		names, err := got.names, got.err
		if wantNames, _, wantErr := decoderMembers(data); !slices.Equal(names, wantNames) || err != nil ||
			wantErr != nil {
			b.Fatalf("IterateMembers of %s saw %q, %v, want %q, nil", data, names, err, wantNames)
		}
		b.Run(bc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			for b.Loop() {
				if err := c.IterateMembers(func([]byte, Cursor[[]byte]) error { return nil }); err != nil {
					b.Fatalf("IterateMembers of %s = %v, want nil", data, err)
				}
			}
		})
	}
}
