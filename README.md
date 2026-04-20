# go-jsonfast

> A zero-allocation JSON builder and scanner for Go, tailored for fixed
> schemas and ultra-high-throughput pipelines.

[![Go Version](https://img.shields.io/badge/Go-1.25+-blue.svg)](https://golang.org)
[![Go Reference](https://pkg.go.dev/badge/github.com/ubyte-source/go-jsonfast.svg)](https://pkg.go.dev/github.com/ubyte-source/go-jsonfast)
[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg)](https://opensource.org/licenses/MIT)
[![Zero Dependencies](https://img.shields.io/badge/Dependencies-0-brightgreen.svg)](go.mod)

## Features

- Zero-allocation Builder with `Acquire` / `Release` pool and `WarmPool` for pre-seeding.
- Pre-computed `FieldKey` for static field names (typed string, literal-constructible).
- `io.Writer` / `io.WriterTo` on both `Builder` and `BatchWriter` for pipeline interop.
- NDJSON `BatchWriter` with pool, pre-seed, and write sinks.
- SWAR-accelerated string escape and scanner (`SkipValueAt`, `SkipStringAt`, `SkipBracedAt`).
- Direct-scan `FindField`, `IterateFields`, `IterateArray`, `IterateStringArray` (zero-alloc borrow).
- `FlattenObject` / `AddFlattenedMapField` for dot-notation flattening.
- RFC 3339 time formatting without `time.Format` allocations, UTC-normalised.
- RFC 8259 compliance: invalid UTF-8 → U+FFFD; NaN / ±Inf → `null`.
- `IsStructuralJSON` single-pass validator for untrusted payloads.
- `testing.AllocsPerRun` CI gate: every hot path is asserted zero-alloc.
- Native Go fuzzers for every scanner and validator.
- Profile-guided optimisation: `default.pgo` tracked; `make pgo` regenerates.

Requires **Go 1.25+**.

## Quick start

```go
package main

import (
    "fmt"
    "time"

    "github.com/ubyte-source/go-jsonfast"
)

func main() {
    b := jsonfast.Acquire()
    defer jsonfast.Release(b)

    b.BeginObject()
    b.AddStringField("message", "hello world")
    b.AddIntField("severity", 3)
    b.AddTimeRFC3339Field("timestamp", time.Now())
    b.EndObject()

    fmt.Println(string(b.Bytes()))
}
```

### Reuse without allocation

```go
b := jsonfast.New(512)

b.BeginObject()
b.AddStringField("k", "v1")
b.EndObject()
process(b.Bytes())

b.Reset() // ready for reuse, no allocation
b.BeginObject()
b.AddStringField("k", "v2")
b.EndObject()
process(b.Bytes())
```

### NDJSON batching

```go
bw := jsonfast.AcquireBatchWriter()
defer jsonfast.ReleaseBatchWriter(bw)

b := jsonfast.Acquire()
defer jsonfast.Release(b)

for _, msg := range messages {
    b.Reset()
    b.BeginObject()
    b.AddStringField("msg", msg)
    b.EndObject()
    bw.Append(b.Bytes())
}

// bw.Bytes() is the NDJSON payload; stream via io.WriterTo:
if _, err := bw.WriteTo(conn); err != nil {
    return err
}
```

### Pre-computed field keys

Static field names known at init time can be lifted to `FieldKey` to
eliminate the per-call quoting. Construct via `NewFieldKey` or as a
typed string literal:

```go
var (
    keyMessage  = jsonfast.NewFieldKey("message")
    keySeverity = jsonfast.NewFieldKey("severity")

    // Equivalent literal form (matching the documented layout):
    keyVersion jsonfast.FieldKey = `,"version":`
)

b := jsonfast.Acquire()
b.BeginObject()
b.AddStringFieldKey(keyMessage, "hello world")
b.AddIntFieldKey(keySeverity, 3)
b.AddIntFieldKey(keyVersion, 1)
b.EndObject()
jsonfast.Release(b)
```

### Warm-up before the hot path

Pre-seed the pools to smooth tail latency at start-up:

```go
jsonfast.WarmPool(128)
jsonfast.WarmBatchWriterPool(32)
```

### Map flattening

```go
nested := map[string]map[string]string{
    "sd@123": {"key1": "val1", "key2": "val2"},
}
flat := jsonfast.FlattenMap(nested, nil)
// flat["sd@123.key1"] == "val1"
```

### Structural validation

```go
if !jsonfast.IsStructuralJSON(payload) {
    return fmt.Errorf("not a structurally valid JSON object/array")
}
```

## API overview

See the [package documentation](https://pkg.go.dev/github.com/ubyte-source/go-jsonfast) for the full godoc.

### Builder

| Function | Description |
|----------|-------------|
| `New(capacity int) *Builder` | New builder with initial capacity (default 256). |
| `Acquire() *Builder` | Take a builder from the pool. |
| `Release(*Builder)` | Return a builder to the pool. |
| `WarmPool(n int)` | Pre-seed the pool with n builders. |
| `(*Builder).Reset()` | Clear for reuse; buffer retained. |
| `(*Builder).Bytes() []byte` | Current payload (aliases internal buffer). |
| `(*Builder).Len() int` | Current byte length. |
| `(*Builder).Grow(n int)` | Ensure n spare bytes of capacity. |
| `(*Builder).Write(p []byte) (int, error)` | `io.Writer`: append p unchanged. |
| `(*Builder).WriteTo(w io.Writer) (int64, error)` | `io.WriterTo`: flush payload. |

### Field methods (string-keyed)

| Method | Output |
|--------|--------|
| `BeginObject()` / `EndObject()` | `{` / `}` |
| `AddStringField(name, value)` | `"name":"value"` with escaping |
| `AddIntField(name, v int)` | `"name":123` |
| `AddInt64Field(name, v int64)` | `"name":123` |
| `AddUint8Field` / `AddUint16Field` / `AddUint32Field` / `AddUint64Field` | `"name":n` |
| `AddFloat64Field(name, v float64)` | `"name":3.14` — NaN/±Inf → `null` |
| `AddBoolField(name, v bool)` | `"name":true` / `"name":false` |
| `AddNullField(name)` | `"name":null` |
| `AddTimeRFC3339Field(name, t time.Time)` | `"name":"YYYY-MM-DDThh:mm:ss[.fffffffff]Z"` |
| `AddRawJSONField(name, rawJSON []byte)` | `"name":<raw>` — no escaping |
| `AddRawJSONFieldString(name, rawJSON string)` | Same, string input |
| `AddNestedStringMapField(name, m)` | `"name":{outer:{inner:"v",...},...}`, sorted |
| `AddStringMapObject(m, rawJSONKey)` | writes a `map[string]string` as object |
| `AddFlattenedMapField(m)` | flat `"outer.inner":"value"` fields, sorted |

Field name requirement: safe ASCII (no escaping required). For untrusted names,
escape via `EscapeString` or compose via `AppendEscapedString` + `AppendRawString`.

### Pre-computed FieldKey methods

| Function | Notes |
|----------|-------|
| `type FieldKey string` | Typed string holding `,"name":` |
| `NewFieldKey(name string) FieldKey` | Factory; call at init time |
| `AddStringFieldKey` / `AddIntFieldKey` / `AddInt64FieldKey` / `AddUint64FieldKey` | |
| `AddBoolFieldKey` / `AddFloat64FieldKey` / `AddNullFieldKey` | |
| `AddTimeRFC3339FieldKey` | |
| `AddRawJSONFieldKey` / `AddRawJSONFieldKeyString` | |

### Raw appenders

| Method | Description |
|--------|-------------|
| `AppendRaw([]byte)` | Append raw bytes, no escaping or framing |
| `AppendRawString(string)` | Append raw string, no escaping or framing |
| `AppendEscapedString(string)` | Append with JSON escaping, zero-alloc |
| `AddRawBytesField(name, value []byte)` | `"name":<value>` with name as raw bytes |

### Scanner

| Function | Description |
|----------|-------------|
| `IterateFields(data []byte, fn) bool` | Callback for each `"key":value` pair. |
| `IterateFieldsString(s string, fn) bool` | Same, string input. |
| `FindField(data []byte, key string) ([]byte, bool)` | Direct lookup without callback. |
| `FindFieldString(s, key string) ([]byte, bool)` | Same, string input. |
| `IterateArray(data []byte, fn) bool` | Callback for each element. |
| `IterateArrayString(s string, fn) bool` | Same, string input. |
| `IterateStringArray(data []byte, fn func(val string) bool) bool` | Zero-alloc borrow; see lifetime note. |
| `IterateStringArrayString(s string, fn) bool` | Same, string input. |
| `FlattenObject(b *Builder, data []byte) bool` | Recursive flatten into builder (≤ 64 levels). |
| `SkipWS` / `SkipValueAt` / `SkipStringAt` / `SkipBracedAt` | Low-level SWAR scanners. |
| `IsStructuralJSON(s string) bool` | Single-pass grammar validator with trailing-content rejection. |
| `EscapeString(s string) string` | Returns s unchanged if already safe; allocates only when escape needed. |

`IterateStringArray` passes a zero-allocation string view aliasing the input
data. The view is only valid for the duration of the callback — clone via
`strings.Clone` to retain.

### BatchWriter (NDJSON)

| Function | Description |
|----------|-------------|
| `NewBatchWriter(capacity int) *BatchWriter` | New writer with initial capacity (default 4096). |
| `AcquireBatchWriter()` / `ReleaseBatchWriter(*BatchWriter)` | Pool API. |
| `WarmBatchWriterPool(n int)` | Pre-seed the pool. |
| `(*BatchWriter).Append([]byte)` / `.AppendString(string)` | Record + newline. |
| `(*BatchWriter).Write(p []byte) (int, error)` | `io.Writer`: one record per call. |
| `(*BatchWriter).WriteTo(w io.Writer) (int64, error)` | `io.WriterTo`: flush payload. |
| `(*BatchWriter).Bytes()` / `.Len()` / `.Count()` / `.Reset()` / `.Grow(n)` | Buffer management. |

### FlattenMap

| Function | Description |
|----------|-------------|
| `FlattenMap(m, dst map[string]string) map[string]string` | Materialise a flat map; pass `dst` to avoid re-allocating. |

## Benchmarks

Measured on a 32-core x86-64 server, Go 1.25.9, with `default.pgo` enabled.

```
BenchmarkBuilder_FullSyslogObject-32             160 ns/op     0 B/op    0 allocs/op
BenchmarkBuilder_FullSyslogObject_FieldKey-32    150 ns/op     0 B/op    0 allocs/op
BenchmarkBuilder_EscapeString_PureASCII-32        34 ns/op     0 B/op    0 allocs/op
BenchmarkBuilder_NestedStringMapField-32         378 ns/op     0 B/op    0 allocs/op
BenchmarkBuilder_AcquireRelease-32                32 ns/op     0 B/op    0 allocs/op
BenchmarkIterateFields-32                        140 ns/op     0 B/op    0 allocs/op
BenchmarkFindField-32                            104 ns/op     0 B/op    0 allocs/op
BenchmarkIterateArray_Strings100-32             1300 ns/op     0 B/op    0 allocs/op
BenchmarkParallel_FindField-32                   7.4 ns/op  11 GB/s      0 allocs/op
BenchmarkParallel_EscapeString_PureASCII-32      1.9 ns/op  60 GB/s      0 allocs/op
```

Run with:

```bash
make bench        # sequential benchmarks
make parallel     # RunParallel benchmarks across CPU counts
```

## Testing and gates

```bash
make test       # all tests with race detector
make alloc      # zero-allocation assertions (AllocsPerRun)
make fuzz       # native Go fuzzing, 30s per target (override with FUZZTIME=10m)
make cover      # HTML coverage report
make lint       # staticcheck -checks=all + golangci-lint (0 issues, 0 nolint for complexity)
make pgo        # regenerate default.pgo
make ci         # vet + lint + test + alloc + cover + bench
```

## Operational limits

| Parameter | Value | Rationale |
|-----------|-------|-----------|
| Builder pool max retained capacity | 256 KiB | Bounds per-instance retention. |
| BatchWriter pool max retained capacity | 4 MiB | Bounds per-instance retention for batch sinks. |
| `FlattenObject` maximum depth | 64 | Defends against pathological nesting. |
| `AddNestedStringMapField` stack buffer | 8 keys (both levels) | Zero-alloc path; larger maps get one heap buffer per oversized map. |
| `AddFlattenedMapField` stack buffer | 16 total entries | Zero-alloc path; larger maps get one heap buffer. |
| `EscapeString` stack buffer | 506 bytes after escape margin | Zero-alloc for the common case. |

## Design principles

1. Zero-alloc is a constraint, not a goal. Every hot path is gated by
   `testing.AllocsPerRun`; a regression fails CI.
2. Fixed schemas only. This is not a general-purpose JSON encoder.
3. Deterministic output. Keys are sorted wherever the caller may dedupe
   or cache the output.
4. No dependencies. Pure Go, no cgo, no code generation.
5. The buffer is the API. All writes land in `[]byte`; no intermediate
   representations.

## Project layout

```
go-jsonfast/
├── jsonfast.go        # Builder: pool, field methods, escape, time, integer formatting
├── scan.go            # Scanner: Skip*, Iterate*, FindField, FlattenObject, IsStructuralJSON
├── swar.go            # SWAR constants and byte-classification helpers
├── flatten.go         # FlattenMap and AddFlattenedMapField
├── ndjson.go          # BatchWriter: pool, append, io.Writer / io.WriterTo
├── doc.go             # Package documentation
├── alloc_test.go      # Zero-allocation CI gate (testing.AllocsPerRun)
├── parallel_test.go   # RunParallel benchmarks for pool-contention profiling
├── io_test.go         # io.Writer / io.WriterTo interface contract
├── fuzz_test.go       # Native Go fuzzers for scanners and validators
├── *_test.go          # Unit tests, example tests
├── default.pgo        # Profile-guided optimisation profile
├── .golangci.yml      # Strict linter configuration
└── Makefile           # test, bench, fuzz, lint, cover, pgo, ci
```

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). For security issues, see
[SECURITY.md](SECURITY.md).

## Versioning

We use [SemVer](https://semver.org/). Releases are tracked in the
repository [tags](https://github.com/ubyte-source/go-jsonfast/tags).

## Authors

- **Paolo Fabris** — [ubyte.it](https://ubyte.it/)

See also the list of [contributors](https://github.com/ubyte-source/go-jsonfast/contributors).

## License

MIT — see [LICENSE](LICENSE).

## Support

If go-jsonfast is useful for your pipelines, consider supporting the work:

[![Buy Me A Coffee](https://img.shields.io/badge/Buy%20Me%20A%20Coffee-Support-orange?style=for-the-badge&logo=buy-me-a-coffee)](https://coff.ee/ubyte)
