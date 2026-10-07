package jsonfast

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// lineAB is the decoding of the token "a\nb".
const lineAB = "a\nb"

// decodeCase is one string token and its decoding, or ok false.
type decodeCase struct {
	raw  string
	want string
	ok   bool
}

// decodeCases returns string tokens that cover every escape, the surrogate and
// UTF-8 replacement rules and each way a token can be malformed.
func decodeCases() []decodeCase {
	return append(decodedTokens(), malformedTokens()...)
}

// decodedTokens returns tokens that decode, with their decoding.
func decodedTokens() []decodeCase {
	long := strings.Repeat("x", longRun)
	return []decodeCase{
		{quotedEmpty, "", true},
		{quotedHello, "hello", true},
		{`"a\"b"`, `a"b`, true},
		{`"a\\b"`, `a\b`, true},
		{`"a\/b"`, "a/b", true},
		{`"\b\f\n\r\t"`, "\b\f\n\r\t", true},
		{`"\u0041"`, "A", true},
		{`"\u00e9\u00E9"`, "éé", true},
		{`"\u4e2d"`, "中", true},
		{`"\ud83d\ude80"`, "🚀", true},
		{`"\uD83D\uDE80"`, "🚀", true},
		{`"\ufffd"`, replacement, true},
		{quoted(replacement), replacement, true},
		{"\"\\n\xef\xbf\xbd\"", "\n\uFFFD", true},
		{`"\u0000"`, "\x00", true},
		{`"\u0001\u0002"`, "\x01\x02", true},
		{"\"\x7f\"", "\x7f", true},
		{loneHigh, replacement, true},
		{`"\udc00"`, replacement, true},
		{`"\udbff\udfff"`, "\U0010FFFF", true},
		{`"\ud800A"`, "\uFFFDA", true},
		{`"\ud800\u0041"`, "\uFFFDA", true},
		{`"\ud800\ud800\udc00"`, "\uFFFD\U00010000", true},
		{`"\udc00\ud800"`, twoReplacements, true},
		{"\"\\ud800\xf0\x90\x80\x80\"", "\uFFFD\U00010000", true},
		{"\"\xff\"", replacement, true},
		{"\"\x80\\n\"", "\uFFFD\n", true},
		{"\"\xf0\x9f\"", twoReplacements, true},
		{"\"\xc0\x80\"", twoReplacements, true},
		{"\"\xed\xa0\x80\"", "\uFFFD\uFFFD\uFFFD", true},
		{"\"\xf4\x90\x80\x80\"", "\uFFFD\uFFFD\uFFFD\uFFFD", true},
		{"\"caf\xc3\xa9 \xff\\n\"", "café \uFFFD\n", true},
		{"\"\\t日本語 café\xff🚀\\n\"", "\t日本語 café\uFFFD🚀\n", true},
		{quoted(long + `\t`), long + "\t", true},
		{quoted(`\n` + long + `\"` + long), "\n" + long + `"` + long, true},
		{`"\ud800xudc00"`, "\uFFFDxudc00", true},
	}
}

