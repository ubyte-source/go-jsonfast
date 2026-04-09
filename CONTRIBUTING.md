# Contributing to go-jsonfast

## Development Prerequisites

- **Go 1.25+**
- Make

## Getting Started

```bash
git clone https://github.com/ubyte-source/go-jsonfast.git
cd go-jsonfast
make all
```

## Running Tests

```bash
make test        # All tests with race detector
make bench       # Benchmarks with memory profiling
make fuzz        # Native Go fuzzing (30s)
make cover       # Coverage report
make vet         # Static analysis
```

## Zero-Allocation Constraint

The hot path (`Builder` methods, `Acquire`/`Release`, `BatchWriter`) must produce
**0 allocations per operation**. This is enforced by benchmarks with `-benchmem`.

All benchmarks must use `b.Loop()` (Go 1.24+), not `for range b.N` or `for i := 0; i < b.N; i++`.

Before submitting a change, run:
```bash
go test -bench=. -benchmem ./...
```

Every builder benchmark line must show `0 B/op` and `0 allocs/op`.

## Code Style

- Follow standard Go conventions (`gofmt`, `goimports`)
- Run `golangci-lint run ./...` (see `.golangci.yml`)
- Comments in English, no abbreviations
- Every exported function and type must have a godoc comment
- Zero unnecessary `nolint` directives — fix the code when possible, use `nolint` only for SWAR/unsafe hot paths with a justifying comment

## Architecture

```
jsonfast.go     — Builder: zero-alloc JSON builder with pool, escaping, field methods
scan.go         — JSON scanner: IterateFields, FindField, FlattenObject, Skip*
swar.go         — SWAR constants and detect functions for word-at-a-time scanning
flatten.go      — FlattenMap, AddFlattenedMapField: nested map flattening
ndjson.go       — BatchWriter: NDJSON record batching
doc.go          — Package documentation
```

## Design Principles

1. **Zero-alloc is a constraint, not a goal.** It guides every design decision.
2. **Fixed schemas only.** Not a general-purpose JSON encoder — tailored for known field sets.
3. **Deterministic output.** Sorted keys for caching and deduplication.
4. **No dependencies.** Pure Go, no external libraries.

## License

By contributing, you agree that your contributions will be licensed under the MIT License.
