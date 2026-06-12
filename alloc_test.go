package jsonfast

import (
	"testing"
	"time"
)

// Zero-allocation CI gate: each test asserts that a hot path produces
// zero heap allocations per call.

const allocIterations = 100

func assertZeroAlloc(t *testing.T, name string, f func()) {
	t.Helper()
	for range 4 {
		f()
	}
	n := testing.AllocsPerRun(allocIterations, f)
	if n != 0 {
		t.Errorf("%s: expected 0 allocs/op, got %.2f", name, n)
	}
}

func TestZeroAlloc_Builder_FullSyslog(t *testing.T) {
	builder := New(512)
	ts := time.Date(2024, 1, 15, 12, 30, 45, 123456789, time.UTC)
	assertZeroAlloc(t, "FullSyslog", func() {
		builder.Reset()
		builder.BeginObject()
		builder.AddStringField("message", "User authentication failed")
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
	})
}

func TestZeroAlloc_Builder_FullSyslog_FieldKey(t *testing.T) {
	builder := New(512)
	ts := time.Date(2024, 1, 15, 12, 30, 45, 123456789, time.UTC)
	keyMessage := NewFieldKey("message")
	keyTimestamp := NewFieldKey("timestamp")
	keyHostname := NewFieldKey("hostname")
	keySeverity := NewFieldKey("severity")
	assertZeroAlloc(t, "FullSyslog_FieldKey", func() {
		builder.Reset()
		builder.BeginObject()
		builder.AddStringFieldKey(keyMessage, "User authentication failed")
		builder.AddTimeRFC3339FieldKey(keyTimestamp, ts)
		builder.AddStringFieldKey(keyHostname, "webserver-prod-01")
		builder.AddIntFieldKey(keySeverity, 4)
		builder.EndObject()
		_ = builder.Bytes()
	})
}

func TestZeroAlloc_StringArrayField(t *testing.T) {
	builder := New(512)
	key := NewFieldKey("ids")
	vals := []string{"id-1", "id-2", "id-3", "id-4", "id-5"}
	assertZeroAlloc(t, "StringArrayField", func() {
		builder.Reset()
		builder.BeginObject()
		builder.AddStringArrayField("items", vals)
		builder.AddStringArrayFieldKey(key, vals)
		builder.EndObject()
	})
}

func TestZeroAlloc_NestedObjectField(t *testing.T) {
	builder := New(512)
	key := NewFieldKey("sd")
	assertZeroAlloc(t, "NestedObjectField", func() {
		builder.Reset()
		builder.BeginObject()
		builder.BeginObjectFieldKey(key)
		builder.AddStringField("a", "1")
		builder.AddStringField("b", "2")
		builder.EndObjectField()
		builder.BeginObjectField("inner")
		builder.AddIntField("x", 42)
		builder.EndObjectField()
		builder.EndObject()
	})
}

// TestZeroAlloc_AllScalarFields covers every remaining scalar Add*
// method (string-keyed and FieldKey variants) in one pass.
func TestZeroAlloc_AllScalarFields(t *testing.T) {
	builder := New(1024)
	ts := time.Date(2024, 1, 15, 12, 30, 45, 123456789, time.FixedZone("CET", 3600))
	raw := []byte(`{"nested":true}`)
	rawName := []byte("rawname")
	kBool := NewFieldKey("kbool")
	kNull := NewFieldKey("knull")
	kI64 := NewFieldKey("ki64")
	kU64 := NewFieldKey("ku64")
	kF64 := NewFieldKey("kf64")
	kRaw := NewFieldKey("kraw")
	kOff := NewFieldKey("koff")
	assertZeroAlloc(t, "AllScalarFields", func() {
		builder.Reset()
		builder.BeginObject()
		builder.AddBoolField("b1", true)
		builder.AddBoolField("b2", false)
		builder.AddBoolFieldKey(kBool, true)
		builder.AddNullField("n1")
		builder.AddNullFieldKey(kNull)
		builder.AddInt64Field("i1", -9007199254740993)
		builder.AddInt64FieldKey(kI64, 1<<62)
		builder.AddUint64Field("u1", 18446744073709551615)
		builder.AddUint64FieldKey(kU64, 0)
		builder.AddFloat64FieldKey(kF64, 2.718281828459045)
		builder.AddRawJSONField("r1", raw)
		builder.AddRawJSONFieldKey(kRaw, raw)
		builder.AddRawBytesField(rawName, raw)
		builder.AddTimeRFC3339OffsetField("t1", ts)
		builder.AddTimeRFC3339OffsetFieldKey(kOff, ts)
		builder.EndObject()
	})
}