// malformedTokens returns texts that are not one string token.
func malformedTokens() []decodeCase {
	return []decodeCase{
		{`"\u12"`, "", false}, {`"\u123`, "", false}, {`"\ud800\u12"`, "", false},
		{"", "", false},
		{quote, "", false},
		{`"abc`, "", false},
		{`abc"`, "", false},
		{`123`, "", false},
		{litNull, "", false},
		{` "a"`, "", false},
		{`"a" `, "", false},
		{`"a"b"`, "", false},
		{`"\n"x`, "", false},
		{"\"a\x01b\"", "", false},
		{"\"a\nb\"", "", false},
		{"\"\x1f\"", "", false},
		{"\"\\n\x1f\"", "", false},
		{"\"\\n日\x1f\"", "", false},
		{`"\"`, "", false},
		{`"a\"`, "", false},
		{`"abc\`, "", false},
		{`"\x41"`, "", false},
		{`"\'"`, "", false},
		{`"\U0041"`, "", false},
		{`"\u"`, "", false},
		{`"\u004"`, "", false},
		{`"\u004g"`, "", false},
		{`"\uGGGG"`, "", false},
		{`"\ud800\u"`, "", false},
		{`"\ud800\udc0"`, "", false},
		{`"\ud800\`, "", false},
		{`"\ud800\xdc00"`, "", false},
	}
}

func TestDecodeStringViewDecodesByTheEncodingJSONRule(t *testing.T) {
	for _, tc := range decodeCases() {
		got, ok := DecodeStringView(tc.raw)
		if ok != tc.ok || got != tc.want {
			t.Errorf("DecodeStringView(%q) = %q, %v, want %q, %v", tc.raw, got, ok, tc.want, tc.ok)
		}
		gotBytes, ok := DecodeStringView([]byte(tc.raw))
		if ok != tc.ok || string(gotBytes) != tc.want {
			t.Errorf("DecodeStringView([]byte(%q)) = %q, %v, want %q, %v", tc.raw, gotBytes, ok, tc.want, tc.ok)
		}
		if std, stdOK := unmarshalString([]byte(tc.raw)); stdOK != tc.ok || std != tc.want {
			t.Errorf("encoding/json decodes %q to %q, %v; the table says %q, %v", tc.raw, std, stdOK, tc.want, tc.ok)
		}
	}
}

func TestDecodeStringViewAliasesCleanInput(t *testing.T) {
	raw := `"plain ascii and café ©"`
	got, ok := DecodeStringView(raw)
	if !ok || !aliases(got, raw, 1) {
		t.Fatalf("DecodeStringView(%q) = %q, %v, want a substring of the input", raw, got, ok)
	}
	b := []byte(raw)
	v, ok := DecodeStringView(b)
	if !ok || &v[0] != &b[1] || !capped(v) {
		t.Fatalf("DecodeStringView([]byte) = %q (cap %d), %v, want a capped view", v, cap(v), ok)
	}
	v = append(v, 'X')
	if string(b) != raw || string(v) != "plain ascii and café ©X" {
		t.Fatalf("appending to the view gave %q and changed the input to %q, want %q and %q",
			v, b, "plain ascii and café ©X", raw)
	}
}

func TestDecodeStringViewCopiesWhatItDecodes(t *testing.T) {
	b := []byte(`"a\nb"`)
	v, ok := DecodeStringView(b)
	if !ok || string(v) != lineAB || !capped(v) {
		t.Fatalf("DecodeStringView = %q (cap %d), %v, want a capped copy", v, cap(v), ok)
	}
	b[1] = 'Z'
	if string(v) != lineAB {
		t.Fatalf("the decoded value %q followed a write to its input, want %q", v, lineAB)
	}
}

func TestDecodeStringViewKeepsNamedTypes(t *testing.T) {
	got, ok := DecodeStringView(json.RawMessage(`"\u00e9"`))
	if !ok || string(got) != "é" {
		t.Fatalf("DecodeStringView(json.RawMessage) = %q, %v, want é", got, ok)
	}
}

func TestDecodeStringMatchesDecodeStringView(t *testing.T) {
	for _, tc := range decodeCases() {
		if got, ok := DecodeString(tc.raw); ok != tc.ok || got != tc.want {
			t.Errorf("DecodeString(%q) = %q, %v, want %q, %v", tc.raw, got, ok, tc.want, tc.ok)
		}
		if got, ok := DecodeString([]byte(tc.raw)); ok != tc.ok || got != tc.want {
			t.Errorf("DecodeString([]byte(%q)) = %q, %v, want %q, %v", tc.raw, got, ok, tc.want, tc.ok)
		}
	}
}

func TestDecodeStringOwnsWhatByteInputGives(t *testing.T) {
	b := []byte(`"clean"`)
	s, ok := DecodeString(b)
	b[1] = 'X'
	if !ok || s != "clean" {
		t.Fatalf("DecodeString result %q, %v followed a write to its input, want clean, true", s, ok)
	}
	raw := `"clean"`
	if s, _ := DecodeString(raw); !aliases(s, raw, 1) {
		t.Fatalf("DecodeString(%s) = a copy, want a view of the input", raw)
	}
}

func TestDecodeStringAllocs(t *testing.T) {
	clean := `"a clean string that needs nothing"`
	cleanBytes := []byte(clean)
	escaped := []byte(`"line one\nline two \u00e9"`)
	escapedString := string(escaped)
	assertAllocs(t, 0, func() { _, _ = DecodeString(clean) })
	assertAllocs(t, 0, func() { _, _ = DecodeStringView(cleanBytes) })
	assertAllocs(t, 1, func() { _, _ = DecodeString(cleanBytes) })
	assertAllocs(t, 1, func() { _, _ = DecodeString(escaped) })
	assertAllocs(t, 1, func() { _, _ = DecodeString(escapedString) })
	assertAllocs(t, 1, func() { _, _ = DecodeStringView(escaped) })
	long := []byte(`"\n` + strings.Repeat("a", documentedRoom) + `"`)
	assertAllocs(t, 1, func() { _, _ = DecodeString(long) })
}

// unmarshalString decodes raw with encoding/json when raw is exactly one
// string token.
func unmarshalString(raw []byte) (string, bool) {
	var s string
	if len(raw) < 2 || raw[0] != '"' || raw[len(raw)-1] != '"' || json.Unmarshal(raw, &s) != nil {
		return "", false
	}
	return s, true
}

func FuzzDecodeString(f *testing.F) {
	for _, tc := range decodeCases() {
		f.Add([]byte(tc.raw))
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		got, ok := DecodeString(raw)
		fromView, viewOK := DecodeStringView(string(raw))
		if ok != viewOK || got != fromView || !utf8.ValidString(got) {
			t.Fatalf("DecodeString(%q) = %q, %v, want valid UTF-8 and %q, %v as DecodeStringView decodes it",
				raw, got, ok, fromView, viewOK)
		}
		if std, stdOK := unmarshalString(raw); ok != stdOK || got != std {
			t.Fatalf("DecodeString(%q) = %q, %v; encoding/json = %q, %v", raw, got, ok, std, stdOK)
		}
	})
}

