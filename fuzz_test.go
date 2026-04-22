package jsonfast

import (
	"bytes"
	"encoding/json"
	"testing"
)

func FuzzSkipStringAt(f *testing.F) {
	f.Add([]byte(`"hello"`))
	f.Add([]byte(`"he\"llo"`))
	f.Add([]byte(`"a\\b"`))
	f.Add([]byte(`""`))
	f.Add([]byte(`"control\x01"`))
	f.Add([]byte(`"unterminated`))
	f.Add([]byte(``))
	f.Fuzz(func(t *testing.T, data []byte) {
		end, ok := SkipStringAt(data, 0)
		if ok && (end < 0 || end > len(data)) {
			t.Errorf("SkipStringAt returned out-of-range end=%d (len=%d)", end, len(data))
		}
	})
}

func FuzzSkipBracedAt(f *testing.F) {
	f.Add([]byte(`{"a":"b"}`))
	f.Add([]byte(`{"nested":{"x":1}}`))
	f.Add([]byte(`[1,2,3]`))
	f.Add([]byte(`{unclosed`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) == 0 {
			return
		}
		opener := data[0]
		var closer byte
		switch opener {
		case '{':
			closer = '}'
		case '[':
			closer = ']'
		default:
			return
		}
		end, ok := SkipBracedAt(data, 0, opener, closer)
		if ok && (end < 0 || end > len(data)) {
			t.Errorf("SkipBracedAt returned out-of-range end=%d (len=%d)", end, len(data))
		}
	})
}

func FuzzIterateFields(f *testing.F) {
	f.Add([]byte(`{"a":"1","b":2,"c":true}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"a":{"b":1}}`))
	f.Add([]byte(`{"bad":}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		var count int
		IterateFields(data, func(k, _ []byte) bool {
			count++
			if len(k) < 2 || k[0] != '"' || k[len(k)-1] != '"' {
				t.Errorf("callback received malformed key %q", k)
				return false
			}
			return count < 1024
		})
	})
}

func FuzzFindField(f *testing.F) {
	f.Add([]byte(`{"a":1,"b":2}`), "a")
	f.Add([]byte(`{"hostname":"FW01"}`), "hostname")
	f.Add([]byte(`{}`), "missing")
	f.Add([]byte(`not json`), "x")
	f.Fuzz(func(t *testing.T, data []byte, key string) {
		val, ok := FindField(data, key)
		if ok && len(val) == 0 {
			t.Errorf("FindField returned ok with empty value")
		}
		var m map[string]json.RawMessage
		if json.Unmarshal(data, &m) != nil {
			return
		}
		want, exists := m[key]
		if exists != ok {
			return
		}
		if ok && !bytes.Equal(bytes.TrimSpace(want), bytes.TrimSpace(val)) {
			t.Errorf("FindField(%q) = %q, want %q", key, val, want)
		}
	})
}

func FuzzIsStructuralJSON(f *testing.F) {
	f.Add(`{"a":"b"}`)
	f.Add(`[1,2,3]`)
	f.Add(`{`)
	f.Add(`{"k":}`)
	f.Add(`"hello"`)
	f.Fuzz(func(t *testing.T, s string) {
		ok := IsStructuralJSON(s)
		if ok && len(s) >= 2 && s[0] != '{' && s[0] != '[' {
			t.Errorf("IsStructuralJSON accepted a payload not starting with { or [: %q", s)
		}
	})
}

func FuzzIterateArray(f *testing.F) {
	f.Add([]byte(`[1,2,3]`))
	f.Add([]byte(`["a","b","c"]`))
	f.Add([]byte(`[]`))
	f.Add([]byte(`[1,2,`))
	f.Fuzz(func(t *testing.T, data []byte) {
		var count int
		IterateArray(data, func(elem []byte) bool {
			count++
			if len(elem) == 0 {
				t.Error("callback received empty element slice")
				return false
			}
			return count < 1024
		})
	})
}

func FuzzFlattenObject(f *testing.F) {
	f.Add([]byte(`{"KV@123":{"action":"pass","srcip":"1.2.3.4"}}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"a":{"b":{"c":"deep"}}}`))
	f.Add([]byte(`{"x":123}`))
	f.Fuzz(func(_ *testing.T, data []byte) {
		b := New(256)
		b.BeginObject()
		FlattenObject(b, data)
		b.EndObject()
	})
}

// FuzzRoundTripStructuralJSON asserts acceptance is a subset of encoding/json.Valid.
func FuzzRoundTripStructuralJSON(f *testing.F) {
	f.Add(`{"a":1,"b":[true,null,"x"]}`)
	f.Add(`[1,2,3]`)
	f.Add(`{}`)
	f.Add(`[]`)
	f.Add(`[00]`)
	f.Add(`{"k":01}`)
	f.Add(`{"k":1.}`)
	f.Add(`{"k":1e}`)
	f.Fuzz(func(t *testing.T, s string) {
		if IsStructuralJSON(s) && !json.Valid([]byte(s)) {
			t.Errorf("IsStructuralJSON accepted %q but encoding/json rejected it", s)
		}
	})
}

// FuzzRoundTripFindField cross-checks FindField against encoding/json,
// skipping cases where the two libraries diverge (duplicate keys,
// encoding/json normalizing invalid UTF-8 to U+FFFD).
func FuzzRoundTripFindField(f *testing.F) {
	f.Add([]byte(`{"a":1,"b":2}`))
	f.Add([]byte(`{"quoted\"name":1,"plain":2}`))
	f.Add([]byte(`{"caf\u00e9":"value"}`))
	f.Add([]byte(`{"rocket \ud83d\ude80":"launch"}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(data, &m); err != nil {
			return
		}
		topLevelKeys := 0
		IterateFields(data, func(_, _ []byte) bool {
			topLevelKeys++
			return true
		})
		if topLevelKeys != len(m) {
			return
		}
		for key, want := range m {
			encoded, err := json.Marshal(key)
			if err != nil || !bytes.Contains(data, encoded) {
				continue
			}
			val, ok := FindField(data, key)
			if !ok {
				t.Errorf("FindField(%q) = not found; encoding/json decoded %q", key, want)
				continue
			}
			if !bytes.Equal(bytes.TrimSpace(want), bytes.TrimSpace(val)) {
				t.Errorf("FindField(%q) = %q, want %q", key, val, want)
			}
		}
	})
}
