package jsonfast

import (
	"bytes"
	"encoding/json"
	"math"
	"slices"
	"strings"
	"testing"
)

// ctrlTail is a raw control byte and a closing quote, which no string holds.
const ctrlTail = "\x01\""

func TestSkipWSStopsAtTheFirstOtherByte(t *testing.T) {
	for _, tc := range []struct {
		data string
		i    int
		rest string
	}{
		{"  " + plainText, 0, plainText},
		{"\t\n\r x", 0, "x"},
		{plainText, 0, plainText},
		{"a  b", 1, "b"},
		{blank, 1, ""},
		{"", 0, ""},
		{"\v\f", 0, "\v\f"},
	} {
		if got := skipWS([]byte(tc.data), tc.i); tc.data[got:] != tc.rest {
			t.Errorf("skipWS(%q, %d) = %d, want the index of %q", tc.data, tc.i, got, tc.rest)
		}
	}
}

func TestSkipWSSkipsExactlyTheFourWhitespaceBytes(t *testing.T) {
	for n := range math.MaxUint8 + 1 {
		c := byte(n)
		want := 0
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			want = 1
		}
		if got := skipWS([]byte{c}, 0); got != want {
			t.Errorf("skipWS(%q) = %d, want %d", c, got, want)
		}
	}
}

func FuzzSkipWS(f *testing.F) {
	for _, s := range []string{"  x", "\t\n\r ", "\v x", "", plainText} {
		f.Add([]byte(s), 0)
	}
	f.Fuzz(func(t *testing.T, data []byte, i int) {
		i = min(max(i, 0), len(data))
		if got, want := skipWS(data, i), len(data)-len(bytes.TrimLeft(data[i:], jsonSpace)); got != want {
			t.Fatalf("skipWS(%q, %d) = %d, want %d", data, i, got, want)
		}
		want := bytes.Trim(data[i:], jsonSpace)
		if got := TrimWS(data[i:]); !bytes.Equal(got, want) || !capped(got) {
			t.Fatalf("TrimWS(%q) = %q (cap %d), want %q", data[i:], got, cap(got), want)
		}
		if got := TrimWS(string(data[i:])); got != string(want) {
			t.Fatalf("TrimWS(%q) = %q, want %q", data[i:], got, want)
		}
	})
}

func TestTrimWSDropsOnlyJSONWhitespace(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"", ""}, {blank, ""}, {" \t\r\n{} \n", emptyObject}, {plainText, plainText}, {" " + plainText, plainText},
		{plainText + "\r", plainText},
		{"\v x \f", "\v x \f"}, {"\n a b\t", "a b"},
	} {
		if got := TrimWS(tc.in); got != tc.want {
			t.Errorf("TrimWS(%q) = %q, want %q", tc.in, got, tc.want)
		}
		if got := TrimWS([]byte(tc.in)); string(got) != tc.want || !capped(got) {
			t.Errorf("TrimWS([]byte(%q)) = %q with capacity %d, want %q capped", tc.in, got, cap(got), tc.want)
		}
	}
	data := " {} "
	if got := TrimWS(data); !aliases(got, data, 1) {
		t.Errorf("TrimWS(%q) = %q, want a view of its input", data, got)
	}
	assertAllocs(t, 0, func() { _ = TrimWS(data) })
}

func TestSkipValueAtFailsAtTheEnd(t *testing.T) {
	data := []byte(" \t")
	end := len(data)
	valueStop, valueOK := skipValueAt(data, end)
	strStop, strOK := skipStringAt(data, end)
	if skipWS(data, end) != end || valueStop != 0 || valueOK || strStop != 0 || strOK {
		t.Errorf("at the end of %q: skipWS = %d, skipValueAt = %d, %v, skipStringAt = %d, %v; want %d, 0, false",
			data, skipWS(data, end), valueStop, valueOK, strStop, strOK, end)
	}
}