// unicodeRepeats is how many times the Unicode benchmark repeats its phrase.
const unicodeRepeats = 8

// stringBenchmarks returns the tokens the string benchmarks decode.
func stringBenchmarks() []struct{ name, raw string } {
	return []struct{ name, raw string }{
		{"Clean", `"The quick brown fox jumps over the lazy dog near the river"`},
		{"Escaped", `"The quick brown fox\njumps over the \"lazy\" dog near the \u00e9"`},
		{"Unicode", quoted(`\n` + strings.Repeat("日本語テスト café ", unicodeRepeats))},
	}
}

// checkString fails b unless decode gives what encoding/json decodes the token
// raw to.
func checkString[T Text](b *testing.B, raw string, decode func() (T, bool)) {
	b.Helper()
	got, ok := decode()
	if want, wantOK := unmarshalString([]byte(raw)); ok != wantOK || string(got) != want {
		b.Fatalf("decoding %s gave %q, %v, want %q, %v", raw, got, ok, want, wantOK)
	}
}

func BenchmarkDecodeStringView(b *testing.B) {
	for _, bc := range stringBenchmarks() {
		raw := []byte(bc.raw)
		checkString(b, bc.raw, func() ([]byte, bool) { return DecodeStringView(raw) })
		b.Run(bc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(raw)))
			for b.Loop() {
				_, _ = DecodeStringView(raw)
			}
		})
	}
}

func BenchmarkDecodeString(b *testing.B) {
	for _, bc := range stringBenchmarks() {
		checkString(b, bc.raw, func() (string, bool) { return DecodeString(bc.raw) })
		b.Run(bc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(bc.raw)))
			for b.Loop() {
				_, _ = DecodeString(bc.raw)
			}
		})
	}
}

