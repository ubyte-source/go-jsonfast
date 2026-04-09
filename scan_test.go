package jsonfast

import "testing"

func TestSkipWS(t *testing.T) {
	tests := []struct {
		data string
		i    int
		want int
	}{
		{"  abc", 0, 2},
		{"\t\n x", 0, 3},
		{"abc", 0, 0},
		{"", 0, 0},
	}
	for _, tt := range tests {
		if got := SkipWS([]byte(tt.data), tt.i); got != tt.want {
			t.Errorf("SkipWS(%q,%d)=%d, want %d", tt.data, tt.i, got, tt.want)
		}
	}
}

func TestSkipStringAt(t *testing.T) {
	tests := []struct {
		data string
		n    int
		ok   bool
	}{
		{`"hello"`, 7, true},
		{`"he\"lo"`, 8, true},
		{`"a\\b"`, 6, true},
		{`""`, 2, true},
		{`"unterminated`, 0, false},
		{`notstring`, 0, false},
		{"\"ctrl\x01\"", 0, false}, // control char
	}
	for _, tt := range tests {
		end, ok := SkipStringAt([]byte(tt.data), 0)
		n := end
		if !ok {
			n = 0
		}
		if n != tt.n || ok != tt.ok {
			t.Errorf("SkipStringAt(%q,0)=(%d,%v), want (%d,%v)",
				tt.data, n, ok, tt.n, tt.ok)
		}
	}
}

func TestSkipBracedAt(t *testing.T) {
	tests := []struct {
		data string
		n    int
		ok   bool
	}{
		{`{"a":"b"}`, 9, true},
		{`{"nested":{"x":1}}`, 18, true},
		{`[1,2,3]`, 7, true},
		{`{"s":"with\"quote"}`, 19, true},
		{`{unclosed`, 0, false},
	}
	for _, tt := range tests {
		closer := byte('}')
		if tt.data[0] == '[' {
			closer = ']'
		}
		end, ok := SkipBracedAt([]byte(tt.data), 0,
			tt.data[0], closer)
		n := end
		if !ok {
			n = 0
		}
		if n != tt.n || ok != tt.ok {
			t.Errorf("SkipBracedAt(%q)=(%d,%v), want (%d,%v)",
				tt.data, n, ok, tt.n, tt.ok)
		}
	}
}

func TestSkipValueAt(t *testing.T) {
	tests := []struct {
		data string
		n    int
		ok   bool
	}{
		{`"hello"`, 7, true},
		{`123`, 3, true},
		{`true`, 4, true},
		{`null`, 4, true},
		{`{"a":1}`, 7, true},
		{`[1,2]`, 5, true},
		{`false,next`, 5, true},
		{``, 0, false},
	}
	for _, tt := range tests {
		n, ok := SkipValueAt([]byte(tt.data), 0)
		if n != tt.n || ok != tt.ok {
			t.Errorf("SkipValueAt(%q,0)=(%d,%v), want (%d,%v)",
				tt.data, n, ok, tt.n, tt.ok)
		}
	}
}

func TestIterateFields(t *testing.T) {
	data := []byte(`{"a":"1","b":2,"c":true}`)
	var keys []string
	ok := IterateFields(data, func(key, _ []byte) bool {
		keys = append(keys, string(key))
		return true
	})
	if !ok {
		t.Fatal("IterateFields returned false")
	}
	if len(keys) != 3 {
		t.Fatalf("expected 3 keys, got %d", len(keys))
	}
	want := []string{`"a"`, `"b"`, `"c"`}
	for i := range keys {
		if keys[i] != want[i] {
			t.Errorf("key[%d]=%q, want %q", i, keys[i], want[i])
		}
	}
}

func TestIterateFields_EmptyObject(t *testing.T) {
	if !IterateFields([]byte(`{}`), func(_, _ []byte) bool { return true }) {
		t.Error("expected true for empty object")
	}
}