func TestSkipValueAtReturnsTheIndexPastTheValue(t *testing.T) {
	for _, tc := range []struct {
		data, rest string
		ok         bool
	}{
		{quotedHello, "", true},
		{`123`, "", true},
		{`-42`, "", true},
		{`0.5`, "", true},
		{`1.5e10`, "", true},
		{`1E+5`, "", true},
		{`2e-3`, "", true},
		{litTrue, "", true},
		{litFalse, "", true},
		{litNull, "", true},
		{objectA1, "", true},
		{twoArray, "", true},
		{`false,next`, ",next", true},
		{`  7`, "", true},
		{`00`, "0", true},
		{`01`, "1", true},
		{`truex`, "x", true},
		{`[{]`, "", true},
		{``, "", false},
		{blank, "", false},
	} {
		want := len(tc.data) - len(tc.rest)
		if !tc.ok {
			want = 0
		}
		if end, ok := skipValueAt([]byte(tc.data), 0); end != want || ok != tc.ok {
			t.Errorf("skipValueAt(%q, 0) = %d, %v, want %d, %v", tc.data, end, ok, want, tc.ok)
		}
	}
	if data := "[1] 7"; !skipsToTheEnd(data, len(oneArray)) {
		t.Errorf("skipValueAt(%q, %d) stopped short, want %d, true", data, len(oneArray), len(data))
	}
}

func TestSkipValueAtRefusesAPartialScalar(t *testing.T) {
	for _, data := range []string{`.25`, `-`, `--1`, `2.`, `2.e3`, `2e`, `1e+`, `tru`, `fals`, `nul`, `tree`, `}`} {
		if end, ok := skipValueAt([]byte(data), 0); end != 0 || ok {
			t.Errorf("skipValueAt(%q, 0) = %d, %v, want 0, false", data, end, ok)
		}
	}
	for _, doc := range []string{`[tru`, `[fals`, `[nul`} {
		data := []byte(doc)
		if end, ok := skipValueAt(slices.Clip(data), 1); end != 0 || ok {
			t.Errorf("skipValueAt(%q, 1) = %d, %v, want 0, false", doc, end, ok)
		}
	}
}

// skipsToTheEnd reports whether skipValueAt from data[i] reaches the end.
func skipsToTheEnd(data string, i int) bool {
	end, ok := skipValueAt([]byte(data), i)
	return ok && end == len(data)
}

func FuzzSkipValueAt(f *testing.F) {
	for _, s := range []string{
		`"a"`, `-1.5e3`, `{"a":[1]}`, `[1,{"b":2}]`, `truex`, `01`, `"\q"`, `1.`, `1.5e`, `0.e1`, `[{]`, " 7 x",
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		end, ok := skipValueAt(data, 0)
		if want := refValue(data, refSpace(data, 0)); ok != (want >= 0) || end != max(want, 0) {
			t.Fatalf("skipValueAt(%q) = %d, %v; the shallow grammar ends the value at %d", data, end, ok, want)
		}
		if want := len(bytes.TrimRight(data, jsonSpace)); json.Valid(data) && (!ok || end != want) {
			t.Fatalf("skipValueAt(%q) = %d, %v, want %d, true", data, end, ok, want)
		}
	})
}

func BenchmarkTrimWS(b *testing.B) {
	data := []byte(" \t\r\n" + objectA1 + "\n\t\r ")
	if got := TrimWS(data); string(got) != objectA1 {
		b.Fatalf("TrimWS(%q) = %q, want %q", data, got, objectA1)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		_ = TrimWS(data)
	}
}

// BenchmarkTrimWSBytesTrim trims the input of BenchmarkTrimWS with bytes.Trim and
// the four JSON whitespace bytes, which TrimWS stands in for.
func BenchmarkTrimWSBytesTrim(b *testing.B) {
	data := []byte(" \t\r\n" + objectA1 + "\n\t\r ")
	if got := bytes.Trim(data, jsonSpace); string(got) != objectA1 {
		b.Fatalf("bytes.Trim(%q) = %q, want %q", data, got, objectA1)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		_ = bytes.Trim(data, jsonSpace)
	}
}

