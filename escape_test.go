package jsonfast

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"
	"unicode/utf8"
)

// hostName is a short plain field value.
const hostName = "fw01"

// escapeCases returns raw strings paired with their escaped form.
func escapeCases() []struct{ in, want string } {
	long := strings.Repeat("a", longRun)
	return []struct{ in, want string }{
		{"", ""},
		{plainText, plainText},
		{`say "hi"`, `say \"hi\"`},
		{`C:\dir`, `C:\\dir`},
		{"\b\f\n\r\t", `\b\f\n\r\t`},
		{"\x00\x01\x1f", `\u0000\u0001\u001f`},
		{"\x7f/<>&", "\x7f/<>&"},
		{"caf\xc3\xa9 日本 🚀", "café 日本 🚀"},
		{"\u2028", "\u2028"},
		{"\xff", "\uFFFD"},
		{"a\uFFFDb", "a\uFFFDb"},
		{"\xf0\x9f", twoReplacements},
		{"\xe0\x80", twoReplacements},
		{"\xc0\x80", twoReplacements},
		{"\xed\xa0\x80", "\uFFFD\uFFFD\uFFFD"},
		{"\xf4\x90\x80\x80", "\uFFFD\uFFFD\uFFFD\uFFFD"},
		{"ok\xffmixed\xc2", "ok\uFFFDmixed\uFFFD"},
		{long + "\n" + long, long + `\n` + long},
		{"1234567\n", `1234567\n`},
		{"12345678\x01", `12345678\u0001`},
	}
}