func ExampleDecodeStringView() {
	name, _ := DecodeStringView(`"caf\u00e9"`)
	plain, _ := DecodeStringView([]byte(`"plain"`))
	fmt.Println(name, string(plain))
	// Output: café plain
}

func TestArenaTokenAtReportsTheOutcome(t *testing.T) {
	for _, tc := range []struct {
		data  string
		value string
		rest  string
		st    strStatus
	}{
		{`"plain"x`, plainText, "x", strView},
		{`"café"]`, cafe, "]", strView},
		{`"caf\u00e9",`, cafe, ",", strDecoded},
		{"\"\\n\xef\xbf\xbd\"", "\n\uFFFD", "", strDecoded},
		{"\"a\xffb\"]", "a\uFFFDb", "]", strReplaced},
		{loneHigh, "\uFFFD", "", strReplaced},
		{`"a\qb"`, "", `"a\qb"`, strMalformed},
		{"\"a\x01\"", "", "\"a\x01\"", strMalformed},
		{`"\n` + "\x01\"", "", `"\n` + "\x01\"", strMalformed},
		{`"ab`, "", `"ab`, strMalformed},
		{`x"a"`, "", `x"a"`, strMalformed},
		{``, "", "", strMalformed},
	} {
		var a arena
		value, end, st := tokenAt[anyBody](&a, []byte(tc.data), 0, len(tc.data))
		if st != tc.st || tc.data[end:] != tc.rest || string(value) != tc.value {
			t.Errorf("tokenAt(%q, 0) = %q, %d, %d, want %q before %q, %d",
				tc.data, value, end, st, tc.value, tc.rest, tc.st)
		}
	}
}

func TestArenaTokenAtSizesTheFirstChunkToTheRestUpToTheRoom(t *testing.T) {
	token := `"a\nb"`
	for _, rest := range []int{len(token), documentedRoom - 1, documentedRoom, documentedRoom + 1, 2 * documentedRoom} {
		data := []byte("[" + token + strings.Repeat(" ", rest-len(token)))
		var a arena
		value, end, st := tokenAt[anyBody](&a, data, 1, arenaRoom)
		want := min(rest, documentedRoom)
		if string(value) != lineAB || end != 1+len(token) || st != strDecoded || cap(a.buf) != want {
			t.Errorf("tokenAt with %d bytes left = %q, %d, %d in a chunk of %d, want %q, %d, %d in one of %d",
				rest, value, end, st, cap(a.buf), lineAB, 1+len(token), strDecoded, want)
		}
	}
}

func TestArenaTokenAtDecodesTheStringsOfADocumentIntoItsFirstChunk(t *testing.T) {
	var a arena
	tail := strings.Repeat("c", hugeDigits)
	data := []byte(`["a\nb","\u00e9",1,"` + tail + `"]`)
	_, end, _ := tokenAt[anyBody](&a, data, 1, arenaRoom)
	value, _, st := tokenAt[anyBody](&a, data, end+1, arenaRoom)
	if string(value) != "é" || st != strDecoded || cap(a.buf) != documentedRoom || len(a.buf) != len(lineAB+"é") {
		t.Fatalf("a second token = %q, %d with %d of %d arena bytes, want é after %q in one chunk of %d",
			value, st, len(a.buf), cap(a.buf), lineAB, documentedRoom)
	}
	used := len(a.buf)
	clean, _, st := tokenAt[anyBody](&a, data, bytes.Index(data, []byte(tail))-1, arenaRoom)
	if st != strView || len(clean) != len(tail) || len(a.buf) != used {
		t.Fatalf("a clean token of %d bytes = %d with %d arena bytes, want a view and %d",
			len(clean), st, len(a.buf), used)
	}
}

