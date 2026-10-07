# Security Policy

## Supported Versions

| Version | Supported          |
|---------|--------------------|
| latest  | :white_check_mark: |

## Reporting a Vulnerability

1. **Do not** open a public GitHub issue.
2. Use GitHub's private vulnerability reporting (Security, Report a vulnerability).
3. Include a description, steps to reproduce and the impact.
4. Receipt is acknowledged within 48 hours, with a fix timeline.

## Threat Model

go-jsonfast writes JSON for its callers and reads JSON that callers take from
untrusted peers. It defends against:

- Resource exhaustion through deep nesting, long input or many members:
  stack growth, quadratic time and unbounded allocation.
- Panics on any input to a scanner, validator or decoder.
- Parser differentials: input that go-jsonfast reads differently from
  `encoding/json` where both define a result, and member names that repeat
  after decoding.
- Invalid or injected output from the Builder, NDJSON records split by an
  embedded newline included.
- Views that alias memory the library, or a later append, writes again.
- Input bytes reaching a caller's logs through errors.

## Implemented Defenses

- **Depth bound.** `ValidUTF8`, `WalkStrings`, `WalkTokens`, `IterateObject`
  and `IterateDocument` walk iteratively under the caller's `maxDepth`, one bit
  of state per level, so no input grows the stack; `MaxDepth` (10000) is the
  bound of `encoding/json`. The other `Iterate*` and the `Find*` scanners walk
  one level and skip nested containers with a counter. `FlattenObject` accepts
  at most 64 levels of nesting, arrays included. `DecodeValue` decodes each
  value once, in the walk that checks the document by the `ValidUTF8` rule,
  and keeps the containers it is inside on a stack of its own, so no
  `maxDepth` lets input grow the goroutine stack. The value writers keep the
  containers they write on a stack of their own too, and write `null` for one
  nested deeper than `MaxDepth` and for one already open on its own path, so a
  cycle ends where it closes.
- **Strict grammar.** `ValidUTF8(data, MaxDepth)` accepts what `json.Valid`
  accepts, one value, whitespace around it, no trailing content, the eight
  short escapes and `\uXXXX` only, and no raw control byte in a string, but
  for invalid UTF-8 and lone surrogate escapes in strings, which `ValidUTF8`,
  `WalkStrings`, `WalkTokens`, `IterateObject` and `IterateDocument` reject, so
  every string they pass decodes without a U+FFFD substitute; `WalkStrings`
  fails closed on anything else.
- **Walks check what they skip, one level deep.** The other `Iterate*` and
  the `Find*` scanners check strings, with the same escape rule, numbers and
  literals in full. A nested array or object is skipped by counting only its
  own opener and closer, so `[{]` passes as a value. Check an untrusted object
  once with `IterateObject`, or any other value with `ValidUTF8`, before
  walking it further. A walk that fails, `WalkStrings`, `WalkTokens`,
  `IterateObject` and `DecodeValue` included, may have called its callback on
  the items before the fault, so a callback defers its effects until the walk
  returns nil.
- **One decoding rule.** Every decoder follows `encoding/json`: surrogate
  pairs join, and a lone surrogate escape or a byte that `utf8.DecodeRune`
  rejects becomes U+FFFD. `EqualString` and `FindMember` compare by the same
  rule, so a name matches exactly when `encoding/json` would decode it to the
  key.
- **Duplicate names.** `IterateMembers` and `IterateObject` fail with
  `ErrDuplicateName` when two names are equal after decoding, `"a"` and
  `"\u0061"` included, and so are two names that both decode to U+FFFD. Its
  first 32 names sit in a table that bounds every probe, so a crafted set of
  names costs at most a few hundred comparisons; later names go to a map with
  a random seed. `DecodeValue` refuses a repeated name at any depth, each
  object's names in a map, and so does `FlattenObject`, which also refuses two
  leaves whose names decode alike; its name sets are tables like that one.
  `IterateDocument`, `IterateObject` in every other respect, also checks the
  names of every object nested in a member's value that way, and `WalkTokens`
  the names of every object it walks. `IterateFields` and `FindMember` do not
  check; `FindMember` returns the first match.
