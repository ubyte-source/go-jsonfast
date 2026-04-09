// Package jsonfast provides a zero-allocation JSON builder for fixed schemas.
//
// Designed for ultra-high-throughput pipelines (100 Gbps+) where every byte
// and every nanosecond matters. All methods operate on a reusable byte buffer;
// steady-state operation requires zero heap allocations.
//
// Features:
//   - Zero-alloc Builder with Acquire/Release pool
//   - Pre-computed FieldKey for static field names (eliminates per-call quoting)
//   - NDJSON BatchWriter for batching records
//   - JSON scanner: IterateFields, FindField, FlattenObject (SWAR-accelerated)
//   - Flatten nested maps to dot-notation keys
//   - Word-at-a-time SWAR string scanning via unsafe for maximum throughput
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
// # Pre-computed Field Keys
//
// For static field names known at init time, use FieldKey to eliminate
// the per-call quoting overhead:
//
//	var keyMessage = jsonfast.NewFieldKey("message")
//	b.AddStringFieldKey(keyMessage, "hello world")
package jsonfast
