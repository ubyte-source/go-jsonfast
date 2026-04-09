package jsonfast

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// ---------------------------------------------------------------------------
// Builder lifecycle
// ---------------------------------------------------------------------------

func TestBuilder_New(t *testing.T) {
	t.Run("with positive capacity", func(t *testing.T) {
		b := New(512)
		if b == nil {
			t.Fatal("New() returned nil")
		}
		if cap(b.buf) < 512 {
			t.Errorf("Expected capacity >= 512, got %d", cap(b.buf))
		}
	})

	t.Run("with zero capacity", func(t *testing.T) {
		b := New(0)
		if b == nil {
			t.Fatal("New() returned nil")
		}
		if cap(b.buf) < 256 {
			t.Errorf("Expected default capacity >= 256, got %d", cap(b.buf))
		}
	})

	t.Run("with negative capacity", func(t *testing.T) {
		b := New(-10)
		if b == nil {
			t.Fatal("New() returned nil")
		}
		if cap(b.buf) < 256 {
			t.Errorf("Expected default capacity >= 256, got %d", cap(b.buf))
		}
	})
}

func TestBuilder_Reset(t *testing.T) {
	b := New(256)
	b.BeginObject()
	b.AddStringField("test", "value")
	b.EndObject()

	if len(b.Bytes()) == 0 {
		t.Error("Expected non-empty buffer before reset")
	}

	b.Reset()

	if len(b.Bytes()) != 0 {
		t.Errorf("Expected empty buffer after reset, got length %d", len(b.Bytes()))
	}
	if !b.first {
		t.Error("Expected first=true after reset")
	}
}

func TestBuilder_Len(t *testing.T) {
	b := New(256)
	if b.Len() != 0 {
		t.Errorf("Expected Len()=0, got %d", b.Len())
	}
	b.BeginObject()
	b.EndObject()
	if b.Len() != 2 {
		t.Errorf("Expected Len()=2 for '{}', got %d", b.Len())
	}
}

func TestBuilder_Grow(t *testing.T) {
	b := New(16)
	b.Grow(1024)
	if cap(b.buf) < 1024 {
		t.Errorf("Expected cap >= 1024 after Grow, got %d", cap(b.buf))
	}
	// Grow with sufficient capacity should be a no-op.
	oldCap := cap(b.buf)
	b.Grow(10)
	if cap(b.buf) != oldCap {
		t.Error("Grow should be no-op when capacity is sufficient")
	}
}

func TestBuilder_BeginEndObject(t *testing.T) {
	b := New(256)
	b.BeginObject()
	b.EndObject()
	if string(b.Bytes()) != "{}" {
		t.Errorf("Expected '{}', got %q", b.Bytes())
	}
}

// ---------------------------------------------------------------------------
// AddStringField
// ---------------------------------------------------------------------------

