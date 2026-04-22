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
		{"\"ctrl\x01\"", 0, false},
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
		{`-42`, 3, true},
		{`0.5`, 3, true},
		{`1.5e10`, 6, true},
		{`1E+5`, 4, true},
		{`true`, 4, true},
		{`null`, 4, true},
		{`{"a":1}`, 7, true},
		{`[1,2]`, 5, true},
		{`false,next`, 5, true},
		{``, 0, false},

		{`00`, 1, true},
		{`01`, 1, true},
		{`truex`, 4, true},

		{`.5`, 0, false},
		{`-`, 0, false},
		{`1.`, 0, false},
		{`1e`, 0, false},
		{`1e+`, 0, false},
		{`tru`, 0, false},
		{`fals`, 0, false},
		{`nul`, 0, false},
	}
	for _, tt := range tests {
		n, ok := SkipValueAt([]byte(tt.data), 0)
		if n != tt.n || ok != tt.ok {
			t.Errorf("SkipValueAt(%q,0)=(%d,%v), want (%d,%v)",
				tt.data, n, ok, tt.n, tt.ok)
		}
	}
}

func TestFindField_EscapedKey(t *testing.T) {
	data := []byte(`{"simple":1,"quoted\"name":2,"slash\\path":3,"tabbed\tkey":4}`)
	cases := []struct {
		key  string
		want string
	}{
		{"simple", "1"},
		{"quoted\"name", "2"},
		{"slash\\path", "3"},
		{"tabbed\tkey", "4"},
	}
	for _, tt := range cases {
		val, ok := FindField(data, tt.key)
		if !ok {
			t.Errorf("FindField(%q) not found", tt.key)
			continue
		}
		if string(val) != tt.want {
			t.Errorf("FindField(%q) = %s, want %s", tt.key, val, tt.want)
		}
	}
}

func TestFindField_EscapedKeyUnicode(t *testing.T) {
	data := []byte(`{"caf\u00e9":1}`)
	val, ok := FindField(data, "café")
	if !ok || string(val) != "1" {
		t.Errorf("FindField(café) = %q,%v, want 1,true", val, ok)
	}
}

func TestFindField_EscapedKeySurrogatePair(t *testing.T) {
	data := []byte(`{"rocket \ud83d\ude80":42}`)
	val, ok := FindField(data, "rocket 🚀")
	if !ok || string(val) != "42" {
		t.Errorf("FindField(rocket 🚀) = %q,%v, want 42,true", val, ok)
	}
}

func TestFindField_EscapedKey_Mismatch(t *testing.T) {
	data := []byte(`{"quoted\"name":1}`)
	if _, ok := FindField(data, `quoted\"name`); ok {
		t.Error("expected no match for key with literal backslash-quote")
	}
	if _, ok := FindField(data, "quoted\"na"); ok {
		t.Error("expected no match for truncated key")
	}
}

func TestFindField_EscapedKey_CodepointSizes(t *testing.T) {
	cases := []struct {
		encoded string
		key     string
		want    string
	}{
		{`{"\u0041":"one"}`, "A", `"one"`},
		{`{"\u00e9":"two"}`, "é", `"two"`},
		{`{"\u4e2d":"three"}`, "中", `"three"`},
		{`{"\ud83d\ude80":"four"}`, "🚀", `"four"`},
	}
	for _, tt := range cases {
		val, ok := FindField([]byte(tt.encoded), tt.key)
		if !ok {
			t.Errorf("%s: FindField(%q) not found", tt.encoded, tt.key)
			continue
		}
		if string(val) != tt.want {
			t.Errorf("%s: FindField(%q) = %q, want %q", tt.encoded, tt.key, val, tt.want)
		}
	}
	for _, enc := range []string{
		`{"\u00e9":1}`,
		`{"\u4e2d":1}`,
		`{"\ud83d\ude80":1}`,
	} {
		if _, ok := FindField([]byte(enc), "x"); ok {
			t.Errorf("%s: expected no match for short key", enc)
		}
	}
}

func TestParseHex4_UppercaseLetters(t *testing.T) {
	data := []byte(`{"\u00FF":1}`)
	val, ok := FindField(data, "\u00ff")
	if !ok || string(val) != "1" {
		t.Errorf("uppercase hex path: got (%q,%v)", val, ok)
	}
}