func TestArenaTokenAtMovesAStringThatOutgrowsItsRoom(t *testing.T) {
	var a arena
	firstToken := []byte(`"a\nb"`)
	first, _, _ := tokenAt[anyBody](&a, firstToken, 0, len(firstToken))
	long := strings.Repeat("x", longRun)
	data := []byte(`"\t` + long + `"`)
	value, end, st := tokenAt[anyBody](&a, data, 0, 1)
	if want := "\t" + long; string(value) != want || end != len(data) || cap(a.buf) != len(want) {
		t.Fatalf("a token past the room = %q, %d, %d in a chunk of %d, want %q, %d moved to one of %d",
			value, end, st, cap(a.buf), want, len(data), len(want))
	}
	chunk := cap(a.buf)
	value, _, st = tokenAt[anyBody](&a, []byte(`"\n"`), 0, len(`"\n"`))
	if string(value) != "\n" || cap(a.buf) != 2*chunk {
		t.Fatalf("a token past a full chunk of %d = %q, %d in a chunk of %d, want a doubled chunk",
			chunk, value, st, cap(a.buf))
	}
	if string(first) != lineAB {
		t.Fatalf("moving a later string rewrote the first to %q, want %q", first, lineAB)
	}
	const body = "\n123"
	var fit arena
	fit.buf = make([]byte, 0, len(body))
	token := []byte(`"\n123"`)
	if value, _, _ = tokenAt[anyBody](&fit, token, 0, len(body)); string(value) != body || cap(fit.buf) != len(body) {
		t.Fatalf("a token that fills its room = %q in a chunk of %d, want %q kept in one of %d",
			value, cap(fit.buf), body, len(body))
	}
}

func TestArenaTokenAtViewsACleanTokenThatEndsItsInput(t *testing.T) {
	var a arena
	data := []byte(quotedHello)
	want := quotedHello[1 : len(quotedHello)-1]
	value, end, st := tokenAt[anyBody](&a, data, 0, arenaRoom)
	if st != strView || string(value) != want || end != len(data) || cap(a.buf) != 0 {
		t.Fatalf("tokenAt(%s) = %q, %d, %d in a chunk of %d, want a view of %q, %d, %d and no chunk",
			data, value, end, st, cap(a.buf), want, len(data), strView)
	}
}

func TestArenaTokenAtRejectsWhatIsNoToken(t *testing.T) {
	for _, doc := range []string{`"a\q"`, `"cd`, "\"a\x01b\"", plainText, ``} {
		data := []byte(doc)
		assertAllocs(t, 0, func() {
			var a arena
			value, end, st := tokenAt[anyBody](&a, data, 0, arenaRoom)
			if st != strMalformed || value != nil || end != 0 {
				t.Errorf("tokenAt(%q) = %q, %d, %d, want nil, 0, malformed", doc, value, end, st)
			}
		})
	}
}

func TestArenaGrowKeepsCarvedStringsAndDoublesChunks(t *testing.T) {
	var a arena
	first := a.keep([]byte(lineAB), 0)
	for _, step := range []struct {
		name           string
		n, floor, want func(capacity int) int
	}{
		{"FullChunkDoubles", one, none, func(c int) int { return 2 * c }},
		{"LargeRequestSizesTheChunk", func(c int) int { return 2*c + 1 }, none, func(c int) int { return 2*c + 1 }},
		{"FloorSizesTheChunk", one, func(c int) int { return 2*c + 2 }, func(c int) int { return 2*c + 2 }},
		{"RoomKeepsTheChunk", none, func(c int) int { return 2*c + 2 }, func(c int) int { return c }},
	} {
		capacity := cap(a.buf)
		n := step.n(capacity)
		v := a.grow(a.buf[len(a.buf):], n, step.floor(capacity))
		if got, want := cap(a.buf), step.want(capacity); got != want || len(v) != 0 || cap(v) < n {
			t.Fatalf("%s: grow(%d) with %d of %d used gave a chunk of %d and room for %d, want %d and %d",
				step.name, n, len(a.buf), capacity, got, cap(v), want, n)
		}
		a.carve(append(v, make([]byte, cap(v))...))
		if string(first) != lineAB {
			t.Fatalf("%s: grow(%d) let a write reach an earlier string: %q, want %q", step.name, n, first, lineAB)
		}
	}
	a.buf = a.buf[:1]
	capacity := cap(a.buf)
	if v := a.grow(a.buf[1:], capacity-1, 0); cap(a.buf) != capacity || cap(v) != capacity-1 {
		t.Fatalf("grow of the exact room left gave a chunk of %d for one of %d, want it kept", cap(a.buf), capacity)
	}
}