func TestZeroAlloc_StringMapObject(t *testing.T) {
	builder := New(1024)
	m := map[string]string{"alpha": "1", "beta": "2", "gamma": `{"j":1}`}
	assertZeroAlloc(t, "StringMapObject", func() {
		builder.Reset()
		builder.AddStringMapObject(m, "gamma")
	})
	assertZeroAlloc(t, "StringMapObjectField", func() {
		builder.Reset()
		builder.BeginObject()
		builder.AddStringMapObjectField("attrs", m, "")
		builder.EndObject()
	})
}

func TestZeroAlloc_AcquireRelease(t *testing.T) {
	WarmPool(4)
	assertZeroAlloc(t, "AcquireRelease", func() {
		b := Acquire()
		b.BeginObject()
		b.AddStringField("msg", "test")
		b.EndObject()
		_ = b.Bytes()
		Release(b)
	})
}

func TestZeroAlloc_NestedStringMapField_Small(t *testing.T) {
	builder := New(1024)
	sd := testSDFixture()
	assertZeroAlloc(t, "NestedStringMapField", func() {
		builder.Reset()
		builder.BeginObject()
		builder.AddNestedStringMapField("sd", sd)
		builder.EndObject()
	})
}

func TestZeroAlloc_FlattenedMapField_Small(t *testing.T) {
	builder := New(1024)
	m := testSDOnlyFixture()
	assertZeroAlloc(t, "AddFlattenedMapField", func() {
		builder.Reset()
		builder.BeginObject()
		builder.AddFlattenedMapField(m)
		builder.EndObject()
	})
}

func TestZeroAlloc_Scan_IterateFields(t *testing.T) {
	data := []byte(`{"facility":23,"severity":3,"hostname":"FW01","app_name":"utm"}`)
	assertZeroAlloc(t, "IterateFields", func() {
		IterateFields(data, func(_, _ []byte) bool { return true })
	})
}

func TestZeroAlloc_Scan_FindField(t *testing.T) {
	data := []byte(`{"facility":23,"severity":3,"hostname":"FW01","app_name":"utm"}`)
	assertZeroAlloc(t, "FindField", func() {
		_, _ = FindField(data, "app_name")
	})
}

func TestZeroAlloc_Scan_IterateArray(t *testing.T) {
	data := []byte(`["alpha","bravo","charlie","delta","echo"]`)
	assertZeroAlloc(t, "IterateArray", func() {
		IterateArray(data, func(_ []byte) bool { return true })
	})
}

func TestZeroAlloc_Scan_IterateStringArray(t *testing.T) {
	data := []byte(`["alpha","bravo","charlie","delta","echo"]`)
	assertZeroAlloc(t, "IterateStringArray", func() {
		IterateStringArray(data, func(_ string) bool { return true })
	})
}

func TestZeroAlloc_Scan_StringVariants(t *testing.T) {
	obj := `{"facility":23,"hostname":"FW01"}`
	arr := `["alpha","bravo","charlie"]`
	assertZeroAlloc(t, "IterateFieldsString", func() {
		IterateFieldsString(obj, func(_, _ []byte) bool { return true })
	})
	assertZeroAlloc(t, "FindFieldString", func() {
		_, _ = FindFieldString(obj, "hostname")
	})
	assertZeroAlloc(t, "IterateArrayString", func() {
		IterateArrayString(arr, func(_ []byte) bool { return true })
	})
	assertZeroAlloc(t, "IterateStringArrayString", func() {
		IterateStringArrayString(arr, func(_ string) bool { return true })
	})
}