- **No allocation on hostile numbers.** `DecodeInt64` and `DecodeUint64`
  reject a value past their range without allocating, and `DecodeFloat64` and
  `DecodeFloat32` reject a magnitude past their range before `strconv` would
  copy the input into its error. A number whose point or exponent `strconv`
  would misread, past 800 digits before the point or from an exponent of
  100000, reaches it as a short text of its first 800 significant digits that
  rounds the same, so every such number reads exactly, without allocating;
  the exponent is read in 64 bits, so 32-bit targets read it alike.
- **Views.** A raw view is a sub-slice of the input whose capacity ends with
  it, so an append copies instead of overwriting the input. A decoded string
  is either such a view or a slice of a buffer the call never writes again,
  so a callback must not write to a view either. The only `unsafe` code
  reinterprets string and slice headers without writing, and loads eight
  bytes that the caller has bounded; `-tags=purego` replaces the load with
  `encoding/binary`.
- **Errors.** `ErrMalformed` and `ErrDuplicateName` are bare sentinels that
  carry no input bytes.
- **Output.** The Builder escapes every name and value it takes as a Go
  string, writes U+FFFD for each invalid UTF-8 byte and `null` for NaN and
  ±Inf, and builds a `FieldKey` only through `NewFieldKey`, which escapes the
  name once. `FlattenObject` embeds JSON only when `ValidUTF8` accepts it,
  compacted, so it writes no newline, invalid UTF-8 or lone surrogate escape.
  No writer of member names from its input, `FlattenObject` and the value
  writers, writes a name twice: keys that differ only in bytes written as
  U+FFFD are written once. The value writers write a number's text as it is
  only when `IsNumber` accepts it, and as a string otherwise. `AppendRaw`,
  `AppendRawString`, `AddRawJSONField`, `AddRawJSONFieldKey`,
  `AddRawJSONElement`, `AddRawBytesField` and `AddRawMembers` write their input
  as it is: the caller vouches for it, and a `BatchWriter` record is one JSON
  text without a newline.

## Known Limits

| Parameter | Limit |
|-----------|-------|
| `ValidUTF8`, `WalkStrings`, `WalkTokens`, `IterateObject` and `IterateDocument` depth | the caller's `maxDepth`; `MaxDepth` (10000) matches `json.Valid` |
| Validator stack use | constant |
| Validator memory | 256 levels inline, then one bit per level reached, in a spill that grows with the depth |
| `IterateMembers`, `IterateObject` and `IterateDocument` name set | 32 names inline, then one map keyed by the name views |
| `IterateDocument` nested name sets | one for each object open inside a member's value: 2 inline, then heap stacks that grow with the depth; decoded nested names take chunks of their own, apart from the member names, and give their room back when their object closes; a set reuses the map of the set closed at its level, cleared, when it held at most 1024 names, and drops a larger one |
| Decoded strings of `IterateStringArray` and `WalkStrings` | chunks that start at the first string to decode; a new chunk takes the rest of the document up to 4 KiB, twice the last chunk or the string it must hold, whichever is most |
| `WalkTokens` name sets | one for each open object: 2 inline, then a heap stack that grows with the depth; 32 names inline each, then one map keyed by the name views; a set reuses the map of the set closed at its level, cleared, when it held at most 1024 names, and drops a larger one |
| Decoded names of `IterateMembers`, `IterateObject`, `IterateDocument`, `FlattenObject` and `WalkTokens` | the same chunks, for the names, and a second sequence of them for the nested names of `IterateDocument` and the names of objects of `FlattenObject`; `FlattenObject` keeps its leaf names for the call, the names of an object inside a leaf give their room back when it closes, and those of the objects outside the leaves when the object that holds them closes |
| `FlattenObject` depth | 64 levels, arrays included |
| `FlattenObject` leaf names | 32 names inline, then one map keyed by the name views |
| `FlattenObject` object names | the leaf names of the innermost object in a list of 32; an object that holds an object or more leaves, and every object inside a leaf, has a name set: 32 names inline, then one map keyed by the name views; 2 sets inline, then a heap stack that grows with the depth; every set also has a mark of where its object's names start, one per level at most, all inline; the next set at the same level reuses the map of the set closed there, cleared, when it held at most 1024 names, and drops a larger one, since clearing a map takes time in proportion to its capacity |
| `DecodeValue` stack use | constant: 16 open containers inline, then a heap stack that grows with the depth |
| Decoded strings of `DecodeValue` | the same chunks, for every string of a byte slice input and the escaped ones of a string input |
| Value writers stack use | constant: 16 open containers and 32 keys inline, then heap stacks that grow with the depth |
| Value writers depth | `MaxDepth` containers; a deeper one, or one open on its own path, is written as `null` |
| Builder pool | Builders whose buffer capacity is at most 256 KiB are kept |
| BatchWriter pool | writers whose buffer capacity is at most 4 MiB are kept |

