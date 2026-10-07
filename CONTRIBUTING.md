# Contributing

## Prerequisites

- Go 1.25 or later (the version in `go.mod`), with cgo for the race detector
- make, and jq for `make testtime`

The Makefile runs every tool at a pinned version through `go run`:
golangci-lint v2.14.0, govulncheck v1.8.0, deadcode v0.50.0 and nilaway.
Nothing needs a global install; `GOLANGCI_LINT=<path>`, `GOVULNCHECK=<path>`,
`DEADCODE=<path>` and `NILAWAY=<path>` point the targets at local binaries of
the same versions. golangci-lint builds and runs with Go 1.27.1
(`GOTOOLCHAIN=go1.27.1`), the others with Go 1.26 or later; the go command
fetches a missing toolchain.

```bash
git clone https://github.com/ubyte-source/go-jsonfast.git
cd go-jsonfast
make ci
```

## Workflow

1. Branch from `main` (`feature/…`, `fix/…`, `docs/…`).
2. Change the code together with its tests and with every document the
   change makes untrue: README (API, benchmarks and limits) and SECURITY
   (defenses and limits).
3. Run `make ci` until it passes.
4. Open a pull request describing what changes, why, and how it was tested.

`make ci` runs these targets, and the workflows run the same ones: `lint.yml`
runs `vet`, `lint`, `deadcode` and `vocab`, `test.yml` runs `modcheck`,
`test`, `cover` and `bench-smoke` on Go 1.25.14 and 1.27.1, `security.yml`
runs `vuln` next to CodeQL. `vet`, `lint`, `deadcode`, `test`, `race`,
`cover`, `bench` and `bench-smoke` check both builds, the default one and
`-tags purego`, and `test` also runs with `GOARCH=386`, so 32-bit arithmetic
is gated; `vuln` runs the default build, and so does `fuzz` unless
`FUZZ_TAGS=purego` selects the other one.

| Target        | Gate |
|---------------|------|
| `modcheck`    | `go mod download`, `go mod verify` and `go mod tidy -diff` |
| `vet`         | `go vet ./...` on both builds |
| `lint`        | golangci-lint v2.14.0 with `.golangci.yml` on both builds (gosec runs inside it) |
| `vuln`        | govulncheck v1.8.0 on the module and the toolchain |
| `deadcode`    | deadcode v0.50.0 `-test ./...` on both builds; any output fails |
| `test`        | the tests of both builds and of `GOARCH=386` with `-shuffle=on`; only this run checks allocation counts, which the race detector changes |
| `race`        | the tests of both builds with `-race`, `-shuffle=on` and `CGO_ENABLED=1`, writing `coverage.out` and `coverage-purego.out` |
| `bench-smoke` | every benchmark of both builds 100 times, `-benchtime=100x` |
| `cover`       | `race`, then `coverage.html` and both totals; fails below 100% |
| `vocab`       | no claim name and no word of the token vocabulary the Makefile lists, in the Go and Markdown files |

`make testtime` runs the tests of both builds one at a time under the race
detector and fails on any test slower than `TESTTIME_MAX` seconds (10).
`make final` runs the delivery gates on an idle machine: `fmtcheck`
(`gofmt -s`) and `ci`, then on both builds `nilaway`, `race-repeat`
(`-race -count=10 -shuffle=on`) and `fuzz-final` (every fuzz target for 60 s),
then `testtime` and `treecheck`, which after `make clean` fails on an
artefact, an untracked file of no source kind, an empty directory, a file not
stored with LF, or a `.gitattributes` without `* text=auto eol=lf`.

`make help` lists every target. The fuzz workflow runs every fuzz target
weekly, reading the list through `make fuzz-list`, which asks
`go test -list '^Fuzz'` for every `Fuzz` function of the module and
fails when there is none. `make fuzz` runs the same list for `FUZZTIME`
each, or only the `pkg:Target` entries of `FUZZ_TARGETS`, and fails on
an entry that names no fuzz target.

## Code

- Control flow stays linear; one helper per concern, no duplicated logic,
  no dead code, no code that exists only for tests, and no mutable package
  state but the two pools.
- Hot paths allocate nothing, and a path that allocates by design asserts
  its exact count; the stack takes what escape analysis allows.
- Generic JSON work belongs here, and the standard library is reused
  wherever it is as fast.
- A type is declared above its first use in the file.
- Struct fields are grouped by meaning, with the boolean flags pooled in one
  trailing block; govet runs every analyzer but `fieldalignment`.
- `unsafe` stays in `text.go`, which reinterprets string and slice headers,
  and in `swar_unsafe.go`, which loads eight bytes.
- `//nolint` names one linter and says why the code is safe.
- Production code does not import `encoding/json`, and the module has no
  dependency.

## Comments

- English, present tense, short: at most 3 lines per block and 88 columns.
- They explain what the code cannot say. No history ("previously", "no
  longer", "legacy", "replaces", "deprecated"), no notes to self ("todo",
  "for now"), no citation of an RFC or a Markdown file, no restated code, no
  commented-out code.
- One package comment per package, in `doc.go`, which may be longer.

## Tests

- Every code file `foo.go` has exactly one sibling `foo_test.go` holding all
  its tests, benchmarks, fuzz targets and examples. `doc.go` has none.
- Test names start with a symbol declared in the sibling file:
  `Test<Symbol><Scenario>` in CamelCase, such as
  `TestIterateMembersRejectsNamesEqualAfterDecoding`.
- `doc_test.go` holds package fixtures only (helpers and shared literals),
  never tests.
- Assertions use the standard library only (`t.Fatalf` and `t.Errorf` with
  got and want).
- A test must fail when the behavior it names breaks; check it by breaking
  the code once before you trust it. A negative test asserts the specific
  sentinel.
- Every fuzz target has a real oracle: `encoding/json`, `strconv`,
  `math/big`, `time` or a reference written independently of the code under
  test.
- A benchmark fails when its operation's result is wrong, checked once before
  the timed loop; it times with `b.Loop()`, or `b.RunParallel` for the pool
  benchmarks, and reports allocations.

## Commit messages

Imperative mood, a subject of at most 50 characters, a blank line, then what
changes and why.

## License

Contributions are licensed under the MIT license of the project.
