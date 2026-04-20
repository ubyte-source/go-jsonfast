package jsonfast

import (
	"testing"
	"time"
)

// Zero-allocation CI gate: each test asserts a documented hot path
// produces zero heap allocations per call via testing.AllocsPerRun.

const allocIterations = 100

// assertZeroAlloc fails the test if f allocates on the steady-state path.
// The warm-up primes pool state so growth is not counted.
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

func TestZeroAlloc_AcquireRelease(t *testing.T) {
	// Warm the pool so New is not called inside AcquireRelease.
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
	assertZeroAlloc(t, "NestedStringMapField", func() {
		builder.Reset()
		builder.BeginObject()
		builder.AddNestedStringMapField("sd", sd)
		builder.EndObject()
	})
}

func TestZeroAlloc_FlattenedMapField_Small(t *testing.T) {
	builder := New(1024)
	m := map[string]map[string]string{
		"exampleSDID@32473": {
			"iut":         "3",
			"eventSource": "Application",
			"eventID":     "1011",
		},
	}
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
		out := EscapeString(s)
		_ = out
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
	// Ensure the NaN/Inf→null path is allocation-free.
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
