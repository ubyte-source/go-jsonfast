package jsonfast

import (
	"io"
	"slices"
	"sync"
)

// BatchWriter accumulates newline-delimited JSON (NDJSON) records into
// a reusable buffer. Each record is a complete JSON object followed by
// '\n'. A BatchWriter is not safe for concurrent use.
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
//	payload := bw.Bytes()
//	bw.Reset()
type BatchWriter struct {
	buf   []byte
	count int
}

// NewBatchWriter creates a BatchWriter with the given initial capacity.
// A non-positive capacity is clamped to 4096 bytes.
func NewBatchWriter(capacity int) *BatchWriter {
	if capacity <= 0 {
		capacity = 4096
	}
	return &BatchWriter{
		buf: make([]byte, 0, capacity),
	}
}

// Grow ensures the buffer has at least n bytes of spare capacity. Uses
// slices.Grow which leverages the runtime's optimized growslice.
func (w *BatchWriter) Grow(n int) {
	if cap(w.buf)-len(w.buf) < n {
		w.buf = slices.Grow(w.buf, n)
	}
}

// Append adds a complete JSON record followed by '\n' to the batch.
func (w *BatchWriter) Append(record []byte) {
	w.Grow(len(record) + 1)
	w.buf = append(w.buf, record...)
	w.buf = append(w.buf, '\n')
	w.count++
}

// AppendString adds a complete JSON record (as string) followed by '\n'
// to the batch.
func (w *BatchWriter) AppendString(record string) {
	w.Grow(len(record) + 1)
	w.buf = append(w.buf, record...)
	w.buf = append(w.buf, '\n')
	w.count++
}

// Write implements io.Writer by appending p as a single NDJSON record
// (a newline is added). Write never returns an error.
func (w *BatchWriter) Write(p []byte) (int, error) {
	w.Append(p)
	return len(p), nil
}

// WriteTo implements io.WriterTo. The accumulated NDJSON payload is
// written to target in a single call. The batch state is unchanged.
func (w *BatchWriter) WriteTo(target io.Writer) (int64, error) {
	n, err := target.Write(w.buf)
	return int64(n), err
}

// Bytes returns the accumulated NDJSON payload. The returned slice
// aliases the internal buffer; do not modify it.
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

// batchWriterPoolBufferSize is the default pool entry capacity.
const batchWriterPoolBufferSize = 8192

// batchWriterPoolMaxRetain is the largest buffer capacity returned to
// the pool; larger buffers are discarded to bound memory retention.
const batchWriterPoolMaxRetain = 1 << 22 // 4 MB

// batchWriterPool is a sync.Pool for reusing BatchWriter instances.
// For pipelines producing batches larger than the pool default capacity,
// construct BatchWriters directly with NewBatchWriter.
var batchWriterPool = sync.Pool{
	New: func() any {
		return &BatchWriter{
			buf: make([]byte, 0, batchWriterPoolBufferSize),
		}
	},
}

// AcquireBatchWriter returns a BatchWriter from the pool, ready for use.
// Call ReleaseBatchWriter when done to return it for reuse.
func AcquireBatchWriter() *BatchWriter {
	bw, ok := batchWriterPool.Get().(*BatchWriter)
	if !ok {
		return NewBatchWriter(batchWriterPoolBufferSize)
	}
	bw.Reset()
	return bw
}

// ReleaseBatchWriter returns the BatchWriter to the pool for reuse. The
// writer must not be used after calling ReleaseBatchWriter. Buffers
// larger than 4 MB are discarded.
func ReleaseBatchWriter(bw *BatchWriter) {
	if bw == nil {
		return
	}
	if cap(bw.buf) <= batchWriterPoolMaxRetain {
		batchWriterPool.Put(bw)
	}
}

// WarmBatchWriterPool pre-allocates n BatchWriter instances and returns
// them to the pool. Use before entering the hot path to smooth tail
// latency during warm-up. Values of n ≤ 0 are a no-op.
func WarmBatchWriterPool(n int) {
	if n <= 0 {
		return
	}
	writers := make([]*BatchWriter, n)
	for i := range writers {
		writers[i] = &BatchWriter{
			buf: make([]byte, 0, batchWriterPoolBufferSize),
		}
	}
	for _, bw := range writers {
		batchWriterPool.Put(bw)
	}
}