func TestSwarBroadcastPair_NonDefaultPair(t *testing.T) {
	data := []byte("(a(b)c)")
	end, ok := SkipBracedAt(data, 0, '(', ')')
	if !ok || end != len(data) {
		t.Errorf("SkipBracedAt with custom delimiters: got (%d,%v)", end, ok)
	}
}

// Validator error paths.

func TestIsStructuralJSON_ValidateValue_UnexpectedEOF(t *testing.T) {
	if IsStructuralJSON(`{"k":   `) {
		t.Error("expected false for truncated object value")
	}
}

func TestIsStructuralJSON_ValidateObject_UnexpectedEOF(t *testing.T) {
	if IsStructuralJSON(`{"k":1`) {
		t.Error("expected false for truncated object")
	}
}

func TestIsStructuralJSON_ValidateArray_UnexpectedEOF(t *testing.T) {
	if IsStructuralJSON(`[1,`) {
		t.Error("expected false for truncated array")
	}
}

func TestIsStructuralJSON_ExpectComma_NotComma(t *testing.T) {
	if IsStructuralJSON(`{"a":1 "b":2}`) {
		t.Error("expected false for missing comma between fields")
	}
}

func TestIsStructuralJSON_ExpectComma_CommaThenEOF(t *testing.T) {
	if IsStructuralJSON(`{"a":1,`) {
		t.Error("expected false for trailing comma at EOF")
	}
}

func TestIsStructuralJSON_ObjectEntry_KeyNotString(t *testing.T) {
	if IsStructuralJSON(`{ 1:2}`) {
		t.Error("expected false for non-string object key")
	}
}

func TestIsStructuralJSON_ObjectEntry_MalformedString(t *testing.T) {
	data := []byte("{\"bad\x01key\":1}")
	if IsStructuralJSON(string(data)) {
		t.Error("expected false for key with raw control byte")
	}
}

func TestIsStructuralJSON_ObjectEntry_MissingColon(t *testing.T) {
	if IsStructuralJSON(`{"k" 1}`) {
		t.Error("expected false for missing colon")
	}
}

func TestIsStructuralJSON_SkipLiteral_Mismatch(t *testing.T) {
	if IsStructuralJSON(`{"k":tree}`) {
		t.Error("expected false for 'tree' literal")
	}
	if IsStructuralJSON(`{"k":fause}`) {
		t.Error("expected false for 'fause' literal")
	}
	if IsStructuralJSON(`{"k":nunl}`) {
		t.Error("expected false for 'nunl' literal")
	}
}

// FindField escape-comparator error paths.

func TestFindField_MatchEscape_BackslashAtEOF(t *testing.T) {
	data := []byte(`{"a\\":1}`)
	if _, ok := FindField(data, "b"); ok {
		t.Error("expected no match for mismatched key against escape body")
	}
}

func TestFindField_MatchEscape_BadShortEscape(t *testing.T) {
	data := []byte(`{"a\zb":1}`)
	if _, ok := FindField(data, "ab"); ok {
		t.Error("expected no match when key contains an invalid escape")
	}
}

func TestFindField_MatchEscape_ShortEscapeWrongByte(t *testing.T) {
	data := []byte(`{"a\tb":1}`)
	if _, ok := FindField(data, "a b"); ok {
		t.Error("expected no match for tab→space mismatch")
	}
}

func TestFindField_MatchEscape_UnicodeSurrogateFailure(t *testing.T) {
	data := []byte(`{"\ud83d":1}`)
	if _, ok := FindField(data, "x"); ok {
		t.Error("expected no match for lone high surrogate in key")
	}
}

func TestDecodeFloat64_Overflow(t *testing.T) {
	if _, ok := DecodeFloat64([]byte("1e400")); ok {
		t.Error("expected DecodeFloat64(1e400) to fail on overflow")
	}
}

func TestDecodeString_AllCodepointSizes(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`"\u0041"`, "A"},
		{`"\u00e9"`, "é"},
		{`"\u4e2d"`, "中"},
		{`"\ud83d\ude80"`, "🚀"},
	}
	for _, tt := range cases {
		got, ok := DecodeString([]byte(tt.in))
		if !ok || got != tt.want {
			t.Errorf("DecodeString(%q) = (%q,%v), want (%q,true)", tt.in, got, ok, tt.want)
		}
	}
}

