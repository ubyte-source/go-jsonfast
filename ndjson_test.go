package jsonfast

import (
	"strings"
	"testing"
)

func TestBatchWriter_Append(t *testing.T) {
	bw := NewBatchWriter(256)
	bw.Append([]byte(`{"key":"value"}`))

	result := string(bw.Bytes())
	if !strings.HasSuffix(result, "\n") {
		t.Error("expected trailing newline")
	}
	if result != "{\"key\":\"value\"}\n" {
		t.Errorf("got %q", result)
	}
}

func TestBatchWriter_MultipleLines(t *testing.T) {
	bw := NewBatchWriter(256)
	bw.Append([]byte(`{"line":1}`))
	bw.Append([]byte(`{"line":2}`))
	bw.Append([]byte(`{"line":3}`))

	lines := strings.Split(strings.TrimSuffix(string(bw.Bytes()), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	if lines[0] != `{"line":1}` {
		t.Errorf("line 0: got %q", lines[0])
	}
	if lines[2] != `{"line":3}` {
		t.Errorf("line 2: got %q", lines[2])
	}
}

func TestBatchWriter_Len(t *testing.T) {
	bw := NewBatchWriter(64)
	if bw.Len() != 0 {
		t.Errorf("expected Len()=0, got %d", bw.Len())
	}
	bw.Append([]byte("x"))
	if bw.Len() != 2 {
		t.Errorf("expected Len()=2, got %d", bw.Len())
	}
}

func TestBatchWriter_Reset(t *testing.T) {
	bw := NewBatchWriter(64)
	bw.Append([]byte("test"))
	bw.Reset()
	if bw.Len() != 0 {
		t.Error("expected Len()=0 after Reset")
	}
}

func TestBatchWriter_Grow(t *testing.T) {
	bw := NewBatchWriter(16)
	bw.Grow(1024)
	if cap(bw.buf) < 1024 {
		t.Errorf("expected cap >= 1024, got %d", cap(bw.buf))
	}
}

func TestBatchWriter_DefaultCapacity(t *testing.T) {
	bw := NewBatchWriter(0)
	if bw == nil {
		t.Fatal("NewBatchWriter(0) returned nil")
	}
	bw.Append([]byte(`{"ok":true}`))
	result := string(bw.Bytes())
	if result != `{"ok":true}`+"\n" {
		t.Errorf("got %q", result)
	}
}

func TestBatchWriter_NegativeCapacity(t *testing.T) {
	bw := NewBatchWriter(-10)
	if bw == nil {
		t.Fatal("NewBatchWriter(-10) returned nil")
	}
}

func TestAcquireReleaseBatchWriter(t *testing.T) {
	bw := AcquireBatchWriter()
	if bw == nil {
		t.Fatal("AcquireBatchWriter() returned nil")
	}
	bw.Append([]byte(`{"k":"v"}`))
	if bw.Len() == 0 {
		t.Error("expected non-zero Len after Append")
	}
	ReleaseBatchWriter(bw)
}

func TestReleaseBatchWriter_Nil(_ *testing.T) {
	ReleaseBatchWriter(nil)
}

func TestAcquireBatchWriter_Reuse(t *testing.T) {
	b1 := AcquireBatchWriter()
	b1.Append([]byte("first"))
	ReleaseBatchWriter(b1)

	b2 := AcquireBatchWriter()
	if b2.Len() != 0 {
		t.Errorf("expected Len()=0 after Acquire, got %d", b2.Len())
	}
	b2.Append([]byte("second"))
	expect(t, "second\n", string(b2.Bytes()))
	ReleaseBatchWriter(b2)
}

func TestWarmBatchWriterPool_NonPositive(_ *testing.T) {
	WarmBatchWriterPool(0)
	WarmBatchWriterPool(-1)
}

func TestWarmBatchWriterPool_Primes(t *testing.T) {
	WarmBatchWriterPool(8)
	for range 4 {
		bw := AcquireBatchWriter()
		if bw == nil {
			t.Fatal("AcquireBatchWriter returned nil after WarmBatchWriterPool")
		}
		if bw.Len() != 0 {
			t.Errorf("AcquireBatchWriter returned non-empty (len=%d)", bw.Len())
		}
		ReleaseBatchWriter(bw)
	}
}

func BenchmarkBatchWriter_Append(b *testing.B) {
	bw := NewBatchWriter(4096)
	line := []byte(`{"timestamp":"2024-01-15T12:30:45Z","message":"test syslog message","severity":4}`)

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		bw.Reset()
		for range 100 {
			bw.Append(line)
		}
	}
}

func BenchmarkBatchWriter_AcquireRelease(b *testing.B) {
	line := []byte(`{"msg":"test"}`)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		bw := AcquireBatchWriter()
		bw.Append(line)
		ReleaseBatchWriter(bw)
	}
}

func TestBatchWriter_AppendString(t *testing.T) {
	bw := NewBatchWriter(256)
	bw.AppendString(`{"msg":"hello"}`)
	bw.AppendString(`{"msg":"world"}`)

	got := string(bw.Bytes())
	want := "{\"msg\":\"hello\"}\n{\"msg\":\"world\"}\n"
	if got != want {
		t.Errorf("AppendString: got %q, want %q", got, want)
	}
}

func TestBatchWriter_Count(t *testing.T) {
	bw := NewBatchWriter(256)
	if bw.Count() != 0 {
		t.Fatalf("expected 0, got %d", bw.Count())
	}
	bw.Append([]byte(`{"a":1}`))
	bw.AppendString(`{"b":2}`)
	if bw.Count() != 2 {
		t.Errorf("expected 2, got %d", bw.Count())
	}
	bw.Reset()
	if bw.Count() != 0 {
		t.Errorf("expected 0 after reset, got %d", bw.Count())
	}
}
