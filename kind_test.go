package jsonfast

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"testing"
)

func TestKindOfClassifiesValues(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want Kind
	}{
		{litNull, KindNull}, {litTrue, KindBool}, {litFalse, KindBool},
		{"0", KindNumber}, {"-1", KindNumber}, {"9", KindNumber},
		{`"s"`, KindString}, {emptyArray, KindArray}, {emptyObject, KindObject},
		{" \t\r\n{", KindObject},
		{"", KindInvalid}, {blank, KindInvalid}, {"+1", KindInvalid},
		{".7", KindInvalid}, {"x", KindInvalid}, {"]", KindInvalid}, {"\xef", KindInvalid},
		{"\v{", KindInvalid}, {" {", KindInvalid},
	} {
		if got := KindOf(tc.in); got != tc.want {
			t.Errorf("KindOf(%q) = %d, want %d", tc.in, got, tc.want)
		}
		if got := KindOf([]byte(tc.in)); got != tc.want {
			t.Errorf("KindOf([]byte(%q)) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

// leadKinds maps each byte that starts a JSON value to the kind of that value.
func leadKinds() map[byte]Kind {
	kinds := map[byte]Kind{
		'n': KindNull, 't': KindBool, 'f': KindBool, '-': KindNumber,
		'"': KindString, '[': KindArray, '{': KindObject,
	}
	for c := byte('0'); c <= '9'; c++ {
		kinds[c] = KindNumber
	}
	return kinds
}

// stdKind returns the kind of v, a value encoding/json decodes into an any.
func stdKind(v any) Kind {
	switch v.(type) {
	case nil:
		return KindNull
	case bool:
		return KindBool
	case float64:
		return KindNumber
	case string:
		return KindString
	case []any:
		return KindArray
	case map[string]any:
		return KindObject
	}
	return KindInvalid
}

func TestKindOfClassifiesEveryLeadByte(t *testing.T) {
	kinds := leadKinds()
	for n := range math.MaxUint8 + 1 {
		c := byte(n)
		if got := KindOf([]byte{c}); got != kinds[c] {
			t.Errorf("KindOf(%q) = %d, want %d", c, got, kinds[c])
		}
	}
}

func TestKindOfAllocs(t *testing.T) {
	raw := []byte(" \t{")
	assertAllocs(t, 0, func() { _ = KindOf(raw) })
}

func FuzzKindOf(f *testing.F) {
	for _, s := range []string{litNull, " true", "-1", `"x"`, oneArray, objectA1, "x"} {
		f.Add([]byte(s))
	}
	kinds := leadKinds()
	f.Fuzz(func(t *testing.T, data []byte) {
		want := KindInvalid
		if trimmed := bytes.TrimLeft(data, " \t\r\n"); len(trimmed) > 0 {
			want = kinds[trimmed[0]]
		}
		if got := KindOf(data); got != want {
			t.Fatalf("KindOf(%q) = %d, want %d by its first byte", data, got, want)
		}
		var v any
		if json.Unmarshal(data, &v) == nil && stdKind(v) != want {
			t.Fatalf("KindOf(%q) = %d, encoding/json decodes a %d", data, want, stdKind(v))
		}
	})
}

func BenchmarkKindOf(b *testing.B) {
	raw := []byte("  \n{\"a\":1}")
	if got := KindOf(raw); got != KindObject {
		b.Fatalf("KindOf(%q) = %v, want KindObject", raw, got)
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = KindOf(raw)
	}
}

func ExampleKindOf() {
	value, _ := FindMember(`{"id":null}`, "id")
	fmt.Println(KindOf(value) == KindNull)
	// Output: true
}