func TestZeroAlloc_Scan_SkipPrimitives(t *testing.T) {
	doc := []byte(`  {"k":[1,2,{"x":"y \" z"}],"n":-12.5e3}  `)
	str := []byte(`"escaped \" and long tail abcdefghijklmnopqrstuvwxyz"`)
	assertZeroAlloc(t, "SkipWS+SkipValueAt", func() {
		i := SkipWS(doc, 0)
		_, _ = SkipValueAt(doc, i)
	})
	assertZeroAlloc(t, "SkipStringAt", func() {
		_, _ = SkipStringAt(str, 0)
	})
	assertZeroAlloc(t, "SkipBracedAt", func() {
		_, _ = SkipBracedAt(doc, 2, '{', '}')
	})
}

func TestZeroAlloc_Decoders(t *testing.T) {
	rawBool := []byte(`true`)
	rawInt := []byte(`-9223372036854775808`)
	rawUint := []byte(`18446744073709551615`)
	rawFloat := []byte(`-12.5e3`)
	assertZeroAlloc(t, "Decode scalars", func() {
		_, _ = DecodeBool(rawBool)
		_, _ = DecodeInt64(rawInt)
		_, _ = DecodeUint64(rawUint)
		_, _ = DecodeFloat64(rawFloat)
	})
}

func TestZeroAlloc_Scan_FlattenObject(t *testing.T) {
	sd := []byte(`{"KV@32473":{"action":"pass","srcip":"1.2.3.4","dstip":"5.6.7.8"}}`)
	builder := New(512)
	assertZeroAlloc(t, "FlattenObject", func() {
		builder.Reset()
		builder.BeginObject()
		FlattenObject(builder, sd)
		builder.EndObject()
	})
}

func TestZeroAlloc_Scan_IsStructuralJSON(t *testing.T) {
	s := `{"a":1,"b":{"c":2,"d":[1,2,3]},"e":"hello"}`
	assertZeroAlloc(t, "IsStructuralJSON", func() {
		_ = IsStructuralJSON(s)
	})
}

func TestZeroAlloc_EscapeString_NoEscape(t *testing.T) {
	s := "hello-world-no-special-chars-here"
	assertZeroAlloc(t, "EscapeString/pure-ASCII", func() {
		_ = EscapeString(s)
	})
}

func TestZeroAlloc_BatchWriter_Append(t *testing.T) {
	bw := NewBatchWriter(4096)
	line := []byte(`{"timestamp":"2024-01-15T12:30:45Z","message":"test","severity":4}`)
	assertZeroAlloc(t, "BatchWriter.Append", func() {
		bw.Reset()
		for range 16 {
			bw.Append(line)
		}
	})
}

func TestZeroAlloc_BatchWriter_AcquireRelease(t *testing.T) {
	WarmBatchWriterPool(4)
	line := []byte(`{"msg":"test"}`)
	assertZeroAlloc(t, "BatchWriter_AcquireRelease", func() {
		bw := AcquireBatchWriter()
		bw.Append(line)
		ReleaseBatchWriter(bw)
	})
}

func TestZeroAlloc_Builder_Write(t *testing.T) {
	builder := New(256)
	payload := []byte("hello world")
	assertZeroAlloc(t, "Builder.Write", func() {
		builder.Reset()
		n, err := builder.Write(payload)
		if err != nil || n != len(payload) {
			panic("Write contract violated")
		}
	})
}

func TestZeroAlloc_Float64_NaN_Inf(t *testing.T) {
	builder := New(64)
	assertZeroAlloc(t, "Float64.NaN_Inf", func() {
		builder.Reset()
		builder.BeginObject()
		builder.AddFloat64Field("nan", nanValue)
		builder.AddFloat64Field("posinf", posInfValue)
		builder.AddFloat64Field("neginf", negInfValue)
		builder.EndObject()
	})
}