func TestIterateFields_Malformed(t *testing.T) {
	if IterateFields([]byte(`not json`), func(_, _ []byte) bool { return true }) {
		t.Error("expected false for malformed")
	}
}

func TestIterateFields_MalformedCases(t *testing.T) {
	nop := func(_, _ []byte) bool { return true }
	cases := []struct {
		name string
		data string
	}{
		{"empty", ""},
		{"not_object", `"string"`},
		{"missing_key_quote", `{abc:"val"}`},
		{"truncated_key", `{"`},
		{"missing_colon", `{"key" "val"}`},
		{"truncated_after_colon", `{"key":`},
		{"bad_value", `{"key":}`},
		{"missing_comma_or_brace", `{"a":1 "b":2}`},
		{"truncated_after_value", `{"a":1`},
		{"truncated_after_comma", `{"a":1,`},
		{"truncated_mid_key", `{"a":1,"`},
	}
	for _, tt := range cases {
		if IterateFields([]byte(tt.data), nop) {
			t.Errorf("%s: expected false", tt.name)
		}
	}
}

func TestIterateFields_CallbackFalse(t *testing.T) {
	data := []byte(`{"a":1,"b":2}`)
	count := 0
	ok := IterateFields(data, func(_, _ []byte) bool {
		count++
		return false // stop after first
	})
	if ok {
		t.Error("expected false when callback returns false")
	}
	if count != 1 {
		t.Errorf("expected 1 callback, got %d", count)
	}
}

func TestFindField(t *testing.T) {
	data := []byte(`{"name":"alice","age":30,"active":true}`)

	val, ok := FindField(data, "age")
	if !ok || string(val) != "30" {
		t.Errorf("FindField(age)=%q,%v, want 30,true", val, ok)
	}

	val, ok = FindField(data, "name")
	if !ok || string(val) != `"alice"` {
		t.Errorf("FindField(name)=%q,%v, want \"alice\",true", val, ok)
	}

	_, ok = FindField(data, "missing")
	if ok {
		t.Error("expected false for missing field")
	}
}

func TestFindField_MalformedCases(t *testing.T) {
	cases := []struct {
		name string
		data string
		key  string
	}{
		{"empty", "", "k"},
		{"not_object", `"string"`, "k"},
		{"missing_key_quote", `{abc:"val"}`, "abc"},
		{"truncated_key", `{"`, "k"},
		{"missing_colon", `{"key" "val"}`, "key"},
		{"truncated_after_colon", `{"key":`, "key"},
		{"bad_value", `{"key":}`, "key"},
		{"missing_comma_or_brace", `{"a":1 "b":2}`, "b"},
		{"truncated_after_comma", `{"a":1,`, "b"},
		{"truncated_after_value", `{"a":1`, "b"},
		{"key_not_found_at_end", `{"a":1}`, "b"},
	}
	for _, tt := range cases {
		val, ok := FindField([]byte(tt.data), tt.key)
		if ok {
			t.Errorf("%s: expected false, got val=%q", tt.name, val)
		}
	}
}

func TestFindField_KeyInMiddle(t *testing.T) {
	data := []byte(`{"x":10,"target":"found","z":30}`)
	val, ok := FindField(data, "target")
	if !ok || string(val) != `"found"` {
		t.Errorf("got %q,%v, want \"found\",true", val, ok)
	}
}

func TestFindField_EmptyObject(t *testing.T) {
	_, ok := FindField([]byte(`{}`), "k")
	if ok {
		t.Error("expected false for empty object")
	}
}

func TestFlattenObject_EmptyObject(t *testing.T) {
	b := New(128)
	b.BeginObject()
	ok := FlattenObject(b, []byte(`{}`))
	if !ok {
		t.Error("expected true for empty object")
	}
	b.EndObject()
	expect(t, `{}`, string(b.Bytes()))
}