func TestArenaRewindGivesBackOnlyWhatFollowsTheMark(t *testing.T) {
	var a arena
	kept := a.keep([]byte(lineAB), 2*len(lineAB))
	same := a.mark()
	a.keep([]byte(lineAB), 0)
	a.rewind(same)
	if len(a.buf) != len(lineAB) || cap(a.buf) != 2*len(lineAB) {
		t.Fatalf("rewind in the marked chunk left %d of %d bytes, want %d of %d",
			len(a.buf), cap(a.buf), len(lineAB), 2*len(lineAB))
	}
	later := a.mark()
	a.keep([]byte(plainText+plainText), 0)
	capacity := cap(a.buf)
	a.rewind(later)
	if len(a.buf) != 0 || cap(a.buf) != capacity || capacity == later.capacity || string(kept) != lineAB {
		t.Fatalf("rewind past a later chunk left %d of %d bytes and %q, want 0 of a new chunk of %d and %q",
			len(a.buf), cap(a.buf), kept, capacity, lineAB)
	}
}

func TestArenaGrowMovesWhatTheStringHolds(t *testing.T) {
	var a arena
	a.buf = make([]byte, 0, len(plainText)+1)
	held := append(a.grow(a.buf, len(plainText), 0), plainText...)
	moved := a.grow(held, 2, 0)
	if want := 2 * (len(plainText) + 1); string(moved) != plainText || cap(moved) != want || cap(a.buf) != want ||
		len(a.buf) != 0 {
		t.Fatalf("grow(2) of %q with 1 byte of room = %q in a chunk of %d (%d carved), want %q in a new one of %d",
			held, moved, cap(a.buf), len(a.buf), plainText, want)
	}
}

// one and none are sizes for TestArenaGrowKeepsCarvedStringsAndDoublesChunks that
// ignore the capacity: one byte and nothing.
func one(int) int { return 1 }

func none(int) int { return 0 }

func TestArenaTokenAtDropsTheMalformedValue(t *testing.T) {
	var a arena
	data := `"ab\q"`
	v, end, st := tokenAt[anyBody](&a, []byte(data), 0, len(data))
	if v != nil || end != 0 || st != strMalformed {
		t.Fatalf("tokenAt = %q, %d, %d, want nil, 0, malformed", v, end, st)
	}
}

func TestArenaStrictTokenStopsAtTheFirstRuneWithNoCodePoint(t *testing.T) {
	tail := strings.Repeat(`\n`, longRun)
	refused := []string{quoted(`\n` + "\xff" + tail), quoted(`\n\ud800` + tail), quoted(`\n\udc00\ud800` + tail)}
	for _, raw := range refused {
		var a arena
		if value, end, ok := a.strictToken([]byte(raw), 0); ok || value != nil || end != 0 || len(a.buf) != 0 {
			t.Errorf("strictToken(%.12q) = %q, %d, %v and carved %d bytes, want nil, 0, false and none",
				raw, value, end, ok, len(a.buf))
		}
		value, end, st := tokenAt[anyBody](&a, []byte(raw), 0, len(raw))
		if st != strReplaced || end != len(raw) || !strings.HasSuffix(string(value), strings.Repeat("\n", longRun)) {
			t.Errorf("tokenAt[anyBody](%.12q) = %d bytes, %d, %d, want the whole token, %d, replaced",
				raw, len(value), end, st, len(raw))
		}
	}
}