A Builder or a BatchWriter past its limit is dropped on release rather than
kept in the pool.

## Continuous Verification

- `make ci` runs every gate below but CodeQL and fuzzing, and the workflows
  run the same Makefile targets; `vet`, `lint`, `deadcode`, `test`, `race`,
  `cover` and `bench-smoke` check both the default and the `purego` build, and
  `test` the 386 build too.
- `make modcheck` (`go mod download`, `go mod verify`, `go mod tidy -diff`),
  `make test` (the tests with `-shuffle=on` and their allocation counts, on
  amd64 and on 386), `make cover` (the tests with `-race` and `-shuffle=on`,
  failing below 100% statement coverage) and `make bench-smoke` (100
  iterations of every benchmark) on Go 1.25.14 and Go 1.27.1 (`test.yml`);
  `make vet` (`lint.yml`).
- `golangci-lint` v2.14.0, built with Go 1.27.1 (`lint.yml`, `make lint`),
  with the repository `.golangci.yml`: every linter of its enable list and the
  `gofmt` and `goimports` formatters, `gosec` included, test files linted. Its
  part shared with go-authware and mcp-server leaves out `run.go` and the
  rules each repository adds; here the `depguard` rule `unsafe` denies
  `unsafe` to every production file but `text.go` and `swar_unsafe.go`.
  govet's `fieldalignment` is off, since struct fields are grouped by meaning.
  `revive` runs every rule, `add-constant` and `line-length-limit` included,
  and `unhandled-error` skips the calls errcheck excludes by default.
  `wrapcheck` lets pass the errors of go-authware's internal packages and
  those a decorator forwards from its delegate, and `depguard` and
  `gomodguard_v2` block assertion libraries. Every `nolint` directive names
  the linter and gives a reason.
- `deadcode -test` v0.50.0 (`lint.yml`, `make deadcode`): no function that
  neither a test nor an entry point reaches.
- Every action is pinned by commit with its version, and every workflow has
  top-level permissions no wider than `contents: read` (CodeQL's in
  `security.yml`), no `pull_request_target` trigger and checkouts without
  persisted credentials. Every `nolint` directive names the linter and gives
  a reason.
- `govulncheck` v1.8.0 and CodeQL with the `security-and-quality` suite
  (`security.yml`).
- Native fuzzing (`fuzz.yml`, weekly, and `make fuzz`) over every `Fuzz`
  function, listed by `make fuzz-list`: every scanner, validator and decoder,
  and the Builder's escaping, number, time, map, value, flattening and batch
  writers, each with `encoding/json`, `strconv`, `math/big`, `time` or a
  reference written independently of the code as its oracle.
