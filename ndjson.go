package jsonfast

import (
	"slices"
	"sync"
)

// BatchWriter accumulates newline-delimited JSON records; its zero value is ready to
// use and not safe for concurrent use.
type BatchWriter struct {
	buf   []byte
	count int
}

// defaultBatchCapacity is the capacity NewBatchWriter gives a BatchWriter when
// asked for none.
const defaultBatchCapacity = 4 << 10

// NewBatchWriter returns a BatchWriter with the given initial capacity; a capacity
// below 1 gives 4 KiB, and it panics if capacity is past what a byte slice can hold.
func NewBatchWriter(capacity int) *BatchWriter {
	if capacity <= 0 {
		capacity = defaultBatchCapacity
	}
	return &BatchWriter{buf: make([]byte, 0, capacity)}
}

// Reset empties the batch and keeps its buffer.
func (w *BatchWriter) Reset() {
	w.buf = w.buf[:0]
	w.count = 0
}

// Bytes returns the batch. The slice aliases the buffer, so it is valid until
// the next write, Reset or ReleaseBatchWriter.
func (w *BatchWriter) Bytes() []byte {
	return w.buf
}

// Len returns the length of the batch in bytes.
func (w *BatchWriter) Len() int {
	return len(w.buf)
}

// Count returns the number of records in the batch.
func (w *BatchWriter) Count() int {
	return w.count
}

// Grow makes room for n more bytes, so they need no further allocation; it panics,
// as slices.Grow does, if n is negative or the result is past what a byte slice can
// hold.
func (w *BatchWriter) Grow(n int) {
	w.buf = slices.Grow(w.buf, n)
}

// Append writes record and a '\n' with at most one growth of the batch. The
// caller vouches that record is one JSON text without a newline.
func (w *BatchWriter) Append(record []byte) {
	w.Grow(len(record) + 1)
	w.buf = append(w.buf, record...)
	w.buf = append(w.buf, '\n')
	w.count++
}

// AppendString is Append for a record held in a string.
func (w *BatchWriter) AppendString(record string) {
	w.Append(bytesOf(record))
}

// Sizes of the pooled BatchWriters: the capacity a new one gets, and the
// largest buffer capacity one may have to go back to the pool.
const (
	batchPoolBufferSize = 8 << 10
	batchPoolMaxRetain  = 4 << 20
)

// batchPool holds the BatchWriters that ReleaseBatchWriter returns.
//
//nolint:gochecknoglobals // AcquireBatchWriter's cache; sync.Pool is concurrency-safe
var batchPool sync.Pool

// AcquireBatchWriter returns an empty BatchWriter from the pool, or a new one
// when the pool has none.
func AcquireBatchWriter() *BatchWriter {
	if w, ok := batchPool.Get().(*BatchWriter); ok {
		w.Reset()
		return w
	}
	return NewBatchWriter(batchPoolBufferSize)
}

// ReleaseBatchWriter returns w to the pool for a later AcquireBatchWriter; w
// must not be used afterwards. A nil w, or one whose buffer capacity passes
// 4 MiB, is dropped.
func ReleaseBatchWriter(w *BatchWriter) {
	if w != nil && cap(w.buf) <= batchPoolMaxRetain {
		batchPool.Put(w)
	}
}