func TestBuilder_AddStringField(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		value    string
		expected string
	}{
		{name: "simple string", key: "message", value: "hello world", expected: `{"message":"hello world"}`},
		{name: "empty string", key: "empty", value: "", expected: `{"empty":""}`},
		{name: "string with quotes", key: "q", value: `she said "hello"`, expected: `{"q":"she said \"hello\""}`},
		{name: "string with backslash", key: "p", value: `C:\Users\Test`, expected: `{"p":"C:\\Users\\Test"}`},
		{name: "string with newline", key: "m", value: "line1\nline2", expected: `{"m":"line1\nline2"}`},
		{name: "string with tab", key: "t", value: "col1\tcol2", expected: `{"t":"col1\tcol2"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := New(256)
			b.BeginObject()
			b.AddStringField(tt.key, tt.value)
			b.EndObject()

			result := string(b.Bytes())
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}

			var parsed map[string]any
			if err := json.Unmarshal(b.Bytes(), &parsed); err != nil {
				t.Errorf("Generated invalid JSON: %v", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// AddRawJSONField
// ---------------------------------------------------------------------------

func TestBuilder_AddRawJSONField(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		expected string
		rawJSON  []byte
	}{
		{"simple object", "data", `{"data":{"nested":"value"}}`, []byte(`{"nested":"value"}`)},
		{"array", "items", `{"items":[1,2,3]}`, []byte(`[1,2,3]`)},
		{"number", "count", `{"count":42}`, []byte(`42`)},
		{"boolean", "flag", `{"flag":true}`, []byte(`true`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := New(256)
			b.BeginObject()
			b.AddRawJSONField(tt.key, tt.rawJSON)
			b.EndObject()

			result := string(b.Bytes())
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}

			var parsed map[string]any
			if err := json.Unmarshal(b.Bytes(), &parsed); err != nil {
				t.Errorf("Generated invalid JSON: %v", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// AddBoolField
// ---------------------------------------------------------------------------

func TestBuilder_AddBoolField_True(t *testing.T) {
	b := New(256)
	b.BeginObject()
	b.AddBoolField("enabled", true)
	b.EndObject()
	expect(t, `{"enabled":true}`, string(b.Bytes()))
}

func TestBuilder_AddBoolField_False(t *testing.T) {
	b := New(256)
	b.BeginObject()
	b.AddBoolField("enabled", false)
	b.EndObject()
	expect(t, `{"enabled":false}`, string(b.Bytes()))
}

// ---------------------------------------------------------------------------
// AddIntField
// ---------------------------------------------------------------------------

func TestBuilder_AddIntField(t *testing.T) {
	b := New(256)
	b.BeginObject()
	b.AddIntField("count", 42)
	b.EndObject()
	assertContains(t, string(b.Bytes()), `"count":42`)
}

func TestBuilder_AddIntField_Negative(t *testing.T) {
	b := New(256)
	b.BeginObject()
	b.AddIntField("neg", -42)
	b.EndObject()
	assertContains(t, string(b.Bytes()), `"neg":-42`)
}

func TestBuilder_AddIntField_Zero(t *testing.T) {
	b := New(256)
	b.BeginObject()
	b.AddIntField("z", 0)
	b.EndObject()
	assertContains(t, string(b.Bytes()), `"z":0`)
}

func TestBuilder_AddIntField_MinInt(t *testing.T) {
	b := New(256)
	b.BeginObject()
	b.AddIntField("min", math.MinInt)
	b.EndObject()
	result := string(b.Bytes())
	assertContains(t, result, `"min":-`)
	if !strings.Contains(result, "-9223372036854775808") && !strings.Contains(result, "-2147483648") {
		t.Errorf("MinInt not correctly formatted: %s", result)
	}
}

func TestBuilder_AddIntField_SmallFastPath(t *testing.T) {
	tests := []struct {
		want string
		val  int
	}{
		{want: `"v":0`, val: 0},
		{want: `"v":1`, val: 1},
		{want: `"v":9`, val: 9},
		{want: `"v":10`, val: 10},
		{want: `"v":42`, val: 42},
		{want: `"v":99`, val: 99},
		{want: `"v":100`, val: 100},
		{want: `"v":999`, val: 999},
	}
	for _, tt := range tests {
		b := New(64)
		b.BeginObject()
		b.AddIntField("v", tt.val)
		b.EndObject()
		assertContains(t, string(b.Bytes()), tt.want)
	}
}

// ---------------------------------------------------------------------------
// AddInt64Field
// ---------------------------------------------------------------------------

func TestBuilder_AddInt64Field(t *testing.T) {
	b := New(256)
	b.BeginObject()
	b.AddInt64Field("big", int64(math.MaxInt64))
	b.EndObject()
	assertContains(t, string(b.Bytes()), `"big":9223372036854775807`)
}

func TestBuilder_AddInt64Field_Negative(t *testing.T) {
	b := New(256)
	b.BeginObject()
	b.AddInt64Field("neg", -123456789)
	b.EndObject()
	assertContains(t, string(b.Bytes()), `"neg":-123456789`)
}

// ---------------------------------------------------------------------------
// AddUint8Field / AddUint16Field / AddUint32Field / AddUint64Field
// ---------------------------------------------------------------------------

func TestBuilder_AddUint8Field(t *testing.T) {
	b := New(256)
	b.BeginObject()
	b.AddUint8Field("severity", 5)
	b.EndObject()
	expect(t, `{"severity":5}`, string(b.Bytes()))
}

func TestBuilder_AddUint8Field_Zero(t *testing.T) {
	b := New(256)
	b.BeginObject()
	b.AddUint8Field("sev", 0)
	b.EndObject()
	assertContains(t, string(b.Bytes()), `"sev":0`)
}

func TestBuilder_AddUint8Field_Max(t *testing.T) {
	b := New(256)
	b.BeginObject()
	b.AddUint8Field("sev", 255)
	b.EndObject()
	assertContains(t, string(b.Bytes()), `"sev":255`)
}

func TestBuilder_AddUint16Field(t *testing.T) {
	b := New(256)
	b.BeginObject()
	b.AddUint16Field("version", 1)
	b.EndObject()
	expect(t, `{"version":1}`, string(b.Bytes()))
}

func TestBuilder_AddUint16Field_Max(t *testing.T) {
	b := New(256)
	b.BeginObject()
	b.AddUint16Field("ver", 65535)
	b.EndObject()
	assertContains(t, string(b.Bytes()), `"ver":65535`)
}

func TestBuilder_AddUint32Field(t *testing.T) {
	b := New(256)
	b.BeginObject()
	b.AddUint32Field("pid", 4294967295)
	b.EndObject()
	assertContains(t, string(b.Bytes()), `"pid":4294967295`)
}

func TestBuilder_AddUint64Field(t *testing.T) {
	b := New(256)
	b.BeginObject()
	b.AddUint64Field("id", uint64(math.MaxUint64))
	b.EndObject()
	assertContains(t, string(b.Bytes()), `"id":18446744073709551615`)
}

// ---------------------------------------------------------------------------
// AddFloat64Field
// ---------------------------------------------------------------------------

func TestBuilder_AddFloat64Field(t *testing.T) {
	tests := []struct {
		name string
		want string
		val  float64
	}{
		{"integer", `"v":42`, 42.0},
		{"negative integer", `"v":-10`, -10.0},
		{"zero", `"v":0`, 0.0},
		{"fraction", `"v":3.14`, 3.14},
		{"negative fraction", `"v":-2.5`, -2.5},
		{"small fraction", `"v":0.001`, 0.001},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := New(64)
			b.BeginObject()
			b.AddFloat64Field("v", tt.val)
			b.EndObject()
			assertContains(t, string(b.Bytes()), tt.want)
		})
	}
}

// ---------------------------------------------------------------------------
// AddTimeRFC3339Field
// ---------------------------------------------------------------------------

func TestBuilder_AddTimeRFC3339Field_WithNanoseconds(t *testing.T) {
	ts := time.Date(2024, 3, 15, 14, 30, 45, 123456789, time.UTC)
	b := New(256)
	b.BeginObject()
	b.AddTimeRFC3339Field("ts", ts)
	b.EndObject()
	assertContains(t, string(b.Bytes()), "2024-03-15T14:30:45.123456789Z")
}

func TestBuilder_AddTimeRFC3339Field_ZeroNanoseconds(t *testing.T) {
	ts := time.Date(2024, 6, 15, 12, 30, 45, 0, time.UTC)
	b := New(256)
	b.BeginObject()
	b.AddTimeRFC3339Field("ts", ts)
	b.EndObject()

	result := string(b.Bytes())
	assertContains(t, result, `"ts":"2024-06-15T12:30:45Z"`)
	if strings.Contains(result, ".") {
		t.Error("Should not contain dot when ns=0")
	}
}

func TestBuilder_AddTimeRFC3339Field_ZeroTime(t *testing.T) {
	b := New(128)
	b.BeginObject()
	b.AddTimeRFC3339Field("ts", time.Time{})
	b.EndObject()
	// time.Time{} is year 1, but our Unix-based approach clamps negative timestamps to epoch.
	// The output must still be valid JSON with a valid RFC3339 timestamp.
	result := string(b.Bytes())
	assertContains(t, result, `"ts":"`)
	var parsed map[string]string
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
}

func TestBuilder_AddTimeRFC3339Field_NegativeYear(t *testing.T) {
	b := New(128)
	b.BeginObject()
	b.AddTimeRFC3339Field("ts", time.Date(-5, 1, 1, 0, 0, 0, 0, time.UTC))
	b.EndObject()
	// Negative dates are clamped to epoch (1970-01-01).
	result := string(b.Bytes())
	assertContains(t, result, `"ts":"`)
	var parsed map[string]string
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
}

func TestBuilder_AddTimeRFC3339Field_Year10000(t *testing.T) {
	b := New(128)
	b.BeginObject()
	b.AddTimeRFC3339Field("ts", time.Date(12000, 1, 1, 0, 0, 0, 0, time.UTC))
	b.EndObject()
	assertContains(t, string(b.Bytes()), "9999-")
}

func TestBuilder_AddTimeRFC3339Field_Year9999(t *testing.T) {
	ts := time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC)
	b := New(256)
	b.BeginObject()
	b.AddTimeRFC3339Field("ts", ts)
	b.EndObject()
	assertContains(t, string(b.Bytes()), "9999-12-31T23:59:59Z")
}

