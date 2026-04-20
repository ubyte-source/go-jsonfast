// Package jsonfast provides a zero-allocation JSON builder for fixed schemas.
//
// Designed for ultra-high-throughput pipelines where every byte and every
// nanosecond matters. All methods operate on a reusable byte buffer;
// steady-state operation requires zero heap allocations on the documented
// hot paths: Builder, BatchWriter, every Add*Field method, string escape,
// the scan package (SkipValueAt, SkipStringAt, SkipBracedAt, IterateFields,
// IterateArray, IterateStringArray, FindField, FlattenObject,
// IsStructuralJSON), and AddNestedStringMapField and AddFlattenedMapField
// within their documented stack-buffer bounds.
//
// # Usage
//
//	b := jsonfast.Acquire()
//	b.BeginObject()
//	b.AddStringField("message", "hello world")
//	b.AddIntField("severity", 3)
//	b.AddTimeRFC3339Field("timestamp", time.Now())
//	b.EndObject()
//	data := b.Bytes() // no allocation
//	jsonfast.Release(b) // return to pool
//
// # Pre-computed field keys
//
// For static field names known at init time, use FieldKey to eliminate
// the per-call quoting overhead:
//
//	var keyMessage = jsonfast.NewFieldKey("message")
//	b.AddStringFieldKey(keyMessage, "hello world")
//
// # Field name requirements
//
// Field names passed to Add*Field (string-keyed variants) and to
// NewFieldKey must be safe ASCII: bytes in the printable ASCII range
// (0x20–0x7E) excluding '"' and '\\'. They are copied verbatim into the
// output. For dynamic or untrusted names, escape them via EscapeString
// before use, or compose the output with AppendEscapedString /
// AppendRawString directly.
//
// # Scanning and parsing
//
// IterateFields, FindField, IterateArray and related functions parse
// JSON structurally without building an intermediate DOM. All accept
// []byte or string inputs (the *String variants use unsafe.Slice to
// avoid the conversion allocation). IterateStringArray and its String
// variant pass a zero-allocation string view to the callback; the view
// is only valid for the duration of the callback. Clone via
// strings.Clone to retain.
//
// # NDJSON batching
//
// BatchWriter appends complete JSON records separated by newlines.
// It implements io.Writer (each Write adds one record) and io.WriterTo
// (payload flush). AcquireBatchWriter / ReleaseBatchWriter manage a
// sync.Pool; WarmBatchWriterPool pre-seeds the pool.
package jsonfast