func TestArenaStrictTokenCopiesNothingBeforeALateFault(t *testing.T) {
	data := []byte(refusedLate())
	assertAllocs(t, 0, func() {
		var a arena
		if value, end, ok := a.strictToken(data, 0); ok || value != nil || end != 0 {
			t.Fatalf("strictToken(%.12q) = %q, %d, %v, want nil, 0, false", data, value, end, ok)
		}
	})
}

func TestEscapeAtDecodesEveryLetter(t *testing.T) {
	letters := map[byte]byte{'"': '"', '\\': '\\', '/': '/', 'b': '\b', 'f': '\f', 'n': '\n', 'r': '\r', 't': '\t'}
	for n := range math.MaxUint8 + 1 {
		c := byte(n)
		want, ok := letters[c]
		if got := unescapeByte(c); got != want || (got != 0) != ok {
			t.Errorf("unescapeByte(%q) = %q, want %q", c, got, want)
		}
		wantRune, wantSize := malformedRune, 1
		if ok {
			wantRune, wantSize = rune(want), len(`\n`)
		}
		if r, size := escapeAt([]byte{'\\', c}, 0); c != 'u' && (r != wantRune || size != wantSize) {
			t.Errorf("escapeAt(\\%c) = %q, %d, want %q, %d", c, r, size, wantRune, wantSize)
		}
	}
}

func TestHexDigitMatchesStrconv(t *testing.T) {
	for n := range math.MaxUint8 + 1 {
		c := byte(n)
		want, err := strconv.ParseInt(string([]byte{c}), hexRadix, intBits)
		if got, ok := hexDigit(c); !sameScalar(int64(got), ok, want, err == nil) {
			t.Errorf("hexDigit(%q) = %d, %v; strconv = %d, %v", c, got, ok, want, err)
		}
	}
}

func TestEqualStringComparesDecodedContent(t *testing.T) {
	for _, tc := range decodeCases() {
		if got := EqualString(tc.raw, tc.want); got != tc.ok {
			t.Errorf("EqualString(%q, %q) = %v, want %v", tc.raw, tc.want, got, tc.ok)
		}
		if !tc.ok {
			continue
		}
		for _, other := range []string{tc.want + "!", "?" + tc.want, tc.want + "\x00", tc.want + quote} {
			if EqualString(tc.raw, other) {
				t.Errorf("EqualString(%q, %q) = true, want false", tc.raw, other)
			}
		}
		if n := len(tc.want); n > 0 && EqualString(tc.raw, tc.want[:n-1]) {
			t.Errorf("EqualString(%q, %q) = true for a prefix, want false", tc.raw, tc.want[:n-1])
		}
	}
}

func TestEqualStringRejectsWhatIsNotItsDecoding(t *testing.T) {
	long := strings.Repeat("abcdefgh", wordSize/2)
	for _, tc := range []struct{ raw, s string }{
		{"\"\xff\"", "\xff"},
		{loneHigh, "\xed\xa0\x80"},
		{"\"a\xffb\"", "a\xffb"},
		{`xab"`, "ab"},
		{`x`, ""},
		{``, ""},
		{quote, ""},
		{`"ab`, "ab"},
		{`"xy`, "xyz"},
		{`"a\qb"`, "aqb"},
		{`"\u00e9"`, "e"},
		{`"\u00e9"`, "é!"},
		{`"café"`, "cafe"},
		{`"café"`, "caf\xc3"},
		{"\"a\x01\"", "a\x01"},
		{"\"\x01\"", replacement},
		{`"\plain"`, replacement + plainText},
		{quoted(long), long[:len(long)-1] + "X"},
		{quoted("X" + long[1:]), long},
		{quoted(long), long + "a"},
		{quoted(long + "a"), long},
		{quoted(long[:len(long)/2] + quote + long[len(long)/2:]), long},
		{`"` + long + `" `, long},
		{`"a\nbcdefgh"`, `a\nbcdefgh`},
	} {
		if EqualString(tc.raw, tc.s) {
			t.Errorf("EqualString(%q, %q) = true, want false", tc.raw, tc.s)
		}
	}
	for _, tc := range []struct{ raw, s string }{
		{`"` + long[:len(long)-1] + `\u0068"`, long}, {`"a\nbcdefgh"`, "a\nbcdefgh"},
	} {
		if !EqualString(tc.raw, tc.s) {
			t.Errorf("EqualString(%q, %q) = false, want true for an escape inside a run of words", tc.raw, tc.s)
		}
	}
}