func TestBuilder_AddTimeRFC3339Field_Epoch(t *testing.T) {
	ts := time.Unix(0, 0).UTC()
	b := New(128)
	b.BeginObject()
	b.AddTimeRFC3339Field("ts", ts)
	b.EndObject()
	assertContains(t, string(b.Bytes()), `"ts":"1970-01-01T00:00:00Z"`)
}

func TestBuilder_AddTimeRFC3339Field_Modern(t *testing.T) {
	ts := time.Date(2025, 7, 4, 18, 45, 30, 500000000, time.UTC)
	b := New(128)
	b.BeginObject()
	b.AddTimeRFC3339Field("ts", ts)
	b.EndObject()
	assertContains(t, string(b.Bytes()), `"ts":"2025-07-04T18:45:30.5Z"`)
}

// ---------------------------------------------------------------------------
// AddNestedStringMapField
// ---------------------------------------------------------------------------

func TestBuilder_AddNestedStringMapField(t *testing.T) {
	b := New(256)
	b.BeginObject()
	nestedMap := map[string]map[string]string{
		"exampleSDID@32473": {
			"iut":         "3",
			"eventSource": "Application",
			"eventID":     "1011",
		},
	}
	b.AddNestedStringMapField("structured_data", nestedMap)
	b.EndObject()

	result := string(b.Bytes())
	assertContains(t, result, `"structured_data"`)
	assertContains(t, result, `"exampleSDID@32473"`)
	assertContains(t, result, `"iut"`)
}

func TestBuilder_AddNestedStringMapField_Empty(t *testing.T) {
	b := New(64)
	b.BeginObject()
	b.AddNestedStringMapField("sd", map[string]map[string]string{})
	b.EndObject()
	expect(t, `{}`, string(b.Bytes()))
}

func TestBuilder_AddNestedStringMapField_EmptyInnerMap(t *testing.T) {
	b := New(64)
	b.BeginObject()
	b.AddNestedStringMapField("sd", map[string]map[string]string{
		"outer": {},
	})
	b.EndObject()
	assertContains(t, string(b.Bytes()), `"sd":{"outer":{}}`)
}

func TestBuilder_AddNestedStringMapField_SpecialCharsInKeys(t *testing.T) {
	b := New(128)
	b.BeginObject()
	b.AddNestedStringMapField("sd", map[string]map[string]string{
		"a\"b": {"c\\d": "e\nf"},
	})
	b.EndObject()
	s := string(b.Bytes())
	assertContains(t, s, `a\"b`)
	assertContains(t, s, `c\\d`)
	assertContains(t, s, `e\nf`)
}

func TestBuilder_NestedMapSmallStackSort(t *testing.T) {
	b := New(512)
	b.BeginObject()
	m := map[string]map[string]string{
		"zulu":  {"k": "v1"},
		"alpha": {"k": "v2"},
		"mike":  {"k": "v3"},
	}
	b.AddNestedStringMapField("sd", m)
	b.EndObject()

	result := string(b.Bytes())
	alphaIdx := strings.Index(result, "alpha")
	mikeIdx := strings.Index(result, "mike")
	zuluIdx := strings.Index(result, "zulu")
	if mikeIdx <= alphaIdx {
		t.Error("mike should come after alpha")
	}
	if zuluIdx <= mikeIdx {
		t.Error("zulu should come after mike")
	}
}

func TestBuilder_NestedMapLargeHeapSort(t *testing.T) {
	b := New(2048)
	b.BeginObject()
	m := make(map[string]map[string]string)
	keys := []string{"k09", "k08", "k07", "k06", "k05", "k04", "k03", "k02", "k01", "k00"}
	for _, k := range keys {
		m[k] = map[string]string{"val": k}
	}
	b.AddNestedStringMapField("sd", m)
	b.EndObject()

	result := string(b.Bytes())
	lastIdx := -1
	for i := range 10 {
		key := "k0" + string(rune('0'+i))
		idx := strings.Index(result, key)
		if idx <= lastIdx {
			t.Errorf("key %s should come after previous key", key)
		}
		lastIdx = idx
	}
}

// ---------------------------------------------------------------------------
// Multiple fields
// ---------------------------------------------------------------------------

func TestBuilder_MultipleFields(t *testing.T) {
	b := New(256)
	b.BeginObject()
	b.AddStringField("name", "John")
	b.AddIntField("age", 30)
	b.AddBoolField("active", true)
	b.AddRawJSONField("tags", []byte(`["dev","go"]`))
	b.EndObject()

	var parsed map[string]any
	if err := json.Unmarshal(b.Bytes(), &parsed); err != nil {
		t.Fatalf("Generated invalid JSON: %v", err)
	}
	if parsed["name"] != "John" {
		t.Errorf("Expected name=John, got %v", parsed["name"])
	}
}

// ---------------------------------------------------------------------------
// sep() — comma separator
// ---------------------------------------------------------------------------

func TestBuilder_Sep(t *testing.T) {
	b := New(64)
	b.BeginObject()
	b.AddStringField("k", "v")
	b.EndObject()
	expect(t, `{"k":"v"}`, string(b.Bytes()))
}