func TestRelease_NilGuard(_ *testing.T)            { Release(nil) }
func TestReleaseBatchWriter_NilGuard(_ *testing.T) { ReleaseBatchWriter(nil) }

func TestCompareCodepoint1_Mismatch(t *testing.T) {
	data := []byte(`{"\u0041":1}`)
	if _, ok := FindField(data, "B"); ok {
		t.Error("expected no match for 1-byte codepoint mismatch")
	}
}

func TestIsStructuralJSON_ValidateArray_MidLoopEOF(t *testing.T) {
	if IsStructuralJSON(`[1 `) {
		t.Error("expected false for array truncated mid-loop")
	}
}

func TestDecodeString_BackslashAtEOF(t *testing.T) {
	raw := []byte{'"', 'a', '\\', '"'}
	if _, ok := DecodeString(raw); ok {
		t.Error("expected DecodeString to reject a body ending in lone backslash")
	}
}

// In-package tests for guards unreachable via the public API.

func TestPoolNewClosure(t *testing.T) {
	v := pool.New()
	b, ok := v.(*Builder)
	if !ok || b == nil {
		t.Fatal("pool.New must return a *Builder")
	}
	if cap(b.buf) < poolBufferSize {
		t.Errorf("pool.New capacity %d, want >= %d", cap(b.buf), poolBufferSize)
	}

	v = batchWriterPool.New()
	bw, ok := v.(*BatchWriter)
	if !ok || bw == nil {
		t.Fatal("batchWriterPool.New must return a *BatchWriter")
	}
	if cap(bw.buf) < batchWriterPoolBufferSize {
		t.Errorf("batchWriterPool.New capacity %d, want >= %d", cap(bw.buf), batchWriterPoolBufferSize)
	}
}

func TestSkipScalar_EmptyGuard(t *testing.T) {
	end, ok := skipScalar(nil, 0)
	if ok || end != 0 {
		t.Errorf("skipScalar(nil,0) = (%d,%v), want (0,false)", end, ok)
	}
}

func TestBytesToString_EmptyGuard(t *testing.T) {
	if s := bytesToString(nil); s != "" {
		t.Errorf("bytesToString(nil) = %q, want empty", s)
	}
	if s := bytesToString([]byte{}); s != "" {
		t.Errorf("bytesToString(empty) = %q, want empty", s)
	}
}

func TestMatchEscape_BackslashAtEOF(t *testing.T) {
	_, _, ok := matchEscape([]byte{'\\'}, 0, "anything", 0)
	if ok {
		t.Error("matchEscape must reject a lone trailing backslash")
	}
}

func TestAcquire_PoolWithBadType(t *testing.T) {
	for i := 0; i < 64; i++ {
		pool.Put(&struct{ x int }{x: i})
	}
	seen := make([]*Builder, 0, 64)
	for i := 0; i < 64; i++ {
		b := Acquire()
		if b == nil {
			t.Fatal("Acquire must not return nil")
		}
		if cap(b.buf) < poolBufferSize {
			t.Errorf("fallback capacity %d, want >= %d", cap(b.buf), poolBufferSize)
		}
		seen = append(seen, b)
	}
	for _, b := range seen {
		Release(b)
	}
}

