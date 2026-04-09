package jsonfast

import (
	"slices"
	"sync"
)

// BatchWriter accumulates newline-delimited JSON (NDJSON) records into a
// reusable buffer. Each record is a complete JSON object followed by '\n'.
//
// Designed for high-throughput pipelines that batch multiple log records
// before sending to a message broker (Redis Streams, Kafka, NATS).
//
// Usage:
//
//	bw := jsonfast.NewBatchWriter(4096)
//	b := jsonfast.Acquire()
//	b.BeginObject()
//	b.AddStringField("msg", "hello")
//	b.EndObject()
//	bw.Append(b.Bytes())
//	jsonfast.Release(b)
//	payload := bw.Bytes()  // "{"msg":"hello"}\n"
//	bw.Reset()
type BatchWriter struct {
	buf   []byte
	count int
}

// NewBatchWriter creates a new BatchWriter with the given initial capacity.
func NewBatchWriter(capacity int) *BatchWriter {
	if capacity <= 0 {
		capacity = 4096
	}
	return &BatchWriter{
		buf: make([]byte, 0, capacity),
	}
}

// Grow ensures the buffer has at least n bytes of spare capacity.
// Uses slices.Grow which leverages the runtime's optimized growslice.
func (w *BatchWriter) Grow(n int) {
	if cap(w.buf)-len(w.buf) < n {
		w.buf = slices.Grow(w.buf, n)
	}
}

// Append adds a complete JSON record followed by a newline to the batch.
func (w *BatchWriter) Append(record []byte) {
	w.buf = append(w.buf, record...)
	w.buf = append(w.buf, '\n')
	w.count++
}

// AppendString adds a complete JSON record (as string) followed by a newline.
func (w *BatchWriter) AppendString(record string) {
	w.buf = append(w.buf, record...)
	w.buf = append(w.buf, '\n')
	w.count++
}

// Bytes returns the accumulated NDJSON payload.
func (w *BatchWriter) Bytes() []byte {
	return w.buf
}

// Len returns the current byte length of the batch.
func (w *BatchWriter) Len() int {
	return len(w.buf)
}

// Count returns the number of records in the batch.
func (w *BatchWriter) Count() int {
	return w.count
}

// Reset clears the batch for reuse without releasing memory.
func (w *BatchWriter) Reset() {
	w.buf = w.buf[:0]
	w.count = 0
}

// batchWriterPool is a sync.Pool for reusing BatchWriter instances.
// Default capacity: 8 KB. Discard threshold: 4 MB (1<<22).
// For 100 Gbps pipelines, callers should use NewBatchWriter with larger
// initial capacity rather than relying on pool defaults.
var batchWriterPool = sync.Pool{
	New: func() any {
		return &BatchWriter{
			buf: make([]byte, 0, 8192),
		}
	},
}

// AcquireBatchWriter returns a BatchWriter from the pool, ready for use.
// Call ReleaseBatchWriter when done to return it for reuse.
func AcquireBatchWriter() *BatchWriter {
	bw, ok := batchWriterPool.Get().(*BatchWriter)
	if !ok {
		bw = NewBatchWriter(8192)
	}
	bw.Reset()
	return bw
}

// ReleaseBatchWriter returns the BatchWriter to the pool for reuse.
// The writer must not be used after calling ReleaseBatchWriter.
func ReleaseBatchWriter(bw *BatchWriter) {
	if bw == nil {
		return
	}
	if cap(bw.buf) <= 1<<22 {
		batchWriterPool.Put(bw)
	}
}