func TestBuilder_Sep_RawJSON(t *testing.T) {
	b := New(64)
	b.BeginObject()
	b.AddRawJSONField("data", []byte(`[1,2]`))
	b.EndObject()
	expect(t, `{"data":[1,2]}`, string(b.Bytes()))
}

// ---------------------------------------------------------------------------
// escapeString — UTF-8 and control characters
// ---------------------------------------------------------------------------

func TestBuilder_EscapeString_BulkASCII(t *testing.T) {
	b := New(512)
	b.BeginObject()
	longStr := "This is a perfectly normal syslog message with " +
		"no special characters at all just plain ASCII text 1234567890"
	b.AddStringField("msg", longStr)
	b.EndObject()
	assertContains(t, string(b.Bytes()), longStr)
}

func TestBuilder_EscapeString_MixedEscapes(t *testing.T) {
	b := New(256)
	b.BeginObject()
	b.AddStringField("msg", "line1\nline2\ttab\\slash\"quote")
	b.EndObject()
	assertContains(t, string(b.Bytes()), `line1\nline2\ttab\\slash\"quote`)
}

func TestBuilder_EscapeControlChars(t *testing.T) {
	b := New(256)
	b.BeginObject()
	b.AddStringField("msg", "hello\x00\x01\x1fworld")
	b.EndObject()
	result := string(b.Bytes())
	assertContains(t, result, `\u0000`)
	assertContains(t, result, `\u0001`)
	assertContains(t, result, `\u001f`)
}

func TestBuilder_EscapeAllControlChars(t *testing.T) {
	b := New(256)
	b.BeginObject()
	s := ""
	for i := range 0x20 {
		s += string(rune(i))
	}
	b.AddStringField("ctrl", s)
	b.EndObject()
	result := string(b.Bytes())
	assertContains(t, result, `\n`)
	assertContains(t, result, `\r`)
	assertContains(t, result, `\t`)
	assertContains(t, result, `\u0000`)
}

func TestBuilder_EscapeInvalidUTF8(t *testing.T) {
	b := New(256)
	b.BeginObject()
	b.AddStringField("msg", "hello\xff\xfeworld")
	b.EndObject()
	assertContains(t, string(b.Bytes()), "\ufffd")
}

func TestEscapeMultiByte_TruncatedUTF8_2byte(t *testing.T) {
	b := New(64)
	b.BeginObject()
	b.AddStringField("k", string([]byte{0xC2}))
	b.EndObject()
	assertContains(t, string(b.Bytes()), "\xef\xbf\xbd")
}

func TestEscapeMultiByte_TruncatedUTF8_3byte(t *testing.T) {
	b := New(64)
	b.BeginObject()
	b.AddStringField("k", string([]byte{0xE0, 0x80}))
	b.EndObject()
	assertContains(t, string(b.Bytes()), "\xef\xbf\xbd")
}

func TestEscapeMultiByte_TruncatedUTF8_4byte(t *testing.T) {
	b := New(64)
	b.BeginObject()
	b.AddStringField("k", string([]byte{0xF0, 0x90, 0x80}))
	b.EndObject()
	assertContains(t, string(b.Bytes()), "\xef\xbf\xbd")
}

func TestEscapeMultiByte_InvalidContinuation(t *testing.T) {
	b := New(64)
	b.BeginObject()
	b.AddStringField("k", string([]byte{0xC2, 0x00}))
	b.EndObject()
	assertContains(t, string(b.Bytes()), "\xef\xbf\xbd")
}

func TestEscapeMultiByte_ValidUTF8(t *testing.T) {
	b := New(64)
	b.BeginObject()
	b.AddStringField("k", "héllo wörld 日本語 🚀")
	b.EndObject()
	s := string(b.Bytes())
	assertContains(t, s, "héllo")
	assertContains(t, s, "wörld")
	assertContains(t, s, "日本語")
	assertContains(t, s, "🚀")
}

func TestEscapeMultiByte_BareContinuationByte(t *testing.T) {
	b := New(64)
	b.BeginObject()
	b.AddStringField("k", string([]byte{0x80}))
	b.EndObject()
	assertContains(t, string(b.Bytes()), "\xef\xbf\xbd")
}

func TestEscapeMultiByte_InvalidLeadByte0xF8(t *testing.T) {
	b := New(64)
	b.BeginObject()
	b.AddStringField("k", string([]byte{0xF8}))
	b.EndObject()
	assertContains(t, string(b.Bytes()), "\xef\xbf\xbd")
}

