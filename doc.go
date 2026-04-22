// Package jsonfast is a zero-allocation JSON builder and scanner for
// fixed schemas.
//
// # Usage
//
//	b := jsonfast.Acquire()
//	b.BeginObject()
//	b.AddStringField("message", "hello")
//	b.AddIntField("severity", 3)
//	b.AddTimeRFC3339Field("timestamp", time.Now())
//	b.EndObject()
//	data := b.Bytes()
//	jsonfast.Release(b)
//
// # Pre-computed keys
//
//	var keyMessage = jsonfast.NewFieldKey("message")
//	b.AddStringFieldKey(keyMessage, "hello")
//
// Field names passed to Add*Field are JSON-escaped on output, so any Go
// string is safe. NewFieldKey caches the prefix verbatim and therefore
// requires a safe-ASCII name (printable ASCII excluding '"' and '\\').
//
// # Scanning
//
// IterateFields, FindField, IterateArray, IterateStringArray parse JSON
// structurally without building a DOM. The *String variants take a
// string input and aliase its backing memory; slices/strings passed to
// callbacks must not outlive the call (clone via strings.Clone to retain).
//
// # NDJSON
//
// BatchWriter appends JSON records separated by '\n' and implements
// io.Writer (one record per Write) and io.WriterTo.
package jsonfast