func TestFlattenObject_Simple(t *testing.T) {
	sd := []byte(`{"KV@123":{"action":"pass","srcip":"1.2.3.4"}}`)
	b := New(256)
	b.BeginObject()
	b.AddStringField("hostname", "FW01")
	if !FlattenObject(b, sd) {
		t.Fatal("FlattenObject returned false")
	}
	b.EndObject()

	got := string(b.Bytes())
	want := `{"hostname":"FW01","action":"pass","srcip":"1.2.3.4"}`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestFlattenObject_DeepNesting(t *testing.T) {
	sd := []byte(`{"L1":{"L2":{"key":"deep"}}}`)
	b := New(256)
	b.BeginObject()
	if !FlattenObject(b, sd) {
		t.Fatal("FlattenObject returned false")
	}
	b.EndObject()

	got := string(b.Bytes())
	want := `{"key":"deep"}`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestFlattenObject_MultipleSDIDs(t *testing.T) {
	sd := []byte(`{"KV@1":{"a":"1"},"KV@2":{"b":"2","c":"3"}}`)
	b := New(256)
	b.BeginObject()
	if !FlattenObject(b, sd) {
		t.Fatal("FlattenObject returned false")
	}
	b.EndObject()

	got := string(b.Bytes())
	// Keys appear in iteration order (object order preserved)
	want := `{"a":"1","b":"2","c":"3"}`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestFlattenObject_LeafOnly(t *testing.T) {
	sd := []byte(`{"key":"value","num":42}`)
	b := New(256)
	b.BeginObject()
	if !FlattenObject(b, sd) {
		t.Fatal("FlattenObject returned false")
	}
	b.EndObject()

	got := string(b.Bytes())
	want := `{"key":"value","num":42}`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestFlattenObject_NonObject(t *testing.T) {
	b := New(64)
	b.BeginObject()
	if !FlattenObject(b, []byte(`"just a string"`)) {
		t.Fatal("non-object should return true")
	}
	if !FlattenObject(b, []byte(`null`)) {
		t.Fatal("null should return true")
	}
	b.EndObject()
	if string(b.Bytes()) != `{}` {
		t.Errorf("expected empty object, got %s", b.Bytes())
	}
}

func TestFlattenObject_Malformed(t *testing.T) {
	b := New(64)
	b.BeginObject()
	if FlattenObject(b, []byte(`{broken`)) {
		t.Error("expected false for malformed JSON")
	}
}

func TestFlattenObject_DepthLimit(t *testing.T) {
	// Build a JSON object nested 100 levels deep — exceeds maxFlattenDepth (64).
	nested := make([]byte, 0, 606)
	for range 100 {
		nested = append(nested, `{"k":`...)
	}
	nested = append(nested, `"deep"`...)
	for range 100 {
		nested = append(nested, '}')
	}
	b := New(256)
	b.BeginObject()
	if FlattenObject(b, nested) {
		t.Error("expected false for excessively nested input")
	}
}

func TestFlattenObject_WithinDepthLimit(t *testing.T) {
	// Build a JSON object nested 10 levels deep — within limit.
	nested := make([]byte, 0, 64)
	for range 10 {
		nested = append(nested, `{"k":`...)
	}
	nested = append(nested, `"ok"`...)
	for range 10 {
		nested = append(nested, '}')
	}
	b := New(256)
	b.BeginObject()
	if !FlattenObject(b, nested) {
		t.Error("expected true for moderately nested input")
	}
	b.EndObject()
	assertContains(t, string(b.Bytes()), `"k":"ok"`)
}

func TestAddRawBytesField(t *testing.T) {
	b := New(128)
	b.BeginObject()
	b.AddRawBytesField([]byte("key"), []byte(`"value"`))
	b.AddRawBytesField([]byte("num"), []byte("42"))
	b.EndObject()

	got := string(b.Bytes())
	want := `{"key":"value","num":42}`
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func BenchmarkFlattenObject(b *testing.B) {
	sd := []byte(`{"KV@32473":{"action":"pass","srcip":"1.2.3.4",` +
		`"dstip":"5.6.7.8","service":"HTTP","srcport":"54321","dstport":"80"}}`)
	builder := New(512)
	b.ReportAllocs()
	b.SetBytes(int64(len(sd)))
	b.ResetTimer()
	for b.Loop() {
		builder.Reset()
		builder.BeginObject()
		FlattenObject(builder, sd)
		builder.EndObject()
	}
}

func BenchmarkIterateFields(b *testing.B) {
	data := []byte(`{"facility":23,"severity":3,"hostname":"FW01","app_name":"utm","source":"10.0.0.1","message":"test"}`)
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for b.Loop() {
		IterateFields(data, func(_, _ []byte) bool { return true })
	}
}

func BenchmarkFindField(b *testing.B) {
	data := []byte(`{"facility":23,"severity":3,"hostname":"FW01","app_name":"utm","source":"10.0.0.1"}`)
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for b.Loop() {
		FindField(data, "source")
	}
}

func TestIterateFieldsString(t *testing.T) {
	s := `{"a":1,"b":"hello"}`
	var keys []string
	ok := IterateFieldsString(s, func(key, _ []byte) bool {
		keys = append(keys, string(key))
		return true
	})
	if !ok {
		t.Fatal("expected true")
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(keys))
	}
	if keys[0] != `"a"` || keys[1] != `"b"` {
		t.Errorf("unexpected keys: %v", keys)
	}
}

func TestIterateFieldsString_Empty(t *testing.T) {
	if IterateFieldsString("", func(_, _ []byte) bool { return true }) {
		t.Error("expected false for empty string")
	}
}

func TestIterateFieldsString_Invalid(t *testing.T) {
	if IterateFieldsString("not json", func(_, _ []byte) bool { return true }) {
		t.Error("expected false for invalid JSON")
	}
}

// ---------------------------------------------------------------------------
// SkipStringAt edge cases
// ---------------------------------------------------------------------------

func TestSkipStringAt_ControlChar(t *testing.T) {
	// Raw control char inside string must be rejected per RFC 8259.
	data := []byte{'"', 0x01, '"'}
	_, ok := SkipStringAt(data, 0)
	if ok {
		t.Error("expected false for control char in string")
	}
}

func TestSkipStringAt_BackslashAtEnd(t *testing.T) {
	data := []byte(`"\`)
	_, ok := SkipStringAt(data, 0)
	if ok {
		t.Error("expected false for backslash at end")
	}
}

func TestSkipStringAt_Unterminated(t *testing.T) {
	data := []byte(`"hello`)
	_, ok := SkipStringAt(data, 0)
	if ok {
		t.Error("expected false for unterminated string")
	}
}

func TestSkipStringAt_NotQuote(t *testing.T) {
	data := []byte(`abc`)
	_, ok := SkipStringAt(data, 0)
	if ok {
		t.Error("expected false when not starting with quote")
	}
}

func TestSkipStringAt_Empty(t *testing.T) {
	_, ok := SkipStringAt(nil, 0)
	if ok {
		t.Error("expected false for nil input")
	}
}

func TestSkipStringAt_LongStringWithEscape(t *testing.T) {
	// String long enough to trigger SWAR, with escape near the end.
	data := []byte(`"` + "aaaaaaaaaaaaaaaaaaa\\nbb" + `"`)
	end, ok := SkipStringAt(data, 0)
	if !ok || end != len(data) {
		t.Errorf("got end=%d ok=%v, want end=%d ok=true", end, ok, len(data))
	}
}

func TestSkipStringAt_LongStringWithControlChar(t *testing.T) {
	// 20 bytes of 'a' then a control char — SWAR should skip past safe bytes.
	buf := make([]byte, 23)
	buf[0] = '"'
	for i := 1; i <= 20; i++ {
		buf[i] = 'a'
	}
	buf[21] = 0x05 // control char
	buf[22] = '"'
	_, ok := SkipStringAt(buf, 0)
	if ok {
		t.Error("expected false for control char in long string")
	}
}

func TestSkipStringAt_VeryLongSafe(t *testing.T) {
	// 64 safe bytes — exercises the 16-byte SWAR unrolled loop fully.
	buf := make([]byte, 66)
	buf[0] = '"'
	for i := 1; i <= 64; i++ {
		buf[i] = 'x'
	}
	buf[65] = '"'
	end, ok := SkipStringAt(buf, 0)
	if !ok || end != 66 {
		t.Errorf("got end=%d ok=%v, want end=66 ok=true", end, ok)
	}
}

func TestSkipStringAt_EscapeIn2ndWord(t *testing.T) {
	// 8 safe bytes then a quote in the 9th byte (second SWAR word).
	s := `"12345678"`
	end, ok := SkipStringAt([]byte(s), 0)
	if !ok || end != len(s) {
		t.Errorf("got end=%d ok=%v, want end=%d ok=true", end, ok, len(s))
	}
}

func TestSkipStringAt_QuoteIn2ndSWARWord(t *testing.T) {
	// j starts at 1; we need j+16 <= n.
	// 8 safe bytes for w1, then closing quote in w2 at position 9.
	// Total data: 1(open) + 8(safe) + 1(close) + 7(pad) = 17 bytes.
	buf := make([]byte, 17)
	buf[0] = '"'
	for i := 1; i <= 8; i++ {
		buf[i] = 'a'
	}
	buf[9] = '"'
	for i := 10; i < 17; i++ {
		buf[i] = 'b'
	}
	end, ok := SkipStringAt(buf, 0)
	if !ok || end != 10 {
		t.Errorf("got end=%d ok=%v, want end=10 ok=true", end, ok)
	}
}

// ---------------------------------------------------------------------------
// flattenObject edge cases (extended)
// ---------------------------------------------------------------------------

func TestFlattenObject_MalformedCases(t *testing.T) {
	cases := []struct {
		name string
		data string
	}{
		{"missing_key_quote", `{abc:"val"}`},
		{"truncated_key", `{"`},
		{"missing_colon", `{"key" "val"}`},
		{"truncated_after_colon", `{"key":`},
		{"bad_value", `{"key": }`},
		{"missing_comma_or_brace", `{"a":1 "b":2}`},
		{"truncated_mid_object", `{"a":`},
		{"truncated_after_value", `{"a":1`},
		{"truncated_after_comma", `{"a":1,`},
	}
	for _, tt := range cases {
		b := New(128)
		b.BeginObject()
		ok := FlattenObject(b, []byte(tt.data))
		if ok {
			t.Errorf("%s: expected false", tt.name)
		}
	}
}

func TestFlattenObject_NestedMalformed(t *testing.T) {
	// Inner object is malformed.
	b := New(128)
	b.BeginObject()
	ok := FlattenObject(b, []byte(`{"k":{bad}}`))
	if ok {
		t.Error("expected false for malformed inner object")
	}
}

// ---------------------------------------------------------------------------
// SkipBracedAt edge cases
// ---------------------------------------------------------------------------

func TestSkipBracedAt_UnterminatedString(t *testing.T) {
	// Brace containing an unterminated string.
	data := []byte(`{"key":"unterminated`)
	_, ok := SkipBracedAt(data, 0, '{', '}')
	if ok {
		t.Error("expected false for unterminated string inside braces")
	}
}

func TestSkipBracedAt_BadStart(t *testing.T) {
	data := []byte(`hello`)
	_, ok := SkipBracedAt(data, 0, '{', '}')
	if ok {
		t.Error("expected false when not starting with opener")
	}
}

func TestSkipBracedAt_UnterminatedBrace(t *testing.T) {
	data := []byte(`{`)
	_, ok := SkipBracedAt(data, 0, '{', '}')
	if ok {
		t.Error("expected false for unterminated brace")
	}
}
