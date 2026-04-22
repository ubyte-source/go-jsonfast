package jsonfast

// io.Writer / io.WriterTo contract for Builder and BatchWriter.

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestBuilder_ImplementsIOWriter(_ *testing.T) {
	var _ io.Writer = (*Builder)(nil)
	var _ io.WriterTo = (*Builder)(nil)
}

func TestBuilder_Write(t *testing.T) {
	b := New(64)
	n, err := b.Write([]byte("hello "))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 6 {
		t.Fatalf("expected 6 bytes, got %d", n)
	}
	if _, err := b.Write([]byte("world")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expect(t, "hello world", string(b.Bytes()))
}

func TestBuilder_Write_Empty(t *testing.T) {
	b := New(16)
	n, err := b.Write(nil)
	if err != nil || n != 0 {
		t.Fatalf("expected (0,nil), got (%d,%v)", n, err)
	}
}

func TestBuilder_WriteTo(t *testing.T) {
	b := New(64)
	b.BeginObject()
	b.AddStringField("k", "v")
	b.EndObject()

	var sink bytes.Buffer
	n, err := b.WriteTo(&sink)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if int(n) != b.Len() {
		t.Fatalf("expected WriteTo to report %d bytes, got %d", b.Len(), n)
	}
	if got := sink.String(); got != `{"k":"v"}` {
		t.Fatalf("sink content %q", got)
	}
	if string(b.Bytes()) != `{"k":"v"}` {
		t.Fatal("WriteTo must not mutate Builder state")
	}
}

func TestBatchWriter_ImplementsIOWriter(_ *testing.T) {
	var _ io.Writer = (*BatchWriter)(nil)
	var _ io.WriterTo = (*BatchWriter)(nil)
}

func TestBatchWriter_Write(t *testing.T) {
	bw := NewBatchWriter(128)
	n, err := bw.Write([]byte(`{"line":1}`))
	if err != nil || n != len(`{"line":1}`) {
		t.Fatalf("Write returned (%d,%v)", n, err)
	}
	if _, err := bw.Write([]byte(`{"line":2}`)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "{\"line\":1}\n{\"line\":2}\n"
	if got := string(bw.Bytes()); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if bw.Count() != 2 {
		t.Fatalf("expected Count=2, got %d", bw.Count())
	}
}

func TestBatchWriter_WriteTo(t *testing.T) {
	bw := NewBatchWriter(64)
	bw.AppendString(`{"a":1}`)
	bw.AppendString(`{"b":2}`)

	var sink strings.Builder
	n, err := bw.WriteTo(&sink)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if int(n) != bw.Len() {
		t.Fatalf("expected WriteTo to report %d bytes, got %d", bw.Len(), n)
	}
	want := "{\"a\":1}\n{\"b\":2}\n"
	if got := sink.String(); got != want {
		t.Fatalf("sink content %q, want %q", got, want)
	}
}
