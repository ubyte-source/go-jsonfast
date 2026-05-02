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
// string input and alias its backing memory; slices/strings passed to
// callbacks must not outlive the call (clone via strings.Clone to retain).
//
// Raw JSON value bytes returned by the scanners can be promoted to Go
// values via DecodeString, DecodeBool, DecodeInt64, DecodeUint64, and
// DecodeFloat64. Each returns ok=false on malformed input; the integer
// decoders also reject fractional, exponent, leading '+', and
// leading-zero forms.
//
// # Out-of-scope
//
// jsonfast intentionally omits a generic JSON-to-DOM decoder
// (map[string]any / []any), a generic any-encoder for Builder, and a
// whitespace-compacting helper. Fixed-schema callers should use the
// Iterate* / FindField scanners and the typed Add*Field methods
// directly. When dynamic data is genuinely required, encode it with
// encoding/json and splice the result via Builder.AppendRaw or
// Builder.AddRawJSONField; for whitespace compaction use
// encoding/json.Compact.
//
// # NDJSON
//
// BatchWriter appends JSON records separated by '\n' and implements
// io.Writer (one record per Write) and io.WriterTo.
package jsonfast
