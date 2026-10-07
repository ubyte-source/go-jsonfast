package jsonfast

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"maps"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestValidUTF8MatchesEncodingJSON(t *testing.T) {
	for _, tc := range validityCases() {
		if json.Valid([]byte(tc.in)) != tc.valid {
			t.Errorf("json.Valid(%q) = %v, want %v as the table says", tc.in, !tc.valid, tc.valid)
		}
		want := tc.valid && replacementFree([]byte(tc.in))
		if got := ValidUTF8(tc.in, MaxDepth); got != want {
			t.Errorf("ValidUTF8(%q) = %v, want %v", tc.in, got, want)
		}
		if got := ValidUTF8([]byte(tc.in), MaxDepth); got != want {
			t.Errorf("ValidUTF8([]byte(%q)) = %v, want %v", tc.in, got, want)
		}
	}
}

func TestValidUTF8HonorsTheDepthBound(t *testing.T) {
	for name, objectAt := range map[string]func(int) bool{
		"Array":  func(int) bool { return false },
		"Object": func(int) bool { return true },
		"Mixed":  func(i int) bool { return i%2 == 1 },
	} {
		for _, limit := range []int{
			1, 2, kindWordBits - 1, kindWordBits, kindWordBits + 1,
			inlineLevels - 1, inlineLevels, inlineLevels + 1, inlineLevels + kindWordBits + 1,
		} {
			within, deeper := leveled(limit, objectAt), leveled(limit+1, objectAt)
			if !json.Valid([]byte(within)) {
				t.Fatalf("%s: json.Valid of the fixture %q = false, want true", name, within)
			}
			if !ValidUTF8(within, limit) {
				t.Errorf("%s: ValidUTF8 of %d levels at maxDepth %d = false, want true", name, limit, limit)
			}
			if ValidUTF8(deeper, limit) {
				t.Errorf("%s: ValidUTF8 of %d levels at maxDepth %d = true, want false", name, limit+1, limit)
			}
			if _, err := walkAll(deeper, limit); !is(err, ErrMalformed) {
				t.Errorf("%s: WalkStrings on %d levels at maxDepth %d = %v, want ErrMalformed",
					name, limit+1, limit, err)
			}
		}
	}
}

func TestValidUTF8GivesScalarsDepthZero(t *testing.T) {
	for _, tc := range []struct {
		in    string
		depth int
		want  bool
	}{
		{"1", 0, true},
		{`"s"`, 0, true},
		{emptyArray, 0, false},
		{emptyObject, 0, false},
		{emptyArray, 1, true},
		{"1", -1, false},
		{"", -1, false},
		{emptyArray, -1, false},
	} {
		if got := ValidUTF8(tc.in, tc.depth); got != tc.want {
			t.Errorf("ValidUTF8(%q, %d) = %v, want %v", tc.in, tc.depth, got, tc.want)
		}
	}
}

func TestValidUTF8MaxDepthMatchesEncodingJSON(t *testing.T) {
	for _, levels := range []int{MaxDepth, MaxDepth + 1} {
		doc := []byte(arrays(levels))
		if got, want := ValidUTF8(doc, MaxDepth), json.Valid(doc); got != want || got != (levels == MaxDepth) {
			t.Errorf("%d levels: ValidUTF8 = %v, json.Valid = %v, want both %v", levels, got, want, levels == MaxDepth)
		}
	}
}

