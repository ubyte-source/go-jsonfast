package jsonfast

import (
	"io"
	"slices"
	"sync"
)

// BatchWriter accumulates newline-delimited JSON records. Not safe for
// concurrent use.
type BatchWriter struct {
	buf   []byte
	count int
}

// NewBatchWriter returns a BatchWriter with the given initial capacity.
// Non-positive capacities are clamped to 4096.
func NewBatchWriter(capacity int) *BatchWriter {
	if capacity <= 0 {
		capacity = 4096
	}
	return &BatchWriter{buf: make([]byte, 0, capacity)}
}

// Grow ensures at least n bytes of spare capacity.
func (w *BatchWriter) Grow(n int) {
	if cap(w.buf)-len(w.buf) < n {
		w.buf = slices.Grow(w.buf, n)
	}
}

// Append writes record followed by '\n'.
func (w *BatchWriter) Append(record []byte) {
	w.Grow(len(record) + 1)
	w.buf = append(w.buf, record...)
	w.buf = append(w.buf, '\n')
	w.count++
}

// AppendString writes record followed by '\n'.
func (w *BatchWriter) AppendString(record string) {
	w.Grow(len(record) + 1)
	w.buf = append(w.buf, record...)
	w.buf = append(w.buf, '\n')
	w.count++
}

// Write implements io.Writer by appending p as one NDJSON record.
func (w *BatchWriter) Write(p []byte) (int, error) {
	w.Append(p)
	return len(p), nil
}

// WriteTo implements io.WriterTo. The batch state is unchanged.
func (w *BatchWriter) WriteTo(target io.Writer) (int64, error) {
	n, err := target.Write(w.buf)
	return int64(n), err
}

// Bytes returns the accumulated payload. The slice aliases the internal buffer.
func (w *BatchWriter) Bytes() []byte { return w.buf }

// Len returns the current byte length.
func (w *BatchWriter) Len() int { return len(w.buf) }

// Count returns the number of records in the batch.
func (w *BatchWriter) Count() int { return w.count }

// Reset clears the batch contents while retaining the backing array.
func (w *BatchWriter) Reset() {
	w.buf = w.buf[:0]
	w.count = 0
}

const (
	batchWriterPoolBufferSize = 8192
	batchWriterPoolMaxRetain  = 1 << 22 // 4 MB
)

var batchWriterPool = sync.Pool{
	New: func() any {
		return &BatchWriter{buf: make([]byte, 0, batchWriterPoolBufferSize)}
	},
}

// AcquireBatchWriter returns a BatchWriter from the pool.
func AcquireBatchWriter() *BatchWriter {
	bw, ok := batchWriterPool.Get().(*BatchWriter)
	if !ok {
		return NewBatchWriter(batchWriterPoolBufferSize)
	}
	bw.Reset()
	return bw
}

// ReleaseBatchWriter returns bw to the pool. Buffers larger than 4 MB
// are discarded.
func ReleaseBatchWriter(bw *BatchWriter) {
	if bw == nil {
		return
	}
	if cap(bw.buf) <= batchWriterPoolMaxRetain {
		batchWriterPool.Put(bw)
	}
}

// WarmBatchWriterPool pre-allocates n BatchWriters and returns them to the pool.
func WarmBatchWriterPool(n int) {
	if n <= 0 {
		return
	}
	writers := make([]*BatchWriter, n)
	for i := range writers {
		writers[i] = &BatchWriter{buf: make([]byte, 0, batchWriterPoolBufferSize)}
	}
	for _, bw := range writers {
		batchWriterPool.Put(bw)
	}
}
