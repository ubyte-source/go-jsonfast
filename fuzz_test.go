package jsonfast

import (
	"bytes"
	"encoding/json"
	"math"
	"strconv"
	"testing"
	"time"
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
		// FindField is documented as first-match; encoding/json's map keeps the
		// last duplicate. Build a first-match oracle by streaming tokens.
		want, exists, decodeOK := firstMatchOracle(data, key)
		if !decodeOK || exists != ok {
			return
		}
		if ok && !bytes.Equal(bytes.TrimSpace(want), bytes.TrimSpace(val)) {
			t.Errorf("FindField(%q) = %q, want %q", key, val, want)
		}
	})
}

// firstMatchOracle returns the raw value of the first top-level field matching
// key in a JSON object, matching FindField's first-wins semantics. The third
// return reports whether the input was well-formed enough to decode.
func firstMatchOracle(data []byte, key string) (raw []byte, found, ok bool) {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return nil, false, false
	}
	d, isDelim := tok.(json.Delim)
	if !isDelim || d != '{' {
		return nil, false, false
	}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, false, false
		}
		k, isStr := keyTok.(string)
		if !isStr {
			return nil, false, false
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, false, false
		}
		if !found && k == key {
			raw = v
			found = true
		}
	}
	return raw, found, true
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

// FuzzFloat64Parity asserts appendFloat64 emits valid JSON that
// round-trips to the same float64, and that non-integral output is
// byte-identical to encoding/json.
func FuzzFloat64Parity(f *testing.F) {
	f.Add(0.0)
	f.Add(math.Copysign(0, -1))
	f.Add(3.14)
	f.Add(1e-7)
	f.Add(1e21)
	f.Add(math.MaxFloat64)
	f.Add(math.SmallestNonzeroFloat64)
	f.Add(float64(1 << 60))
	f.Fuzz(func(t *testing.T, v float64) {
		b := New(64)
		b.appendFloat64(v)
		assertFloat64Output(t, v, b.Bytes())
	})
}

func assertFloat64Output(t *testing.T, v float64, out []byte) {
	t.Helper()
	if math.IsNaN(v) || math.IsInf(v, 0) {
		if string(out) != "null" {
			t.Fatalf("appendFloat64(%g) = %s, want null", v, out)
		}
		return
	}
	if !json.Valid(out) {
		t.Fatalf("appendFloat64(%g) = %s is not valid JSON", v, out)
	}
	back, err := strconv.ParseFloat(string(out), 64)
	if err != nil || back != v {
		t.Fatalf("appendFloat64(%g) = %s, parse-back %g err=%v", v, out, back, err)
	}
	assertFloat64StdlibParity(t, v, out)
}

func assertFloat64StdlibParity(t *testing.T, v float64, out []byte) {
	t.Helper()
	if iv := int64(v); v > -1e18 && v < 1e18 && float64(iv) == v {
		return // exact-integer fast path may differ textually from stdlib
	}
	want, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal(%g): %v", v, err)
	}
	if !bytes.Equal(out, want) {
		t.Fatalf("appendFloat64(%g) = %s, encoding/json = %s", v, out, want)
	}
}

// FuzzTimeRFC3339Parity asserts both time emitters are byte-identical
// to time.RFC3339Nano formatting within the supported domain (epoch ≤ t,
// year ≤ 9999, whole-minute offsets). When a negative offset would need
// a pre-epoch wall date, the offset emitter falls back to the UTC form
// of the same instant.
func FuzzTimeRFC3339Parity(f *testing.F) {
	f.Add(int64(0), int64(0), 0)
	f.Add(int64(1705321845), int64(123456789), 0)
	f.Add(int64(1705321845), int64(0), 3600)
	f.Add(int64(253402300799), int64(999999999), -7200) // 9999-12-31T23:59:59
	f.Add(int64(0), int64(0), -83)                      // epoch with negative offset
	f.Fuzz(func(t *testing.T, sec, nsec int64, offset int) {
		if sec < 0 || nsec < 0 || nsec > 999999999 || offset < -24*3600 || offset > 24*3600 {
			return
		}
		offset = offset / 60 * 60 // stdlib Format cannot round-trip sub-minute offsets
		in := time.Unix(sec, nsec).In(time.FixedZone("", offset))
		if in.Year() > 9999 || in.UTC().Year() > 9999 {
			return
		}
		assertTimeParity(t, in, sec+int64(offset) < 0)
	})
}

func assertTimeParity(t *testing.T, in time.Time, preEpochWall bool) {
	t.Helper()
	b := New(64)
	b.appendTimeRFC3339(in)
	if got, want := string(b.Bytes()), `"`+in.UTC().Format(time.RFC3339Nano)+`"`; got != want {
		t.Errorf("appendTimeRFC3339(%v) = %s, stdlib = %s", in, got, want)
	}

	want := `"` + in.Format(time.RFC3339Nano) + `"`
	if preEpochWall {
		want = `"` + in.UTC().Format(time.RFC3339Nano) + `"`
	}
	b.Reset()
	b.appendTimeRFC3339Offset(in)
	if got := string(b.Bytes()); got != want {
		t.Errorf("appendTimeRFC3339Offset(%v) = %s, want %s", in, got, want)
	}
}