func TestValidUTF8WalksHugeNestingIteratively(t *testing.T) {
	const levels = 1 << 18
	doc := []byte(arrays(levels))
	defer debug.SetMaxStack(debug.SetMaxStack(flatStack))
	for _, tc := range []struct {
		name     string
		doc      []byte
		maxDepth int
		want     bool
	}{
		{"2^18 levels at MaxDepth", doc, MaxDepth, false},
		{"2^18 balanced levels with no bound", doc, math.MaxInt, true},
		{"2^18 levels with one closer missing", doc[:len(doc)-1], math.MaxInt, false},
	} {
		if got := ValidUTF8(tc.doc, tc.maxDepth); got != tc.want {
			t.Fatalf("ValidUTF8 of %s = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestValidUTF8TracksKindsPastTheInlineStack(t *testing.T) {
	const pairs = 150
	doc := nest(`[{"a":`, "1", "}]", pairs)
	for _, tc := range []struct {
		name, doc string
		want      bool
	}{
		{"a valid 300-level document", doc, true},
		{"a wrong closer at level 300", strings.Replace(doc, "1}", "1]", 1), false},
		{"a wrong closer at level 2", doc[:len(doc)-2] + "]}", false},
	} {
		if got := ValidUTF8(tc.doc, 2*pairs); got != tc.want {
			t.Fatalf("ValidUTF8 of %s = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestKindStackSpillsOnlyTheLevelsPastItsInlineWords(t *testing.T) {
	for _, tc := range []struct{ levels, words int }{
		{inlineLevels + 1, 1}, {inlineLevels + kindWordBits, 1}, {inlineLevels + kindWordBits + 1, 2},
		{inlineLevels + 2*kindWordBits, 2},
	} {
		var s kindStack
		for depth := range tc.levels {
			s.set(depth, uint64(depth%3&1))
		}
		if len(s.deep) != tc.words {
			t.Errorf("%d levels: spill holds %d words, want %d", tc.levels, len(s.deep), tc.words)
		}
		for depth := range tc.levels {
			if got := s.get(depth); got != uint64(depth%3&1) {
				t.Fatalf("%d levels: level %d holds %d, want %d", tc.levels, depth, got, depth%3&1)
			}
		}
	}
	var s kindStack
	for depth := range inlineLevels {
		s.set(depth, openObject)
	}
	if s.deep != nil {
		t.Fatalf("%d levels spilled %d words, want none", inlineLevels, len(s.deep))
	}
}

func TestKindStackGrowsWithTheDepthReached(t *testing.T) {
	doc := []byte(nest("[", strings.Repeat("0,", hugeDigits)+"0", "]", inlineLevels+1))
	w := newWalker(doc, math.MaxInt)
	for {
		if i, ok := w.next(); !ok || i == endOfValue {
			break
		}
	}
	if w.state != walkDone || cap(w.kinds.deep) != 1 {
		t.Fatalf("a %d-byte document %d levels deep ended in state %d with %d spill words, want 1",
			len(doc), inlineLevels+1, w.state, cap(w.kinds.deep))
	}
}

func TestValidUTF8Allocs(t *testing.T) {
	doc := []byte(`{"a":1,"b":{"c":[1,2,3],"d":"caf\u00e9"},"e":"hello"}`)
	within := []byte(arrays(inlineLevels))
	beyond := []byte(arrays(inlineLevels + 1))
	docString := string(doc)
	assertAllocs(t, 0, func() { _ = ValidUTF8(doc, 64) })
	assertAllocs(t, 0, func() { _ = ValidUTF8(docString, 64) })
	assertAllocs(t, 0, func() { _ = ValidUTF8(within, MaxDepth) })
	assertAllocs(t, 1, func() { _ = ValidUTF8(beyond, MaxDepth) })
}

func BenchmarkValidUTF8EncodingJSON(b *testing.B) {
	doc := []byte(validityDocument)
	if !json.Valid(doc) {
		b.Fatalf("json.Valid(%s) = false, want true", doc)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(doc)))
	for b.Loop() {
		_ = json.Valid(doc)
	}
}

func TestValidUTF8RejectsWhatDecodingWouldReplace(t *testing.T) {
	for _, tc := range utf8Cases() {
		if got := ValidUTF8(tc.in, MaxDepth); got != tc.valid {
			t.Errorf("ValidUTF8(%q) = %v, want %v", tc.in, got, tc.valid)
		}
		if oracle := json.Valid([]byte(tc.in)) && replacementFree([]byte(tc.in)); oracle != tc.valid {
			t.Errorf("the reference oracle on %q = %v, want %v as the table says", tc.in, oracle, tc.valid)
		}
	}
}

func FuzzValidUTF8(f *testing.F) {
	for _, tc := range utf8Cases() {
		f.Add([]byte(tc.in))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		got := ValidUTF8(data, MaxDepth)
		if want := jsonUTF8(data); got != want {
			t.Fatalf("ValidUTF8(%q) = %v, want %v", data, got, want)
		}
		if got && !utf8.Valid(data) {
			t.Fatalf("ValidUTF8(%q) = true for invalid UTF-8, want false", data)
		}
		if ValidUTF8(string(data), MaxDepth) != got {
			t.Fatalf("ValidUTF8 of the string %q = %v, want %v as of the bytes", data, !got, got)
		}
	})
}

func BenchmarkValidUTF8(b *testing.B) {
	doc := []byte(validityDocument)
	if !ValidUTF8(doc, MaxDepth) || !jsonUTF8(doc) {
		b.Fatalf("ValidUTF8(%s) = false, want true as encoding/json reads it", doc)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(doc)))
	for b.Loop() {
		_ = ValidUTF8(doc, MaxDepth)
	}
}

// stringWithEscapes is a string token with escapes, which strictEnd checks and
// strictToken decodes.
const stringWithEscapes = `"line\nbreak caf\u00e9"`

// benchmarkStringEnd times end on stringWithEscapes, which ends at its last byte.
func benchmarkStringEnd(b *testing.B, end func(data []byte, at int) (int, bool)) {
	b.Helper()
	data := []byte(stringWithEscapes)
	if got, ok := end(data, 0); !ok || got != len(data) {
		b.Fatalf("the end of %s = %d, %v, want %d, true", data, got, ok, len(data))
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		_, _ = end(data, 0)
	}
}

func BenchmarkStrictEnd(b *testing.B) {
	benchmarkStringEnd(b, strictEnd)
}

func BenchmarkStrictEndSharedForm(b *testing.B) {
	benchmarkStringEnd(b, func(data []byte, at int) (int, bool) {
		var a arena
		_, end, ok := a.strictToken(data, at)
		return end, ok
	})
}

func ExampleValidUTF8() {
	fmt.Println(ValidUTF8(`{"a":"\ud83d\ude80"}`, MaxDepth), ValidUTF8(`{"a":[1,2]}`, 1))
	fmt.Println(ValidUTF8(`{"a":"\ud83d"}`, MaxDepth), ValidUTF8(`[1,]`, MaxDepth), ValidUTF8(`42`, 0))
	// Output:
	// true false
	// false false true
}

// walkAll collects the strings WalkStrings reports for data.
func walkAll[T Text](data T, maxDepth int) ([]T, error) {
	got := []T{}
	err := WalkStrings(data, maxDepth, func(s T) error {
		got = append(got, s)
		return nil
	})
	return got, err
}

func TestWalkStringsVisitsEveryStringInOrder(t *testing.T) {
	doc := `{"a":["b\n",{"c\u00e9":"d"}],"e":1,"f":null,"g":"h","i":[true,-2.5e3,"","\t` + "\uFFFD\"]}"
	got, err := walkAll(doc, MaxDepth)
	want := []string{"a", "b\n", "cé", "d", "e", "f", "g", "h", "i", "", "\t\uFFFD"}
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("WalkStrings = %q, %v, want %q", got, err, want)
	}
	if got, err := walkAll(` "top" `, 0); err != nil || !slices.Equal(got, []string{"top"}) {
		t.Fatalf("WalkStrings(top-level string) = %q, %v, want [top]", got, err)
	}
	if got, err := walkAll(`[1,true,null]`, 1); err != nil || len(got) != 0 {
		t.Fatalf("WalkStrings(no strings) = %q, %v, want nothing", got, err)
	}
}

func TestWalkStringsFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		in    string
		seen  []string
		depth int
	}{
		{"[\"ok\",\"\xff\"]", []string{"ok"}, MaxDepth},
		{"{\"\xc0\x80\":1}", []string{}, MaxDepth},
		{`["ok","\ud800"]`, []string{"ok"}, MaxDepth},
		{`["\ud800\u0041"]`, []string{}, MaxDepth},
		{`["a",]`, []string{"a"}, MaxDepth},
		{`["a"] x`, []string{"a"}, MaxDepth},
		{`["ok","\q"]`, []string{"ok"}, MaxDepth},
		{"[\"]\t", []string{}, MaxDepth},
		{`[["a"]]`, []string{}, 1},
		{`"a"`, []string{}, -1},
		{``, []string{}, MaxDepth},
	} {
		if seen, err := walkAll(tc.in, tc.depth); !is(err, ErrMalformed) || !slices.Equal(seen, tc.seen) {
			t.Errorf("WalkStrings(%q, %d) = %v after %q, want ErrMalformed after %q",
				tc.in, tc.depth, err, seen, tc.seen)
		}
	}
}

