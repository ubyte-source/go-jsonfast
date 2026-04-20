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
make test        # all tests with race detector
make alloc       # zero-allocation CI gate (AllocsPerRun)
make bench       # benchmarks with memory profiling
make parallel    # RunParallel benchmarks at cpu=1,4,8,16,32
make fuzz        # native Go fuzzing (default 30s per target)
make cover       # HTML coverage report
make lint        # golangci-lint + staticcheck
make ci          # vet + lint + test + alloc + cover + bench
```

## Zero-Allocation Constraint

Every documented hot path must produce **0 allocations per operation**.
`make alloc` runs the `TestZeroAlloc_*` suite which gates CI via
`testing.AllocsPerRun`. All benchmarks use `b.Loop()`.

Before submitting a change, run:

```bash
make ci
```

## Code Style

- Follow standard Go conventions (`gofmt`, `goimports`)
- Run `golangci-lint run ./...` (see `.golangci.yml`)
- Comments in English, no abbreviations
- Every exported function and type must have a godoc comment
- Zero unnecessary `nolint` directives — fix the code when possible, use `nolint` only for SWAR/unsafe hot paths with a justifying comment

## Architecture

See [README.md](README.md#project-layout) for the full file map.

## Design Principles

1. **Zero-alloc is a constraint, not a goal.** It guides every design decision.
2. **Fixed schemas only.** Not a general-purpose JSON encoder — tailored for known field sets.
3. **Deterministic output.** Sorted keys for caching and deduplication.
4. **No dependencies.** Pure Go, no external libraries.

## License

By contributing, you agree that your contributions will be licensed under the MIT License.