func TestUtf8SeqLen(t *testing.T) {
	cases := []struct {
		in   byte
		want int
	}{
		{0xC2, 2}, {0xE0, 3}, {0xF0, 4}, {0xF4, 4},
		{0x80, 0}, {0xF8, 0}, {0xFF, 0},
		{0xC0, 0}, {0xC1, 0}, // overlong 2-byte leaders
		{0xF5, 0}, {0xF6, 0}, {0xF7, 0}, // above U+10FFFF
	}
	for _, c := range cases {
		if got := utf8SeqLen(c.in); got != c.want {
			t.Errorf("utf8SeqLen(0x%02X) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestValidContinuation(t *testing.T) {
	if !validContinuation("é", 0, 2) { // C3 A9
		t.Error("expected valid for 'é'")
	}
	if validContinuation(string([]byte{0xC2, 0x00}), 0, 2) {
		t.Error("expected invalid for 0xC2 0x00")
	}
}

func TestValidContinuation_OverlongRejection(t *testing.T) {
	// validContinuation only checks continuation byte form (10xxxxxx).
	// Overlong/range validation is done at the codepoint level in escapeMultiByte.
	// Here we verify continuation byte checks still work correctly.

	// Valid continuation bytes should pass.
	if !validContinuation(string([]byte{0xE0, 0x80, 0x80}), 0, 3) {
		t.Error("expected valid continuation bytes for E0 80 80")
	}
	// Invalid continuation byte should fail.
	if validContinuation(string([]byte{0xE0, 0x00, 0x80}), 0, 3) {
		t.Error("expected invalid for E0 00 80 (non-continuation)")
	}
}

func TestEscapeString_OverlongUTF8(t *testing.T) {
	// All overlong sequences should produce U+FFFD (0xEF 0xBF 0xBD)
	tests := []struct {
		name  string
		input []byte
	}{
		{"2-byte overlong C0 80", []byte{0xC0, 0x80}},
		{"2-byte overlong C1 BF", []byte{0xC1, 0xBF}},
		{"3-byte overlong E0 80 80", []byte{0xE0, 0x80, 0x80}},
		{"3-byte overlong E0 9F BF", []byte{0xE0, 0x9F, 0xBF}},
		{"4-byte overlong F0 80 80 80", []byte{0xF0, 0x80, 0x80, 0x80}},
		{"4-byte overlong F0 8F BF BF", []byte{0xF0, 0x8F, 0xBF, 0xBF}},
		{"above U+10FFFF F4 90 80 80", []byte{0xF4, 0x90, 0x80, 0x80}},
		{"invalid leader F5", []byte{0xF5, 0x80, 0x80, 0x80}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := New(64)
			b.BeginObject()
			b.AddStringField("k", string(tt.input))
			b.EndObject()
			assertContains(t, string(b.Bytes()), "\xef\xbf\xbd")
		})
	}
}

// ---------------------------------------------------------------------------
// Exported EscapeString
// ---------------------------------------------------------------------------

func TestExportedEscapeString_NoEscape(t *testing.T) {
	in := "hello-world-123"
	out := EscapeString(in)
	if out != in {
		t.Errorf("EscapeString(%q) = %q; want same string", in, out)
	}
}

func TestExportedEscapeString_Empty(t *testing.T) {
	out := EscapeString("")
	if out != "" {
		t.Errorf("EscapeString(\"\") = %q; want empty", out)
	}
}

func TestExportedEscapeString_WithQuotes(t *testing.T) {
	expect(t, `say \"hello\"`, EscapeString(`say "hello"`))
}

func TestExportedEscapeString_WithBackslash(t *testing.T) {
	expect(t, `path\\to\\file`, EscapeString(`path\to\file`))
}

func TestExportedEscapeString_WithNewline(t *testing.T) {
	expect(t, `line1\nline2`, EscapeString("line1\nline2"))
}

func TestExportedEscapeString_WithControlChar(t *testing.T) {
	expect(t, `x\u0001y`, EscapeString("x\x01y"))
}

func TestExportedEscapeString_WithTab(t *testing.T) {
	expect(t, `a\tb`, EscapeString("a\tb"))
}

func TestExportedEscapeString_Mixed(t *testing.T) {
	expect(t, `a\"b\\c\nd\u0000e`, EscapeString("a\"b\\c\nd\x00e"))
}

func TestExportedEscapeString_InvalidUTF8(t *testing.T) {
	// EscapeString must handle invalid UTF-8, replacing with U+FFFD.
	result := EscapeString("\xff\xfe")
	assertContains(t, result, "\ufffd")
	// Result wrapped in quotes must be valid JSON.
	raw := `{"v":"` + result + `"}`
	var parsed map[string]string
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		t.Fatalf("EscapeString produced invalid JSON content: %v\nraw: %s", err, raw)
	}
}

func TestExportedEscapeString_InvalidLeadingByte(t *testing.T) {
	// Byte 0x80 is an invalid UTF-8 leading byte and must be replaced.
	result := EscapeString(string([]byte{0x80}))
	assertContains(t, result, "\ufffd")
}

// ---------------------------------------------------------------------------
// Complex end-to-end
// ---------------------------------------------------------------------------

func TestComplexJSON(t *testing.T) {
	b := New(512)
	b.BeginObject()
	b.AddStringField("source", "10.0.0.1")
	b.AddStringField("timestamp", "1234567890")
	b.AddRawJSONField("object", []byte(`{"message":"test","severity":5}`))
	b.AddStringField("raw", "<189>1 test syslog message")
	b.EndObject()

	var parsed map[string]any
	if err := json.Unmarshal(b.Bytes(), &parsed); err != nil {
		t.Fatalf("Generated invalid JSON: %v", err)
	}
	if parsed["source"] != "10.0.0.1" {
		t.Errorf("Expected source=10.0.0.1, got %v", parsed["source"])
	}
	obj, ok := parsed["object"].(map[string]any)
	if !ok {
		t.Fatalf("Expected object to be a map, got %T", parsed["object"])
	}
	if obj["message"] != "test" {
		t.Errorf("Expected object.message=test, got %v", obj["message"])
	}
}

// ---------------------------------------------------------------------------
// absInt64AsUint64
// ---------------------------------------------------------------------------

func TestAbsInt64AsUint64(t *testing.T) {
	if absInt64AsUint64(0) != 0 {
		t.Error("absInt64AsUint64(0) != 0")
	}
	if absInt64AsUint64(42) != 42 {
		t.Error("absInt64AsUint64(42) != 42")
	}
	if absInt64AsUint64(-42) != 42 {
		t.Error("absInt64AsUint64(-42) != 42")
	}
	if absInt64AsUint64(math.MinInt64) != 1<<63 {
		t.Error("absInt64AsUint64(MinInt64) != 2^63")
	}
}

// ---------------------------------------------------------------------------
// Benchmarks
// ---------------------------------------------------------------------------

func BenchmarkBuilder_AddStringField(b *testing.B) {
	builder := New(256)
	b.ResetTimer()
	for b.Loop() {
		builder.Reset()
		builder.BeginObject()
		builder.AddStringField("key1", "value1")
		builder.AddStringField("key2", "value2")
		builder.AddStringField("key3", "value3")
		builder.EndObject()
		_ = builder.Bytes()
	}
}

func BenchmarkBuilder_FullSyslogObject(b *testing.B) {
	builder := New(512)
	ts := time.Date(2024, 1, 15, 12, 30, 45, 123456789, time.UTC)

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		builder.Reset()
		builder.BeginObject()
		builder.AddStringField("message", "User authentication failed for admin from 192.168.1.100 port 22 ssh2")
		builder.AddTimeRFC3339Field("timestamp", ts)
		builder.AddStringField("hostname", "webserver-prod-01")
		builder.AddIntField("severity", 4)
		builder.AddIntField("facility", 10)
		builder.AddStringField("app_name", "sshd")
		builder.AddStringField("proc_id", "28374")
		builder.AddStringField("msg_id", "AUTH_FAIL")
		builder.AddIntField("version", 1)
		builder.AddStringField("source", "192.168.1.100")
		builder.EndObject()
		_ = builder.Bytes()
	}
}