func TestWalkStringsReturnsTheCallbackErrorUnchanged(t *testing.T) {
	var seen []string
	err := WalkStrings(`["a","b","c",`, MaxDepth, func(s string) error {
		seen = append(seen, s)
		if s == "b" {
			return errStop
		}
		return nil
	})
	if !is(err, errStop) || strings.Join(seen, "") != "ab" {
		t.Fatalf("WalkStrings = %v after %q, want errStop after ab", err, seen)
	}
}

func TestWalkStringsKeepsEveryStringIntact(t *testing.T) {
	doc := `{"plain":"x\ny","caf\u00e9":["\t` + strings.Repeat("z", longRun) + `","tail\\"]}`
	got, err := walkAll(doc, MaxDepth)
	want := []string{plainText, "x\ny", cafe, "\t" + strings.Repeat("z", longRun), "tail\\"}
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("WalkStrings = %q, %v, want %q", got, err, want)
	}
	if !aliases(got[0], doc, 2) {
		t.Fatalf("the clean string %q is a copy, want a view of the document", got[0])
	}
	data := []byte(doc)
	views, err := walkAll(data, MaxDepth)
	noError(t, err)
	for _, v := range views {
		_ = append(v, "OVERWRITE"...)
	}
	for i, v := range views {
		if string(v) != want[i] || string(data) != doc {
			t.Fatalf("an append rewrote string %d to %q or the document, want %q and %s", i, v, want[i], doc)
		}
	}
}