func BenchmarkSkipValueAt(b *testing.B) {
	data := []byte(`  {"k":[1,2,{"x":"y \" z"}],"n":-12.5e3,"t":true,"s":"tail"}`)
	if end, ok := skipValueAt(data, 0); end != len(data) || !ok {
		b.Fatalf("skipValueAt(%s, 0) = %d, %v, want %d, true", data, end, ok, len(data))
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		_, _ = skipValueAt(data, 0)
	}
}

func TestSkipStringAtReturnsTheIndexPastTheQuote(t *testing.T) {
	long := strings.Repeat("s", longRun)
	for _, tc := range []struct {
		data, rest string
		ok         bool
	}{
		{quotedHello, "", true},
		{`"he\"lo"`, "", true},
		{`"a\\b"`, "", true},
		{quotedEmpty, "", true},
		{`"a"bc`, "bc", true},
		{`"\b\f\n\r\t\/\\\""`, "", true},
		{`"\u00e9\u00E9"`, "", true},
		{`"\uD800"`, "", true},
		{`"\udc00x"`, "", true},
		{"\"\xff\x7f\"", "", true},
		{`"12345678"`, "", true},
		{`"aaaaaaaa"bbbbbbb`, "bbbbbbb", true},
		{quoted(`ab\"` + long), "", true},
		{quoted(`\\\\\\\\` + long + `\t` + long), "", true},
		{quoted(long + `\n`), "", true},
		{`"unterminated`, "", false},
		{plainText, "", false},
		{``, "", false},
		{"\"ctrl" + ctrlTail, "", false},
		{quoted(long + "\x05"), "", false},
		{`"\`, "", false},
		{`"` + long + `\`, "", false},
		{`"a\t` + long + ctrlTail, "", false},
		{`"\q"`, "", false},
		{`"\x41"`, "", false},
		{`"\u12"`, "", false},
		{`"\u123`, "", false},
		{`"\ud800\u12"`, "", false},
		{`"\u0041`, "", false},
		{`"\u12g4"`, "", false},
		{`"\U0041"`, "", false},
	} {
		// A slice with no room past its end panics on any read past it.
		want := len(tc.data) - len(tc.rest)
		if !tc.ok {
			want = 0
		}
		if end, ok := skipStringAt(slices.Clip([]byte(tc.data)), 0); end != want || ok != tc.ok {
			t.Errorf("skipStringAt(%.40q, 0) = %d, %v, want %d, %v", tc.data, end, ok, want, tc.ok)
		}
	}
	if data := `["a"]`; !skipsNothingAtTheEnd(data) {
		t.Errorf("skipStringAt at the end of %q = ok, want 0, false", data)
	}
}

// skipsNothingAtTheEnd reports whether skipStringAt at len(data) fails.
func skipsNothingAtTheEnd(data string) bool {
	end, ok := skipStringAt([]byte(data), len(data))
	return !ok && end == 0
}

func FuzzSkipStringAt(f *testing.F) {
	for _, s := range []string{quotedHello, `"he\"llo"`, `"a\\b"`, quotedEmpty, "\"c\x01\"", `"\q"`, `"\u00e9"`, `"x`} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		end, ok := skipStringAt(data, 0)
		if want := refString(data, 0); ok != (want >= 0) || end != max(want, 0) {
			t.Fatalf("skipStringAt(%q) = %d, %v; the string grammar ends the token at %d", data, end, ok, want)
		}
		if ok && !json.Valid(data[:end]) {
			t.Fatalf("skipStringAt(%q) = %d, which ends %q, want the end of a JSON string", data, end, data[:end])
		}
	})
}

