package jsonfast

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestBytesOfAliasesEveryShape(t *testing.T) {
	s := strings.Repeat("shared text ", longRun)
	var got []byte
	// The result outlives each run, so a copy of s would allocate.
	assertAllocs(t, 0, func() { got = bytesOf(s) })
	if string(got) != s || !capped(got) {
		t.Fatalf("bytesOf(string) = %.24q (cap %d), want a capped alias", got, cap(got))
	}
	b := make([]byte, 5, 16)
	copy(b, "bytes")
	if got := bytesOf(b); &got[0] != &b[0] || !capped(got) {
		t.Fatalf("bytesOf([]byte) = %q (cap %d), want a capped alias", got, cap(got))
	}
	raw := json.RawMessage(objectA1)
	if got := bytesOf(raw); &got[0] != &raw[0] || len(got) != len(raw) {
		t.Fatalf("bytesOf(json.RawMessage) = %q, want an alias", got)
	}
	if len(bytesOf("")) != 0 || bytesOf([]byte(nil)) != nil {
		t.Fatalf("bytesOf(\"\") and bytesOf(nil) = %q and %#v, want empty and nil", bytesOf(""), bytesOf([]byte(nil)))
	}
}

func TestTextOfAliasesItsBytes(t *testing.T) {
	b := []byte("owned bytes")
	s := textOf[string](b)
	if s != "owned bytes" || &bytesOf(s)[0] != &b[0] {
		t.Fatalf("textOf[string] = %q, want an alias of b", s)
	}
	short := textOf[[]byte](b[:len("own"):len("owned")])
	if string(short) != "own" || cap(short) != len("owned") || &short[0] != &b[0] {
		t.Fatalf("textOf[[]byte] = %q (cap %d), want the own of owned", short, cap(short))
	}
	if raw := textOf[json.RawMessage](b[:5]); string(raw) != "owned" || &raw[0] != &b[0] {
		t.Fatalf("textOf[json.RawMessage] = %q, want an alias of b", raw)
	}
}

func TestStringOfAliasesItsInput(t *testing.T) {
	b := []byte("12345")
	if s := stringOf(b); s != "12345" || &bytesOf(s)[0] != &b[0] {
		t.Fatalf("stringOf([]byte) = %q, want an alias", s)
	}
	doc := "text"
	if s := stringOf(doc); !aliases(s, doc, 0) {
		t.Fatalf("stringOf(string) = %q, want the input itself", s)
	}
}

func TestViewCapsCapacityAtItsEnd(t *testing.T) {
	data := []byte(`{"x":"y"}`)
	at := bytes.LastIndex(data, []byte(`"y"`))
	v := view[[]byte](data, at, at+len(`"y"`))
	if string(v) != `"y"` || !capped(v) || &v[0] != &data[at] {
		t.Fatalf("view = %q (cap %d), want the capped alias of the value", v, cap(v))
	}
	_ = append(v, 'X')
	if string(data) != `{"x":"y"}` {
		t.Fatalf("appending to a view rewrote the input to %s, want %s", data, `{"x":"y"}`)
	}
	doc := string(data)
	if s := view[string](bytesOf(doc), 1, 1+len(`"x"`)); s != `"x"` || !aliases(s, doc, 1) {
		t.Fatalf("view[string] = %q, want an alias of the name", s)
	}
}

func TestViewAllocs(t *testing.T) {
	data := []byte(`{"a":"b"}`)
	doc := string(data)
	assertAllocs(t, 0, func() {
		_ = view[string](data, 1, 4)
		_ = view[json.RawMessage](bytesOf(doc), 5, 8)
		_ = stringOf(data)
	})
}
