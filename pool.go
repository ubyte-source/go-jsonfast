package jsonfast

import "sync"

// Sizes of the pooled Builders: the capacity a new one gets, and the largest
// buffer capacity one may have to go back to the pool.
const (
	poolBufferSize = 2 << 10
	poolMaxRetain  = 256 << 10
)

// builderPool holds the Builders that Release returns.
//
//nolint:gochecknoglobals // Acquire's cache; sync.Pool is concurrency-safe
var builderPool sync.Pool

// Acquire returns an empty Builder from the pool, or a new one when the pool
// has none.
func Acquire() *Builder {
	if b, ok := builderPool.Get().(*Builder); ok {
		b.Reset()
		return b
	}
	return New(poolBufferSize)
}

// Release returns b to the pool for a later Acquire; b must not be used
// afterwards. A nil b, or one whose buffer capacity passes 256 KiB, is dropped.
func Release(b *Builder) {
	if b != nil && cap(b.buf) <= poolMaxRetain {
		builderPool.Put(b)
	}
}

// Build returns a copy of what fn, which must not be nil, writes into a pooled
// Builder; the result is never nil, and fn must not keep b.
func Build(fn func(b *Builder)) []byte {
	b := Acquire()
	defer Release(b)
	fn(b)
	out := make([]byte, len(b.buf))
	copy(out, b.buf)
	return out
}