func BenchmarkSkipStringAt(b *testing.B) {
	data := []byte(`"escaped \" and long tail abcdefghijklmnopqrstuvwxyz0123456789"`)
	if end, ok := skipStringAt(data, 0); end != len(data) || !ok {
		b.Fatalf("skipStringAt(%s, 0) = %d, %v, want %d, true", data, end, ok, len(data))
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		_, _ = skipStringAt(data, 0)
	}
}

func TestSkipBracedCountsOnlyItsOwnPair(t *testing.T) {
	longKey := `{"` + strings.Repeat("n", longRun) + `":{"x":[1,2]}}`
	for _, tc := range []struct {
		data, rest string
		ok         bool
	}{
		{`{"a":"b"}`, "", true},
		{`{"nested":{"x":1}}`, "", true},
		{`[1,2,3]`, "", true},
		{`[[1],[[2]]]!`, "!", true},
		{`[[1,2,3,4,5,6,7,8]]`, "", true},
		{`{"s":"with\"quote}"}`, "", true},
		{`{"a":[}`, "", true},
		{`[{]`, "", true},
		{`{[}]`, "]", true},
		{longKey, "", true},
		{`{unclosed`, "", false},
		{`{`, "", false},
		{`{"key":"unterminated`, "", false},
		{`{"k":"\q"}`, "", false},
		{`{"` + strings.Repeat("n", longRun) + `":[1]`, "", false},
	} {
		want := len(tc.data) - len(tc.rest)
		if !tc.ok {
			want = 0
		}
		if end, ok := skipBraced([]byte(tc.data), 0); end != want || ok != tc.ok {
			t.Errorf("skipBraced(%q) = %d, %v, want %d, %v", tc.data, end, ok, want, tc.ok)
		}
	}
}

func FuzzSkipBraced(f *testing.F) {
	for _, s := range []string{`{"a":"b"}`, `{"nested":{"x":1}}`, `[1,2,3]`, `{unclosed`, `[{"]":1}]`, `[1,"\q"]`} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) == 0 || (data[0] != '{' && data[0] != '[') {
			return
		}
		end, ok := skipBraced(data, 0)
		if want := refBraced(data, 0); ok != (want >= 0) || end != max(want, 0) {
			t.Fatalf("skipBraced(%q) = %d, %v; counting only its own pair ends it at %d", data, end, ok, want)
		}
		if want := len(bytes.TrimRight(data, jsonSpace)); json.Valid(data) && (!ok || end != want) {
			t.Fatalf("skipBraced(%q) = %d, %v, want %d, true", data, end, ok, want)
		}
	})
}

func TestBracedRunStopsAtTheFirstQuoteOpenerOrCloser(t *testing.T) {
	keep := func(c byte) bool { return c != '"' && c != '{' && c != '}' }
	for _, data := range runInputs(`"{}[\`) {
		for start := 0; start <= len(data); start++ {
			if got, want := bracedRun(data, start, '{'), firstMatch(data, start, keep); got != want {
				t.Fatalf("bracedRun(%q, %d) = %d, want %d", data, start, got, want)
			}
		}
	}
}

func TestSkipValueAtAllocs(t *testing.T) {
	doc := []byte(`  {"k":[1,2,{"x":"y \" z"}],"n":-12.5e3}  `)
	assertAllocs(t, 0, func() {
		_, _ = skipValueAt(doc, 0)
		_, _ = skipBraced(doc, 2)
		_, _ = skipStringAt(doc, 3)
	})
}

func BenchmarkSkipBraced(b *testing.B) {
	data := []byte(`{"facility":23,"tags":["a","b"],"nested":{"x":{"y":"z \" }"}},"end":true}`)
	if end, ok := skipBraced(data, 0); end != len(data) || !ok {
		b.Fatalf("skipBraced(%s, 0) = %d, %v, want %d, true", data, end, ok, len(data))
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		_, _ = skipBraced(data, 0)
	}
}