func TestEqualStringRejectsRawControlBytes(t *testing.T) {
	for c := range byte(' ') {
		for _, body := range []string{string([]byte{c}), plainText + cafe + string([]byte{c})} {
			if EqualString(`"`+body+`"`, body) {
				t.Errorf("EqualString of the raw control byte %#02x = true, want false", c)
			}
		}
	}
}

func TestEqualStringAllocs(t *testing.T) {
	raw := []byte(`"caf\u00e9 \ud83d\ude80 and a long clean tail 日本語"`)
	assertAllocs(t, 0, func() { _ = EqualString(raw, "café 🚀 and a long clean tail 日本語") })
}

func FuzzEqualString(f *testing.F) {
	for _, tc := range decodeCases() {
		f.Add([]byte(tc.raw), tc.want)
	}
	f.Fuzz(func(t *testing.T, raw []byte, s string) {
		decoded, ok := unmarshalString(raw)
		if got, want := EqualString(raw, s), ok && decoded == s; got != want {
			t.Fatalf("EqualString(%q, %q) = %v; encoding/json decodes %q, %v", raw, s, got, decoded, ok)
		}
		if ok && !EqualString(string(raw), decoded) {
			t.Fatalf("EqualString(%q, %q) = false for its own decoding, want true", raw, decoded)
		}
	})
}

func BenchmarkEqualString(b *testing.B) {
	for _, bc := range stringBenchmarks() {
		raw := []byte(bc.raw)
		decoded, ok := unmarshalString(raw)
		if !ok || !EqualString(raw, decoded) {
			b.Fatalf("EqualString(%s, %q) = false for the encoding/json decoding, want true", raw, decoded)
		}
		b.Run(bc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(raw)))
			for b.Loop() {
				_ = EqualString(raw, decoded)
			}
		})
	}
}

func ExampleEqualString() {
	fmt.Println(EqualString(`"\u0041BC"`, "ABC"), EqualString(`"ABC"`, "abc"))
	// Output: true false
}

func TestMatchedPrefixStopsPastTheRunThatDiffers(t *testing.T) {
	for _, tc := range []struct {
		token, s, rest string
		equal          bool
	}{
		{`"abcdefgh12345678"`, "w", quote, false},
		{`"abcdefgh12345678\n"`, "w", `\n"`, false},
		{`"cd"`, "cd", quote, true},
		{`"c\nd"`, "c\n", quote, false},
		{`"c\nd"`, "c\nd", quote, true},
		{`"cd`, "cd", "", false},
		{"\"g\x01\"", "g", "\x01\"", false},
		{`"c\nd"`, "cx", `d"`, false},
		{`"e\qf"`, "e", `\qf"`, false},
		{"\"e\xfff\"", "e", `f"`, false},
		{`"\ud83d\ude00"`, "\U0001F600", quote, true},
		{`"\ud83d\ude00"`, "\U0001F601", quote, false},
	} {
		want := len(tc.token) - len(tc.rest)
		if stop, equal := matchedPrefix([]byte(tc.token), 1, []byte(tc.s)); stop != want || equal != tc.equal {
			t.Errorf("matchedPrefix(%q, %q) = %d, %t, want %d, %t (matchName scans a name once)",
				tc.token, tc.s, stop, equal, want, tc.equal)
		}
	}
}