func TestBuilderAppendEscapedEscapesEveryByteClass(t *testing.T) {
	for _, tc := range escapeCases() {
		b := New(1)
		b.AppendEscaped([]byte(tc.in))
		s := New(1)
		s.AppendEscapedString(tc.in)
		if string(b.Bytes()) != tc.want || string(s.Bytes()) != tc.want {
			t.Errorf("AppendEscaped(%q) = %q, AppendEscapedString = %q, want %q", tc.in, b.Bytes(), s.Bytes(), tc.want)
		}
		if got := EscapeString(tc.in); got != tc.want {
			t.Errorf("EscapeString(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestBuilderAppendEscapedStringDecodesBackOnEveryByte(t *testing.T) {
	for n := range math.MaxUint8 + 1 {
		in := string([]byte{'a', byte(n), 'b'})
		b := New(1)
		b.buf = append(b.buf, '"')
		b.AppendEscapedString(in)
		b.buf = append(b.buf, '"')
		var got string
		if err := json.Unmarshal(b.Bytes(), &got); err != nil || got != strings.ToValidUTF8(in, "\uFFFD") {
			t.Errorf("escaping %q wrote %s, which decodes to %q, %v, want %q", in, b.Bytes(), got, err,
				strings.ToValidUTF8(in, "\uFFFD"))
		}
	}
}

func TestEscapeStringReturnsCleanInputItself(t *testing.T) {
	for _, s := range []string{"hello-world-no-special-chars-here", cafe, "日本語 🚀 /<>&'", "\u2028"} {
		if got := EscapeString(s); !aliases(got, s, 0) {
			t.Errorf("EscapeString(%q) copied a string that needs no escaping, want the string itself", s)
		}
	}
	if got := EscapeString(""); got != "" {
		t.Fatalf("EscapeString(\"\") = %q, want \"\"", got)
	}
}

// stackOutput is how many bytes of output EscapeString builds on the stack.
const stackOutput = 512

func TestEscapeStringAllocs(t *testing.T) {
	plain := strings.Repeat(plainText+" ", longRun/len(plainText))
	fits := strings.Repeat("a", stackOutput-len(`\n`)) + "\n"
	grows := strings.Repeat("a", stackOutput-1) + "\n"
	assertAllocs(t, 0, func() { _ = EscapeString(plain) })
	assertAllocs(t, 0, func() { _ = EscapeString(cafe + " 日本語") })
	assertAllocs(t, 1, func() { _ = EscapeString(fits) })
	assertAllocs(t, 2, func() { _ = EscapeString(grows) })
	if got := EscapeString(grows); got != grows[:len(grows)-1]+`\n` {
		t.Fatalf("EscapeString past the stack = %q, want %q", got, grows[:len(grows)-1]+`\n`)
	}
}

func TestBuilderAppendEscapedAllocs(t *testing.T) {
	p := []byte("tools/call result with \"quotes\"\nand café")
	b := New(0)
	assertAllocs(t, 0, func() {
		b.Reset()
		b.AppendRawString(`{"text":"`)
		b.AppendEscaped(p)
		b.AppendRawString(`"}`)
		b.AppendEscapedString("more\ttext")
	})
}

// escapedBody reports whether escaped is a JSON string body that decodes to
// what encoding/json decodes the marshaled s to.
func escapedBody(escaped []byte, s string) bool {
	var got string
	want, err := marshalDecoded(s)
	return err == nil && utf8.Valid(escaped) &&
		json.Unmarshal([]byte(`"`+string(escaped)+`"`), &got) == nil && got == want
}

// needsNoEscape reports whether JSON holds s as it is: valid UTF-8 with no
// control byte, quote or backslash.
func needsNoEscape(s string) bool {
	escaped := func(r rune) bool { return r < ' ' || r == '"' || r == '\\' }
	return utf8.ValidString(s) && !strings.ContainsFunc(s, escaped)
}

func FuzzEscapeString(f *testing.F) {
	for _, tc := range escapeCases() {
		f.Add(tc.in)
	}
	f.Fuzz(func(t *testing.T, s string) {
		escaped := EscapeString(s)
		if !escapedBody([]byte(escaped), s) {
			t.Fatalf("EscapeString(%q) = %q, want the body of a JSON string of s", s, escaped)
		}
		strictlyRead(t, []byte(quoted(escaped)))
		if needsNoEscape(s) && s != "" && !aliases(escaped, s, 0) {
			t.Fatalf("EscapeString(%q) copied a string that needs no escaping, want the string itself", s)
		}
	})
}

func FuzzBuilderAppendEscaped(f *testing.F) {
	for _, tc := range escapeCases() {
		f.Add([]byte(tc.in))
	}
	f.Fuzz(func(t *testing.T, p []byte) {
		b := New(1)
		b.AppendRawString("x")
		b.AppendEscaped(p)
		if b.Bytes()[0] != 'x' || !escapedBody(b.Bytes()[1:], string(p)) {
			t.Fatalf("AppendEscaped(%q) = %q, want x and the body of a JSON string of p", p, b.Bytes())
		}
		strictlyRead(t, []byte(quoted(string(b.Bytes()[1:]))))
		s := New(1)
		s.AppendRawString("x")
		s.AppendEscapedString(string(p))
		if !bytes.Equal(b.Bytes(), s.Bytes()) {
			t.Fatalf("AppendEscaped(%q) = %q, want %q as AppendEscapedString writes", p, b.Bytes(), s.Bytes())
		}
	})
}

// escapeBenchmarks returns the named texts the escaping benchmarks write.
func escapeBenchmarks() []struct{ name, s string } {
	return []struct{ name, s string }{
		{"Short", hostName},
		{"Field", "webserver-prod-01"},
		{"ASCII", "This is a typical syslog message with hostname=myhost severity=info facility=local0"},
		{"Escapes", "Message with \"quotes\" and \\backslash and\nnewline and\ttab characters"},
		{"Unicode", "Message with unicode: 日本語テスト and emojis 🎉🔥 mixed with ASCII text"},
		{"Long", strings.Repeat("tool output line with a tab\t and more ASCII text ", wordSize*wordSize)},
	}
}

// jsonEscaped returns s as encoding/json writes it in a string without HTML
// escapes, quotes dropped.
func jsonEscaped(tb testing.TB, s string) string {
	tb.Helper()
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	noError(tb, enc.Encode(s))
	encoded := strings.TrimSpace(out.String())
	return encoded[len(quote) : len(encoded)-len(quote)]
}

func BenchmarkBuilderAppendEscaped(b *testing.B) {
	for _, bc := range escapeBenchmarks() {
		p := []byte(bc.s)
		b.Run(bc.name, func(b *testing.B) {
			benchWrite(b, jsonEscaped(b, bc.s), func(builder *Builder) { builder.AppendEscaped(p) })
		})
	}
}

func BenchmarkBuilderAppendEscapedString(b *testing.B) {
	for _, bc := range escapeBenchmarks() {
		b.Run(bc.name, func(b *testing.B) {
			benchWrite(b, jsonEscaped(b, bc.s), func(builder *Builder) { builder.AppendEscapedString(bc.s) })
		})
	}
}

func BenchmarkEscapeString(b *testing.B) {
	for _, bc := range []struct{ name, s string }{
		{"Plain", "This is a typical syslog message with no special characters at all"},
		{"Escapes", "Message with \"quotes\" and \\backslash and\nnewline"},
	} {
		b.Run(bc.name, func(b *testing.B) {
			if got, want := EscapeString(bc.s), jsonEscaped(b, bc.s); got != want {
				b.Fatalf("EscapeString(%q) = %q, want %q", bc.s, got, want)
			}
			b.ReportAllocs()
			b.SetBytes(int64(len(bc.s)))
			for b.Loop() {
				_ = EscapeString(bc.s)
			}
		})
	}
}

func ExampleEscapeString() {
	fmt.Println(EscapeString(`She said "hello"` + "\n"))
	// Output: She said \"hello\"\n
}