func TestAcquireBatchWriter_PoolWithBadType(t *testing.T) {
	for i := 0; i < 64; i++ {
		batchWriterPool.Put(&struct{ x int }{x: i})
	}
	seen := make([]*BatchWriter, 0, 64)
	for i := 0; i < 64; i++ {
		bw := AcquireBatchWriter()
		if bw == nil {
			t.Fatal("AcquireBatchWriter must not return nil")
		}
		if cap(bw.buf) < batchWriterPoolBufferSize {
			t.Errorf("fallback capacity %d, want >= %d", cap(bw.buf), batchWriterPoolBufferSize)
		}
		seen = append(seen, bw)
	}
	for _, bw := range seen {
		ReleaseBatchWriter(bw)
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
		return false
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

func TestFindFieldString_Basic(t *testing.T) {
	s := `{"a":1,"target":"found","b":2}`
	val, ok := FindFieldString(s, "target")
	if !ok {
		t.Fatal("expected field to be found")
	}
	if string(val) != `"found"` {
		t.Fatalf("got %q, want %q", val, `"found"`)
	}
}

func TestFindFieldString_Empty(t *testing.T) {
	_, ok := FindFieldString("", "k")
	if ok {
		t.Error("expected false for empty string")
	}
}

func TestFindFieldString_NotFound(t *testing.T) {
	_, ok := FindFieldString(`{"a":1}`, "missing")
	if ok {
		t.Error("expected false for missing key")
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

func TestSkipStringAt_ControlChar(t *testing.T) {
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
	data := []byte(`"` + "aaaaaaaaaaaaaaaaaaa\\nbb" + `"`)
	end, ok := SkipStringAt(data, 0)
	if !ok || end != len(data) {
		t.Errorf("got end=%d ok=%v, want end=%d ok=true", end, ok, len(data))
	}
}

func TestSkipStringAt_LongStringWithControlChar(t *testing.T) {
	buf := make([]byte, 23)
	buf[0] = '"'
	for i := 1; i <= 20; i++ {
		buf[i] = 'a'
	}
	buf[21] = 0x05
	buf[22] = '"'
	_, ok := SkipStringAt(buf, 0)
	if ok {
		t.Error("expected false for control char in long string")
	}
}

func TestSkipStringAt_VeryLongSafe(t *testing.T) {
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
	s := `"12345678"`
	end, ok := SkipStringAt([]byte(s), 0)
	if !ok || end != len(s) {
		t.Errorf("got end=%d ok=%v, want end=%d ok=true", end, ok, len(s))
	}
}

func TestSkipStringAt_QuoteIn2ndSWARWord(t *testing.T) {
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
	b := New(128)
	b.BeginObject()
	ok := FlattenObject(b, []byte(`{"k":{bad}}`))
	if ok {
		t.Error("expected false for malformed inner object")
	}
}

func TestSkipBracedAt_UnterminatedString(t *testing.T) {
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

func TestIterateArray_Strings(t *testing.T) {
	data := []byte(`["alpha","beta","gamma"]`)
	var got []string
	ok := IterateArray(data, func(elem []byte) bool {
		got = append(got, string(elem))
		return true
	})
	if !ok {
		t.Fatal("expected ok=true")
	}
	want := []string{`"alpha"`, `"beta"`, `"gamma"`}
	if len(got) != len(want) {
		t.Fatalf("len=%d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d]=%q, want %q", i, got[i], want[i])
		}
	}
}

func TestIterateArray_MixedTypes(t *testing.T) {
	data := []byte(`[1, "two", true, null, {"k":"v"}, [3]]`)
	var got []string
	ok := IterateArray(data, func(elem []byte) bool {
		got = append(got, string(elem))
		return true
	})
	if !ok {
		t.Fatal("expected ok=true")
	}
	want := []string{"1", `"two"`, "true", "null", `{"k":"v"}`, "[3]"}
	if len(got) != len(want) {
		t.Fatalf("len=%d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d]=%q, want %q", i, got[i], want[i])
		}
	}
}

func TestIterateArray_Empty(t *testing.T) {
	called := false
	ok := IterateArray([]byte(`[]`), func(_ []byte) bool {
		called = true
		return true
	})
	if !ok {
		t.Error("expected ok=true for empty array")
	}
	if called {
		t.Error("fn should not be called for empty array")
	}
}

func TestIterateArray_WhitespaceFormatted(t *testing.T) {
	data := []byte("  [  \"a\" , \"b\"  ]  ")
	var got []string
	ok := IterateArray(data, func(elem []byte) bool {
		got = append(got, string(elem))
		return true
	})
	if !ok {
		t.Fatal("expected ok=true")
	}
	if len(got) != 2 || got[0] != `"a"` || got[1] != `"b"` {
		t.Errorf("got %v, want [\"a\", \"b\"]", got)
	}
}

func TestIterateArray_EarlyStop(t *testing.T) {
	data := []byte(`[1,2,3,4,5]`)
	count := 0
	IterateArray(data, func(_ []byte) bool {
		count++
		return count < 3
	})
	if count != 3 {
		t.Errorf("count=%d, want 3", count)
	}
}

func TestIterateArray_Malformed(t *testing.T) {
	cases := []struct {
		name string
		data string
	}{
		{"not_array", `{"a":1}`},
		{"unterminated", `[1,2`},
		{"missing_comma", `[1 2]`},
		{"empty_string", ``},
		{"just_bracket", `[`},
		{"truncated_value", `["`},
	}
	for _, tt := range cases {
		ok := IterateArray([]byte(tt.data), func(_ []byte) bool { return true })
		if ok {
			t.Errorf("%s: expected false", tt.name)
		}
	}
}

func TestIterateArray_NestedObjects(t *testing.T) {
	data := []byte(`[{"a":1},{"b":2}]`)
	var got []string
	ok := IterateArray(data, func(elem []byte) bool {
		got = append(got, string(elem))
		return true
	})
	if !ok {
		t.Fatal("expected ok=true")
	}
	if len(got) != 2 || got[0] != `{"a":1}` || got[1] != `{"b":2}` {
		t.Errorf("got %v", got)
	}
}

func TestIterateArray_SingleElement(t *testing.T) {
	data := []byte(`[42]`)
	var got []string
	ok := IterateArray(data, func(elem []byte) bool {
		got = append(got, string(elem))
		return true
	})
	if !ok {
		t.Fatal("expected ok=true")
	}
	if len(got) != 1 || got[0] != "42" {
		t.Errorf("got %v, want [42]", got)
	}
}

func TestIterateStringArray_Basic(t *testing.T) {
	data := []byte(`["hello","world"]`)
	var got []string
	ok := IterateStringArray(data, func(val string) bool {
		got = append(got, val)
		return true
	})
	if !ok {
		t.Fatal("expected ok=true")
	}
	if len(got) != 2 || got[0] != "hello" || got[1] != "world" {
		t.Errorf("got %v, want [hello, world]", got)
	}
}

func TestIterateStringArray_Empty(t *testing.T) {
	called := false
	ok := IterateStringArray([]byte(`[]`), func(_ string) bool {
		called = true
		return true
	})
	if !ok {
		t.Error("expected ok=true for empty array")
	}
	if called {
		t.Error("fn should not be called for empty array")
	}
}

func TestIterateStringArray_NonStringElement(t *testing.T) {
	ok := IterateStringArray([]byte(`["a",123,"b"]`), func(_ string) bool {
		return true
	})
	if ok {
		t.Error("expected false for non-string element in array")
	}
}

func TestIterateStringArray_SingleID(t *testing.T) {
	data := []byte(`["1771419690573-2"]`)
	var got []string
	ok := IterateStringArray(data, func(val string) bool {
		got = append(got, val)
		return true
	})
	if !ok {
		t.Fatal("expected ok=true")
	}
	if len(got) != 1 || got[0] != "1771419690573-2" {
		t.Errorf("got %v, want [1771419690573-2]", got)
	}
}

func TestIterateStringArray_EmptyString(t *testing.T) {
	data := []byte(`["",""]`)
	var got []string
	ok := IterateStringArray(data, func(val string) bool {
		got = append(got, val)
		return true
	})
	if !ok {
		t.Fatal("expected ok")
	}
	if len(got) != 2 || got[0] != "" || got[1] != "" {
		t.Errorf("got %v, want [\"\", \"\"]", got)
	}
}

func TestIterateStringArray_ManyIDs(t *testing.T) {
	data := []byte(`["id-1","id-2","id-3","id-4","id-5"]`)
	var got []string
	ok := IterateStringArray(data, func(val string) bool {
		got = append(got, val)
		return true
	})
	if !ok {
		t.Fatal("expected ok=true")
	}
	if len(got) != 5 {
		t.Fatalf("len=%d, want 5", len(got))
	}
	for i, want := range []string{"id-1", "id-2", "id-3", "id-4", "id-5"} {
		if got[i] != want {
			t.Errorf("[%d]=%q, want %q", i, got[i], want)
		}
	}
}

func BenchmarkIterateArray(b *testing.B) {
	data := []byte(`[1,"hello",true,null,{"k":"v"},[1,2,3]]`)
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for b.Loop() {
		IterateArray(data, func(_ []byte) bool { return true })
	}
}

func BenchmarkIterateArray_Strings10(b *testing.B) {
	data := []byte(`["alpha","bravo","charlie","delta","echo","foxtrot","golf","hotel","india","juliet"]`)
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for b.Loop() {
		IterateStringArray(data, func(_ string) bool { return true })
	}
}

func BenchmarkIterateArray_Strings100(b *testing.B) {
	buf := []byte{'['}
	for i := range 100 {
		if i > 0 {
			buf = append(buf, ',')
		}
		buf = append(buf, '"')
		buf = append(buf, []byte("id-"+string(rune('A'+i%26))+"-item")...)
		buf = append(buf, '"')
	}
	buf = append(buf, ']')
	b.ReportAllocs()
	b.SetBytes(int64(len(buf)))
	b.ResetTimer()
	for b.Loop() {
		IterateStringArray(buf, func(_ string) bool { return true })
	}
}

func BenchmarkIterateArray_NestedObjects(b *testing.B) {
	data := []byte(`[{"a":1},{"b":2},{"c":3},{"d":4},{"e":5}]`)
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for b.Loop() {
		IterateArray(data, func(_ []byte) bool { return true })
	}
}

func TestIterateArrayString_Basic(t *testing.T) {
	s := `[1,"hello",true]`
	var got []string
	ok := IterateArrayString(s, func(elem []byte) bool {
		got = append(got, string(elem))
		return true
	})
	if !ok {
		t.Fatal("expected ok")
	}
	want := []string{"1", `"hello"`, "true"}
	if len(got) != len(want) {
		t.Fatalf("len=%d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d]=%q, want %q", i, got[i], want[i])
		}
	}
}

func TestIterateArrayString_Empty(t *testing.T) {
	if IterateArrayString("", func(_ []byte) bool { return true }) {
		t.Fatal("expected false for empty string")
	}
	if !IterateArrayString("[]", func(_ []byte) bool { return true }) {
		t.Fatal("expected ok for empty array")
	}
}

func TestIterateStringArrayString_Basic(t *testing.T) {
	s := `["one","two","three"]`
	var got []string
	ok := IterateStringArrayString(s, func(val string) bool {
		got = append(got, val)
		return true
	})
	if !ok {
		t.Fatal("expected ok")
	}
	want := []string{"one", "two", "three"}
	if len(got) != len(want) {
		t.Fatalf("len=%d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d]=%q, want %q", i, got[i], want[i])
		}
	}
}

func TestIterateStringArrayString_Empty(t *testing.T) {
	if IterateStringArrayString("", func(_ string) bool { return true }) {
		t.Fatal("expected false for empty string")
	}
	if !IterateStringArrayString("[]", func(_ string) bool { return true }) {
		t.Fatal("expected ok for empty array")
	}
}

func TestDecodeString(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{`"hello"`, "hello", true},
		{`""`, "", true},
		{`"a\"b"`, `a"b`, true},
		{`"a\\b"`, `a\b`, true},
		{`"a\/b"`, "a/b", true},
		{`"a\nb"`, "a\nb", true},
		{`"a\tb"`, "a\tb", true},
		{`"a\rb"`, "a\rb", true},
		{`"a\bb"`, "a\bb", true},
		{`"a\fb"`, "a\fb", true},
		{`"\u00e9"`, "é", true},
		{`"\ud83d\ude80"`, "🚀", true},
		{`no quotes`, "", false},
		{`"`, "", false},
		{`"unterminated`, "", false},
		{`"bad\z"`, "", false},
		{`"bad\u"`, "", false},
		{`"bad\uZZZZ"`, "", false},
		{`"\ud83d"`, "", false},
		{`"\udc00"`, "", false},
		{`"\ud83d\u0041"`, "", false},
	}
	for _, tt := range cases {
		got, ok := DecodeString([]byte(tt.in))
		if ok != tt.ok || got != tt.want {
			t.Errorf("DecodeString(%q) = (%q,%v), want (%q,%v)", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

func TestDecodeBool(t *testing.T) {
	cases := []struct {
		in   string
		want bool
		ok   bool
	}{
		{"true", true, true},
		{"false", false, true},
		{"True", false, false},
		{"FALSE", false, false},
		{"tru", false, false},
		{"falsex", false, false},
		{"", false, false},
	}
	for _, tt := range cases {
		got, ok := DecodeBool([]byte(tt.in))
		if ok != tt.ok || got != tt.want {
			t.Errorf("DecodeBool(%q) = (%v,%v), want (%v,%v)", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

func TestDecodeInt64(t *testing.T) {
	cases := []struct {
		in   string
		want int64
		ok   bool
	}{
		{"0", 0, true},
		{"-0", 0, true},
		{"42", 42, true},
		{"-42", -42, true},
		{"9223372036854775807", 9223372036854775807, true},
		{"-9223372036854775808", -9223372036854775808, true},
		{"", 0, false},
		{"+1", 0, false},
		{"01", 0, false},
		{"00", 0, false},
		{"1.5", 0, false},
		{"1e5", 0, false},
		{"-", 0, false},
		{"9223372036854775808", 0, false},
		{"-9223372036854775809", 0, false},
	}
	for _, tt := range cases {
		got, ok := DecodeInt64([]byte(tt.in))
		if ok != tt.ok || got != tt.want {
			t.Errorf("DecodeInt64(%q) = (%d,%v), want (%d,%v)", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

func TestDecodeUint64(t *testing.T) {
	cases := []struct {
		in   string
		want uint64
		ok   bool
	}{
		{"0", 0, true},
		{"42", 42, true},
		{"18446744073709551615", 18446744073709551615, true},
		{"-1", 0, false},
		{"+1", 0, false},
		{"01", 0, false},
		{"1.5", 0, false},
		{"18446744073709551616", 0, false},
		{"", 0, false},
	}
	for _, tt := range cases {
		got, ok := DecodeUint64([]byte(tt.in))
		if ok != tt.ok || got != tt.want {
			t.Errorf("DecodeUint64(%q) = (%d,%v), want (%d,%v)", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

func TestDecodeFloat64(t *testing.T) {
	cases := []struct {
		in   string
		want float64
		ok   bool
	}{
		{"0", 0, true},
		{"3.14", 3.14, true},
		{"-2.5", -2.5, true},
		{"1e10", 1e10, true},
		{"1.5E-3", 1.5e-3, true},
		{"", 0, false},
		{"NaN", 0, false},
		{"Inf", 0, false},
		{"+1", 0, false},
		{".5", 0, false},
		{"1.", 0, false},
		{"1e", 0, false},
	}
	for _, tt := range cases {
		got, ok := DecodeFloat64([]byte(tt.in))
		if ok != tt.ok || got != tt.want {
			t.Errorf("DecodeFloat64(%q) = (%g,%v), want (%g,%v)", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

var decodersSample = []byte(`{"name":"Alice","age":30,"weight":55.5,"active":true,"label":"caf\u00e9"}`)

func findOrFail(t *testing.T, data []byte, key string) []byte {
	t.Helper()
	raw, ok := FindField(data, key)
	if !ok {
		t.Fatalf("FindField(%q): not found", key)
	}
	return raw
}

func TestDecoders_WithFindField_String(t *testing.T) {
	raw := findOrFail(t, decodersSample, "name")
	if s, ok := DecodeString(raw); !ok || s != "Alice" {
		t.Errorf("DecodeString(name) = (%q,%v)", s, ok)
	}
}

func TestDecoders_WithFindField_Int64(t *testing.T) {
	raw := findOrFail(t, decodersSample, "age")
	if v, ok := DecodeInt64(raw); !ok || v != 30 {
		t.Errorf("DecodeInt64(age) = (%d,%v)", v, ok)
	}
}

func TestDecoders_WithFindField_Float64(t *testing.T) {
	raw := findOrFail(t, decodersSample, "weight")
	if v, ok := DecodeFloat64(raw); !ok || v != 55.5 {
		t.Errorf("DecodeFloat64(weight) = (%g,%v)", v, ok)
	}
}

func TestDecoders_WithFindField_Bool(t *testing.T) {
	raw := findOrFail(t, decodersSample, "active")
	if v, ok := DecodeBool(raw); !ok || !v {
		t.Errorf("DecodeBool(active) = (%v,%v)", v, ok)
	}
}

func TestDecoders_WithFindField_StringEscape(t *testing.T) {
	raw := findOrFail(t, decodersSample, "label")
	if s, ok := DecodeString(raw); !ok || s != "café" {
		t.Errorf("DecodeString(label) = (%q,%v)", s, ok)
	}
}