func TestWalkStringsAllocs(t *testing.T) {
	doc := []byte(`{"a":["b",{"c":"d"}],"e":1,"f":"g"}`)
	fn := func([]byte) error { return nil }
	assertAllocs(t, 0, func() { noError(t, WalkStrings(doc, 64, fn)) })
	escaped := []byte(`{"a":"b\nc","d":"\u00e9"}`)
	assertAllocs(t, 1, func() { noError(t, WalkStrings(escaped, 64, fn)) })
	for _, many := range [][]byte{escapedArray(), escapedObject()} {
		assertAllocs(t, escapedChunks, func() { noError(t, WalkStrings(many, 64, fn)) })
	}
	refused := []byte("[" + refusedLate() + "]")
	assertAllocs(t, 0, func() {
		if err := WalkStrings(refused, 64, fn); !is(err, ErrMalformed) {
			t.Fatalf("WalkStrings(%.12q) = %v, want ErrMalformed", refused, err)
		}
	})
}

// TestWalkStringsAllocsImported pins that WalkStrings, instantiated outside
// package jsonfast as every caller does, allocates nothing for a document whose
// strings need no decoding: the test of testdata/walkallocs asserts the count.
func TestWalkStringsAllocsImported(t *testing.T) {
	out, err := exec.CommandContext(t.Context(), "go", "test", "-count=1", "./testdata/walkallocs").CombinedOutput()
	if err != nil {
		t.Fatalf("go test ./testdata/walkallocs = %v, want it to pass:\n%s", err, out)
	}
}

func TestWalkerValueEndWalksEachValueAnewWithOneSpill(t *testing.T) {
	deep := arrays(inlineLevels + 1)
	data := []byte(deep + " " + emptyObject + deep)
	w := newWalker(data, inlineLevels+1)
	for _, tc := range []struct{ at, want int }{{0, len(deep)}, {len(deep), len(deep) + 1 + len(emptyObject)}} {
		if end, ok := w.valueEnd(tc.at); !ok || end != tc.want {
			t.Errorf("valueEnd(%d) = %d, %v, want %d, true", tc.at, end, ok, tc.want)
		}
	}
	at := len(data) - len(deep)
	assertAllocs(t, 0, func() {
		if end, ok := w.valueEnd(at); !ok || end != len(data) {
			t.Fatalf("valueEnd(%d) = %d, %v, want %d, true", at, end, ok, len(data))
		}
	})
	if cap(w.kinds.deep) != 1 {
		t.Errorf("the walker spilled %d words over three values, want the 1 of the first", cap(w.kinds.deep))
	}
}

func TestWalkerValueEndHonorsTheBound(t *testing.T) {
	deep := arrays(inlineLevels + 1)
	data := []byte(deep + emptyObject)
	if end, ok := newWalker(data, -1).valueEnd(len(deep)); ok || end != 0 {
		t.Errorf("valueEnd under a negative bound = %d, %v, want 0, false", end, ok)
	}
	if end, ok := newWalker(data, inlineLevels).valueEnd(0); ok || end != 0 {
		t.Errorf("valueEnd of %d levels under a bound of %d = %d, %v, want 0, false",
			inlineLevels+1, inlineLevels, end, ok)
	}
}