func BenchmarkBuilder_FullSyslogObject_FieldKey(b *testing.B) {
	builder := New(512)
	ts := time.Date(2024, 1, 15, 12, 30, 45, 123456789, time.UTC)

	keyMessage := NewFieldKey("message")
	keyTimestamp := NewFieldKey("timestamp")
	keyHostname := NewFieldKey("hostname")
	keySeverity := NewFieldKey("severity")
	keyFacility := NewFieldKey("facility")
	keyAppName := NewFieldKey("app_name")
	keyProcID := NewFieldKey("proc_id")
	keyMsgID := NewFieldKey("msg_id")
	keyVersion := NewFieldKey("version")
	keySource := NewFieldKey("source")

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		builder.Reset()
		builder.BeginObject()
		builder.AddStringFieldKey(keyMessage, "User authentication failed for admin from 192.168.1.100 port 22 ssh2")
		builder.AddTimeRFC3339FieldKey(keyTimestamp, ts)
		builder.AddStringFieldKey(keyHostname, "webserver-prod-01")
		builder.AddIntFieldKey(keySeverity, 4)
		builder.AddIntFieldKey(keyFacility, 10)
		builder.AddStringFieldKey(keyAppName, "sshd")
		builder.AddStringFieldKey(keyProcID, "28374")
		builder.AddStringFieldKey(keyMsgID, "AUTH_FAIL")
		builder.AddIntFieldKey(keyVersion, 1)
		builder.AddStringFieldKey(keySource, "192.168.1.100")
		builder.EndObject()
		_ = builder.Bytes()
	}
}

func BenchmarkBuilder_EscapeString_PureASCII(b *testing.B) {
	builder := New(512)
	s := "This is a typical syslog message with hostname=myhost " +
		"severity=info facility=local0 and no special characters at all"
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		builder.Reset()
		builder.buf = append(builder.buf, '"')
		builder.escapeString(s)
		builder.buf = append(builder.buf, '"')
	}
}

func BenchmarkBuilder_EscapeString_WithEscapes(b *testing.B) {
	builder := New(512)
	s := "Message with \"quotes\" and \\backslash and\nnewline and\ttab characters"
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		builder.Reset()
		builder.buf = append(builder.buf, '"')
		builder.escapeString(s)
		builder.buf = append(builder.buf, '"')
	}
}

func BenchmarkBuilder_EscapeString_LongASCII(b *testing.B) {
	builder := New(4096)
	s := strings.Repeat("abcdefghijklmnopqrstuvwxyz0123456789 ", 100)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		builder.Reset()
		builder.buf = append(builder.buf, '"')
		builder.escapeString(s)
		builder.buf = append(builder.buf, '"')
	}
}

func BenchmarkBuilder_EscapeString_Unicode(b *testing.B) {
	builder := New(512)
	s := "Message with unicode: 日本語テスト and emojis 🎉🔥 mixed with ASCII text"
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		builder.Reset()
		builder.buf = append(builder.buf, '"')
		builder.escapeString(s)
		builder.buf = append(builder.buf, '"')
	}
}

func BenchmarkBuilder_AppendInt(b *testing.B) {
	builder := New(64)
	b.ResetTimer()
	b.ReportAllocs()
	var i int
	for b.Loop() {
		builder.Reset()
		builder.appendInt(i)
		i++
	}
}

func BenchmarkBuilder_NestedStringMapField(b *testing.B) {
	builder := New(1024)
	sd := map[string]map[string]string{
		"exampleSDID@32473": {
			"iut":         "3",
			"eventSource": "Application",
			"eventID":     "1011",
		},
		"examplePriority@32473": {
			"class": "high",
		},
	}

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		builder.Reset()
		builder.BeginObject()
		builder.AddNestedStringMapField("structured_data", sd)
		builder.EndObject()
	}
}

func BenchmarkBuilder_AcquireRelease(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		builder := Acquire()
		builder.BeginObject()
		builder.AddStringField("msg", "test")
		builder.EndObject()
		_ = builder.Bytes()
		Release(builder)
	}
}

// ---------------------------------------------------------------------------
// AppendRaw / AppendRawString / AppendEscapedString
// ---------------------------------------------------------------------------

func TestBuilder_AppendRaw(t *testing.T) {
	b := New(64)
	b.AppendRaw([]byte(`{"k":`))
	b.AppendRaw([]byte(`"v"}`))
	expect(t, `{"k":"v"}`, string(b.Bytes()))
}

func TestBuilder_AppendRawString(t *testing.T) {
	b := New(64)
	b.AppendRawString(`{"k":`)
	b.AppendRawString(`"v"}`)
	expect(t, `{"k":"v"}`, string(b.Bytes()))
}

func TestBuilder_AppendEscapedString(t *testing.T) {
	b := New(64)
	b.AppendRawString(`"`)
	b.AppendEscapedString("line1\nline2")
	b.AppendRawString(`"`)
	expect(t, `"line1\nline2"`, string(b.Bytes()))
}

func TestBuilder_AppendEscapedString_NoEscape(t *testing.T) {
	b := New(64)
	b.AppendEscapedString("hello world")
	expect(t, "hello world", string(b.Bytes()))
}

// ---------------------------------------------------------------------------
// AddNestedStringMapField — large map (>8 keys, heap sort path)
// ---------------------------------------------------------------------------

func TestBuilder_AddNestedStringMapField_LargeMap(t *testing.T) {
	b := New(2048)
	b.BeginObject()
	m := make(map[string]map[string]string)
	inner := make(map[string]string)
	for i := range 10 {
		inner[fmt.Sprintf("k%02d", i)] = fmt.Sprintf("v%02d", i)
	}
	m["outer"] = inner
	b.AddNestedStringMapField("sd", m)
	b.EndObject()

	var parsed map[string]any
	if err := json.Unmarshal(b.Bytes(), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
}

// ---------------------------------------------------------------------------
// appendNano — trailing zero trimming
// ---------------------------------------------------------------------------

func TestBuilder_AddTimeRFC3339Field_AllNanoDigits(t *testing.T) {
	ts := time.Date(2024, 1, 1, 0, 0, 0, 100000000, time.UTC)
	b := New(128)
	b.BeginObject()
	b.AddTimeRFC3339Field("ts", ts)
	b.EndObject()
	assertContains(t, string(b.Bytes()), ".1Z")
}

// ---------------------------------------------------------------------------
// IsLikelyJSON
// ---------------------------------------------------------------------------

func TestIsLikelyJSON(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{`{"key":"value"}`, true},
		{`[1,2,3]`, true},
		{`{}`, true},
		{`[]`, true},
		{`{`, false},
		{`}`, false},
		{``, false},
		{`x`, false},
		{`"hello"`, false},
		{`not json`, false},
	}
	for _, tt := range tests {
		if got := IsLikelyJSON(tt.input); got != tt.want {
			t.Errorf("IsLikelyJSON(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// AddNullField
// ---------------------------------------------------------------------------

func TestBuilder_AddNullField(t *testing.T) {
	b := New(64)
	b.BeginObject()
	b.AddNullField("data")
	b.EndObject()
	expect(t, `{"data":null}`, string(b.Bytes()))
}

// ---------------------------------------------------------------------------
// AddStringMapObject
// ---------------------------------------------------------------------------

func TestAddStringMapObject_WithRawJSON(t *testing.T) {
	b := New(256)
	m := map[string]string{
		"object": `{"a":1}`,
		"host":   "fw01",
	}
	b.AddStringMapObject(m, "object")
	got := string(b.Bytes())

	var parsed map[string]any
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v\ngot: %s", err, got)
	}
	assertContains(t, got, `"object":{"a":1}`)
	assertContains(t, got, `"host":"fw01"`)
}

func TestAddStringMapObject_SortedKeys(t *testing.T) {
	b := New(256)
	m := map[string]string{
		"zulu":  "z",
		"alpha": "a",
		"mike":  "m",
	}
	b.AddStringMapObject(m, "")
	got := string(b.Bytes())

	alphaIdx := strings.Index(got, "alpha")
	mikeIdx := strings.Index(got, "mike")
	zuluIdx := strings.Index(got, "zulu")
	if mikeIdx <= alphaIdx {
		t.Errorf("mike should come after alpha in %s", got)
	}
	if zuluIdx <= mikeIdx {
		t.Errorf("zulu should come after mike in %s", got)
	}
}

func TestAddStringMapObject_NoRawKey(t *testing.T) {
	b := New(128)
	m := map[string]string{"key": "value"}
	b.AddStringMapObject(m, "")
	expect(t, `{"key":"value"}`, string(b.Bytes()))
}

func TestAddStringMapObject_Empty(t *testing.T) {
	b := New(64)
	b.AddStringMapObject(map[string]string{}, "")
	expect(t, `{}`, string(b.Bytes()))
}

func TestAddStringMapObject_RawKeyNotJSON(t *testing.T) {
	b := New(128)
	m := map[string]string{"object": "plain text"}
	b.AddStringMapObject(m, "object")
	expect(t, `{"object":"plain text"}`, string(b.Bytes()))
}

// ---------------------------------------------------------------------------
// digitPairs table
// ---------------------------------------------------------------------------

func TestDigitPairs(t *testing.T) {
	for i := range 100 {
		hi := digitPairs[i*2]
		lo := digitPairs[i*2+1]
		want := fmt.Sprintf("%02d", i)
		got := string([]byte{hi, lo})
		if got != want {
			t.Errorf("digitPairs[%d] = %q, want %q", i, got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func expect(t *testing.T, want, got string) {
	t.Helper()
	if got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

func assertContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("expected %q to contain %q", haystack, needle)
	}
}

// ---------------------------------------------------------------------------
// Acquire / Release (Builder pool)
// ---------------------------------------------------------------------------

func TestAcquireRelease(t *testing.T) {
	b := Acquire()
	if b == nil {
		t.Fatal("Acquire() returned nil")
	}
	b.BeginObject()
	b.AddStringField("k", "v")
	b.EndObject()
	expect(t, `{"k":"v"}`, string(b.Bytes()))
	Release(b)
}

func TestAcquireRelease_Reuse(t *testing.T) {
	b1 := Acquire()
	b1.BeginObject()
	b1.AddStringField("first", "1")
	b1.EndObject()
	Release(b1)

	b2 := Acquire()
	if b2.Len() != 0 {
		t.Errorf("expected Len()=0 after Acquire, got %d", b2.Len())
	}
	b2.BeginObject()
	b2.AddStringField("second", "2")
	b2.EndObject()
	expect(t, `{"second":"2"}`, string(b2.Bytes()))
	Release(b2)
}

func TestRelease_Nil(_ *testing.T) {
	Release(nil)
}

func TestAcquire_PoolCapacity(t *testing.T) {
	b := Acquire()
	if cap(b.buf) < 2048 {
		t.Errorf("expected pool capacity >= 2048, got %d", cap(b.buf))
	}
	Release(b)
}

func TestAddRawJSONFieldString(t *testing.T) {
	b := New(256)
	b.BeginObject()
	b.AddRawJSONFieldString("nested", `{"x":1}`)
	b.EndObject()
	expect(t, `{"nested":{"x":1}}`, string(b.Bytes()))
}

func TestAddRawJSONFieldString_Multiple(t *testing.T) {
	b := New(256)
	b.BeginObject()
	b.AddRawJSONFieldString("a", `[1,2]`)
	b.AddRawJSONFieldString("b", `true`)
	b.EndObject()
	expect(t, `{"a":[1,2],"b":true}`, string(b.Bytes()))
}

func FuzzEscapeString(f *testing.F) {
	f.Add("")
	f.Add("hello world")
	f.Add("line1\nline2")
	f.Add("tab\there")
	f.Add(`with "quotes"`)
	f.Add(`with \backslash`)
	f.Add("control\x00chars\x01here")
	f.Add("日本語テスト")
	f.Add("emoji 🎉🔥")
	f.Add(string([]byte{0xff, 0xfe}))       // invalid UTF-8
	f.Add(string([]byte{0xC2}))             // truncated 2-byte
	f.Add(string([]byte{0xE0, 0x80}))       // truncated 3-byte
	f.Add(string([]byte{0xF0, 0x90, 0x80})) // truncated 4-byte
	f.Add(string([]byte{0x80}))             // bare continuation
	f.Add(string([]byte{0xF8}))             // invalid lead byte
	f.Add(string([]byte{0xC2, 0x00}))       // invalid continuation

	f.Fuzz(func(t *testing.T, s string) {
		escaped := EscapeString(s)
		jsonStr := `"` + escaped + `"`
		var parsed string
		if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
			t.Errorf("EscapeString(%q) produced invalid JSON: %v\njsonStr: %s", s, err, jsonStr)
		}
		if !utf8.ValidString(escaped) {
			t.Errorf("EscapeString(%q) produced invalid UTF-8", s)
		}
	})
}

// ---------------------------------------------------------------------------
// validContinuation edge cases
// ---------------------------------------------------------------------------

func TestValidContinuation_Size2_Invalid(t *testing.T) {
	// 0xC2 followed by a non-continuation byte.
	s := string([]byte{0xC2, 0x00})
	if validContinuation(s, 0, 2) {
		t.Error("expected false for bad 2-byte continuation")
	}
}

func TestValidContinuation_Size3_Invalid(t *testing.T) {
	// Valid first continuation, invalid second.
	s := string([]byte{0xE0, 0x80, 0x00})
	if validContinuation(s, 0, 3) {
		t.Error("expected false for bad 3-byte continuation")
	}
	// Invalid first continuation.
	s = string([]byte{0xE0, 0x00, 0x80})
	if validContinuation(s, 0, 3) {
		t.Error("expected false for bad 3-byte first continuation")
	}
}

func TestValidContinuation_Size4_Invalid(t *testing.T) {
	// All permutations of bad continuation bytes.
	cases := [][]byte{
		{0xF0, 0x00, 0x80, 0x80}, // first bad
		{0xF0, 0x80, 0x00, 0x80}, // second bad
		{0xF0, 0x80, 0x80, 0x00}, // third bad
	}
	for i, c := range cases {
		s := string(c)
		if validContinuation(s, 0, 4) {
			t.Errorf("case %d: expected false", i)
		}
	}
}

func TestValidContinuation_InvalidSize(t *testing.T) {
	s := "abcde"
	if validContinuation(s, 0, 5) {
		t.Error("expected false for invalid size")
	}
	if validContinuation(s, 0, 1) {
		t.Error("expected false for size 1")
	}
}

// ---------------------------------------------------------------------------
// EscapeString edge cases
// ---------------------------------------------------------------------------

func TestEscapeString_InvalidUTF8(t *testing.T) {
	// Bare continuation byte should be replaced with U+FFFD.
	s := string([]byte{0x80})
	got := EscapeString(s)
	if got != "\ufffd" {
		t.Errorf("got %q, want U+FFFD", got)
	}
}

func TestEscapeString_Truncated2Byte(t *testing.T) {
	s := string([]byte{0xC2})
	got := EscapeString(s)
	if got != "\ufffd" {
		t.Errorf("got %q, want U+FFFD", got)
	}
}

func TestEscapeString_Truncated3Byte(t *testing.T) {
	s := string([]byte{0xE0, 0xA0})
	got := EscapeString(s)
	// Each invalid byte replaced individually → 2 × U+FFFD.
	want := "\ufffd\ufffd"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestEscapeString_Truncated4Byte(t *testing.T) {
	s := string([]byte{0xF0, 0x90, 0x80})
	got := EscapeString(s)
	// Each invalid byte replaced individually → 3 × U+FFFD.
	want := "\ufffd\ufffd\ufffd"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestEscapeString_LongNeedsEscape(t *testing.T) {
	// Trigger the non-stackBuf path: >506 byte string with a special char.
	s := string(make([]byte, 510)) + "\n"
	got := EscapeString(s)
	if len(got) < 510 {
		t.Errorf("expected long output, got len=%d", len(got))
	}
}

// ---------------------------------------------------------------------------
// civilDate edge cases
// ---------------------------------------------------------------------------

func TestCivilDate_NegativeTimestamp(t *testing.T) {
	y, m, d := civilDate(-1)
	// Negative clamped to 0 → 1970-01-01.
	if y != 1970 || m != 1 || d != 1 {
		t.Errorf("got %d-%d-%d, want 1970-1-1", y, m, d)
	}
}

func TestCivilDate_Epoch(t *testing.T) {
	y, m, d := civilDate(0)
	if y != 1970 || m != 1 || d != 1 {
		t.Errorf("got %d-%d-%d, want 1970-1-1", y, m, d)
	}
}

func TestCivilDate_Y2K(t *testing.T) {
	// 2000-01-01 00:00:00 UTC = 946684800
	y, m, d := civilDate(946684800)
	if y != 2000 || m != 1 || d != 1 {
		t.Errorf("got %d-%d-%d, want 2000-1-1", y, m, d)
	}
}

func TestCivilDate_LeapYear(t *testing.T) {
	// 2024-02-29 00:00:00 UTC = 1709164800
	y, m, d := civilDate(1709164800)
	if y != 2024 || m != 2 || d != 29 {
		t.Errorf("got %d-%d-%d, want 2024-2-29", y, m, d)
	}
}

// ---------------------------------------------------------------------------
// Release edge case
// ---------------------------------------------------------------------------

func TestRelease_LargeBuffer(_ *testing.T) {
	b := Acquire()
	// Grow buffer beyond 256 KB to ensure it's discarded.
	b.buf = make([]byte, 0, 1<<19)
	Release(b)
	// No assertion needed — just verify no panic.
}