func TestWalkerAnswersANegativeBoundAtOnce(t *testing.T) {
	refuse := func(doc string) func() {
		return func() {
			if ValidUTF8(doc, -1) {
				t.Fatalf("ValidUTF8(%.12q, -1) = true, want false", doc)
			}
			if err := WalkStrings(doc, -1, func(string) error { return nil }); !is(err, ErrMalformed) {
				t.Fatalf("WalkStrings(%.12q, -1) = %v, want ErrMalformed", doc, err)
			}
		}
	}
	assertCost(t, refuse(`"s"`), refuse(strings.Repeat(" ", decidedSpaces)+`"s"`), 1)
}

// decoderStrings lists the string tokens of data, member names included, as
// json.Decoder reports them.
func decoderStrings(data []byte) ([]string, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var out []string
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return out, fmt.Errorf("token: %w", err)
		}
		if s, ok := tok.(string); ok {
			out = append(out, s)
		}
	}
}

func FuzzWalkStrings(f *testing.F) {
	for _, tc := range utf8Cases() {
		f.Add([]byte(tc.in))
	}
	f.Add([]byte(`{"a":["b\n",{"c\u00e9":"d"}],"e":1}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		got, err := walkAll(string(data), MaxDepth)
		if !jsonUTF8(data) {
			if !is(err, ErrMalformed) {
				t.Fatalf("WalkStrings(%q) = %v, want ErrMalformed", data, err)
			}
			return
		}
		want, derr := decoderStrings(data)
		if err != nil || derr != nil || !slices.Equal(got, want) {
			t.Fatalf("WalkStrings(%q) = %q, %v; json.Decoder = %q, %v", data, got, err, want, derr)
		}
	})
}

func BenchmarkWalkStrings(b *testing.B) {
	doc := []byte(validityDocument)
	got, err := walkAll(doc, MaxDepth)
	decoded, derr := decoderStrings(doc)
	same := slices.EqualFunc(got, decoded, func(g []byte, s string) bool { return string(g) == s })
	if err != nil || derr != nil || !same {
		b.Fatalf("WalkStrings = %q, %v; json.Decoder = %q, %v", got, err, decoded, derr)
	}
	fn := func([]byte) error { return nil }
	b.ReportAllocs()
	b.SetBytes(int64(len(doc)))
	for b.Loop() {
		if err := WalkStrings(doc, MaxDepth, fn); err != nil {
			b.Fatalf("WalkStrings = %v, want nil", err)
		}
	}
}

func ExampleWalkStrings() {
	var seen []string
	err := WalkStrings(`{"cmd":["ls","-l\t/tmp"]}`, MaxDepth, func(s string) error {
		seen = append(seen, s)
		return nil
	})
	fmt.Printf("%q %v\n", seen, err)
	// Output: ["cmd" "ls" "-l\t/tmp"] <nil>
}

func TestValidUTF8CostIsLinear(t *testing.T) {
	valid := func(doc []byte) func() {
		return func() {
			if !ValidUTF8(doc, MaxDepth) {
				t.Fatalf("ValidUTF8 of %d bytes = false, want true", len(doc))
			}
		}
	}
	assertCost(t, valid(levelsDocument(costLevels)), valid(levelsDocument(costScale*costLevels)), costScale)
}

func TestWalkStringsCostIsLinear(t *testing.T) {
	walk := func(doc []byte) func() {
		return func() { noError(t, WalkStrings(doc, MaxDepth, func([]byte) error { return nil })) }
	}
	assertCost(t, walk(levelsDocument(costLevels)), walk(levelsDocument(costScale*costLevels)), costScale)
}

// limitTables are the limits tables of the documents: README's and SECURITY's.
func limitTables() map[string]string {
	return map[string]string{"README.md": "## Operational limits", "SECURITY.md": "## Known Limits"}
}

// limitProbes maps the parameter of each row of the limits tables to the tests that
// pin its value at the bound and one past it.
func limitProbes() map[string][]string {
	spill := "TestNameSetResetDropsAMapPastTheNamesItKeeps"
	depth := []string{
		"TestValidUTF8HonorsTheDepthBound", "TestValidUTF8MaxDepthMatchesEncodingJSON",
		"TestWalkTokensHonorsTheDepthBound", "TestIterateObjectHonorsTheDepthBound",
	}
	validator := []string{
		"TestKindStackSpillsOnlyTheLevelsPastItsInlineWords", "TestKindStackGrowsWithTheDepthReached",
		"TestValidUTF8Allocs",
	}
	openValues := []string{"TestDecodeValueAllocs", "TestDecodeValueKeepsTheGoroutineStackFlat"}
	writers := []string{
		"TestBuilderAddValueElementAllocs", "TestValueEncoderIndexKeepsTwoToFourBucketsPerFrame",
		"TestBuilderAddValueElementKeepsTheGoroutineStackFlat",
	}
	probes := map[string][]string{
		"Builder pool":                     {"TestReleaseKeepsBuildersUpToTheLimit"},
		"BatchWriter pool":                 {"TestReleaseBatchWriterKeepsWritersUpToTheLimit"},
		"Validator depth":                  slices.Concat(depth, validator),
		"Validator stack use":              {"TestValidUTF8WalksHugeNestingIteratively"},
		"Validator memory":                 validator,
		"`FlattenObject` depth":            {"TestFlattenObjectWritesLeaves", "TestFlattenObjectCompactsArrayLeaves"},
		"`DecodeValue` open containers":    openValues,
		"`DecodeValue` stack use":          openValues,
		"Decoded strings of `DecodeValue`": {"TestValueDecoderTextCopiesByteInputIntoAChunkOfTheRest"},
		"Value writers open containers":    writers,
		"Value writers stack use":          writers,
		"Value writers depth":              {"TestBuilderAddValueElementWritesNullPastMaxDepth"},
		"`EscapeString`":                   {"TestEscapeStringAllocs"},
		"`WalkTokens` name sets": {
			"TestWalkTokensAllocs", "TestWalkTokensReusesTheSetOfAClosedObject", spill,
		},
		"`FlattenObject` leaf names": {
			"TestFlattenObjectAllocs", "TestFlattenObjectRefusesAnObjectNamedAsAnEarlierLeaf",
		},
		"`FlattenObject` object names": {
			"TestFlattenObjectAllocs", "TestFlattenObjectSpillsOneMapPerLevel",
			"TestPushSetTakesOverTheMapOfAClosedSet", "TestFlattenObjectGivesBackTheRoomOfTheObjectsItLeaves", spill,
		},
		"`IterateDocument` nested name sets": {
			"TestIterateDocumentAllocs", "TestIterateDocumentAllocatesForTheOpenObjectsOnly", spill,
		},
		"Decoded strings of `IterateStringArray` and `WalkStrings`": {
			"TestArenaTokenAtSizesTheFirstChunkToTheRestUpToTheRoom",
			"TestIterateStringArrayDecodesALongDocumentIntoDoublingChunks",
		},
		"`IterateMembers`, `IterateObject` and `IterateDocument` name set": {
			"TestIterateMembersAllocs", "TestIterateObjectAllocs",
			"TestIterateMembersFollowsTheIterateFieldsContract",
		},
	}
	probes["`ValidUTF8`, `WalkStrings`, `WalkTokens`, `IterateObject` and `IterateDocument` depth"] = depth
	probes["Decoded names of `IterateMembers`, `IterateObject`, `IterateDocument`, `FlattenObject` and "+
		"`WalkTokens`"] = []string{
		"TestArenaTokenAtSizesTheFirstChunkToTheRestUpToTheRoom",
		"TestIterateMembersDecodesLongNamesIntoChunksUpToTheRoom",
		"TestIterateDocumentDecodesNestedNamesIntoChunksOfTheirOwn",
		"TestFlattenObjectAllocatesForTheOpenObjectsOnly", "TestWalkTokensKeepsEveryNameIntact",
		"TestFlattenObjectGivesBackTheRoomOfTheObjectsItLeaves",
		"TestFlattenObjectSizesTheChunksOfObjectNamesByTheRestOfData",
	}
	return probes
}

// limitConstants maps each candidate limit constant to the parameter of its row,
// or to "" for one that bounds nothing a caller can reach.
func limitConstants() map[string]string {
	return map[string]string{
		"MaxDepth":            "Validator depth",
		"maxFlattenDepth":     "`FlattenObject` depth",
		"poolMaxRetain":       "Builder pool",
		"batchPoolMaxRetain":  "BatchWriter pool",
		"escapeStackSize":     "`EscapeString`",
		"poolBufferSize":      "",
		"batchPoolBufferSize": "",
		"wordSize":            "",
		"minPlainFloat":       "",
		"maxPlainFloat":       "",
		"shortestIntLimit":    "",
		"minutesPerHour":      "",
		"minutesPerDay":       "",
	}
}

// limitName matches the names M24 takes for limits.
var limitName = regexp.MustCompile(`(?i)^(max|min)|limit|(cap|size|bytes|depth)$`)

// tableRows returns the parameter and the text of each row of the table under
// heading in file.
func tableRows(t *testing.T, file, heading string) map[string]string {
	t.Helper()
	text, err := fs.ReadFile(os.DirFS("."), file)
	if err != nil {
		t.Fatalf("read %s: %v, want the document", file, err)
	}
	_, table, found := strings.Cut(string(text), "\n"+heading+"\n")
	if !found {
		t.Fatalf("%s has no %q, want its limits table", file, heading)
	}
	rows := map[string]string{}
	for k, line := range strings.Split(strings.TrimLeft(table, "\n"), "\n") {
		if !strings.HasPrefix(line, "|") {
			break
		}
		if param, _, _ := strings.Cut(strings.TrimPrefix(line, "| "), " | "); k > 1 {
			rows[param] = line
		}
	}
	return rows
}

// packageDecls parses the Go files of the package and returns the functions of its
// tests and, for each constant of its code, whether it holds a number.
func packageDecls(t *testing.T) (tests, consts map[string]bool) {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("glob *.go = %d files, %v, want some", len(files), err)
	}
	tests, consts = map[string]bool{}, map[string]bool{}
	for _, name := range files {
		f, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v, want Go", name, err)
		}
		for _, decl := range f.Decls {
			if strings.HasSuffix(name, "_test.go") {
				fileFunc(decl, tests)
			} else {
				fileConsts(decl, consts)
			}
		}
	}
	return tests, consts
}

// fileFunc files the name of a function in funcs.
func fileFunc(decl ast.Decl, funcs map[string]bool) {
	if fn, ok := decl.(*ast.FuncDecl); ok {
		funcs[fn.Name.Name] = true
	}
}

// fileConsts files in consts each constant of decl, and whether it holds a number.
func fileConsts(decl ast.Decl, consts map[string]bool) {
	gen, ok := decl.(*ast.GenDecl)
	if !ok || gen.Tok != token.CONST {
		return
	}
	var values []ast.Expr
	for _, spec := range gen.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		if len(vs.Values) > 0 {
			values = vs.Values
		}
		for _, name := range vs.Names {
			consts[name.Name] = !holdsString(values)
		}
	}
}

// holdsString reports whether a string literal appears in exprs.
func holdsString(exprs []ast.Expr) bool {
	found := false
	for _, e := range exprs {
		ast.Inspect(e, func(n ast.Node) bool {
			if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				found = true
			}
			return !found
		})
	}
	return found
}

// checkLimitConstants fails t on a constant that a row of rows names, or a numeric
// one whose name marks a limit, without its entry in limitConstants.
func checkLimitConstants(t *testing.T, rows map[string]string, consts map[string]bool) {
	t.Helper()
	known := limitConstants()
	for name, numeric := range consts {
		named := slices.ContainsFunc(slices.Collect(maps.Values(rows)), func(row string) bool {
			return strings.Contains(row, "`"+name+"`")
		})
		if !named && (!numeric || !limitName.MatchString(name)) {
			continue
		}
		switch param, listed := known[name]; {
		case !listed:
			t.Errorf("the limit candidate %s is in limitConstants neither with a row nor as no limit", name)
		case param != "" && rows[param] == "":
			t.Errorf("the limit constant %s names the row %q, which the limits tables lack", name, param)
		}
	}
	for name := range known {
		if _, declared := consts[name]; !declared {
			t.Errorf("limitConstants lists %s, want a constant of the package", name)
		}
	}
}

func TestMaxDepthAndEveryOtherLimitHaveARowAndAProbe(t *testing.T) {
	rows := map[string]string{}
	for file, heading := range limitTables() {
		maps.Copy(rows, tableRows(t, file, heading))
	}
	tests, consts := packageDecls(t)
	probes := limitProbes()
	for param := range rows {
		if len(probes[param]) == 0 {
			t.Errorf("the limits row %q has no probe, want the tests that pin it", param)
		}
	}
	for param, names := range probes {
		for _, name := range names {
			if !tests[name] || rows[param] == "" {
				t.Errorf("the probe %s of %q is no test of a row, want both", name, param)
			}
		}
	}
	checkLimitConstants(t, rows, consts)
}
