# go-jsonfast

> A zero-allocation JSON builder and scanner for Go. Scanners walk the bytes, the Builder writes
> them, `DecodeValue` alone builds Go values and the value writers write them back.

[![Go Version](https://img.shields.io/badge/Go-1.25+-blue.svg)](https://golang.org)
[![Lint](https://github.com/ubyte-source/go-jsonfast/actions/workflows/lint.yml/badge.svg)](https://github.com/ubyte-source/go-jsonfast/actions/workflows/lint.yml)
[![Test](https://github.com/ubyte-source/go-jsonfast/actions/workflows/test.yml/badge.svg)](https://github.com/ubyte-source/go-jsonfast/actions/workflows/test.yml)
[![Security](https://github.com/ubyte-source/go-jsonfast/actions/workflows/security.yml/badge.svg)](https://github.com/ubyte-source/go-jsonfast/actions/workflows/security.yml)
[![Fuzz](https://github.com/ubyte-source/go-jsonfast/actions/workflows/fuzz.yml/badge.svg)](https://github.com/ubyte-source/go-jsonfast/actions/workflows/fuzz.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/ubyte-source/go-jsonfast.svg)](https://pkg.go.dev/github.com/ubyte-source/go-jsonfast)
[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg)](https://opensource.org/licenses/MIT)
[![Zero Dependencies](https://img.shields.io/badge/Dependencies-0-brightgreen.svg)](go.mod)

## Features

- A Builder with an `Acquire` / `Release` pool and `Build`; the zero value is ready to use.
- A closed matrix: every kind of value has a Field, a FieldKey and an Element writer, and the container
  openers a bare form too, `{Object, Array} × {bare, Field, FieldKey, Element}`, so arrays of any shape
  need no hand-written commas; the value writers write a whole Go value, a `[]any` included.
- Precomputed `FieldKey` prefixes for names known in advance.
- Scanners generic over `Text` (`string`, `[]byte`, `json.RawMessage`, …): every view has the caller's
  type, string callers get substrings they can keep, and byte views are capped so an append never
  writes over the input.
- One result convention: `Iterate*`, `WalkStrings`, `WalkTokens`, `DecodeValue` and `FlattenObject`
  return `error` (`ErrMalformed`, `ErrDuplicateName`, or the callback's error unchanged); `ValidUTF8`,
  `IsNumber`, `Find*` and the other `Decode*` return `bool`.
- `IterateMembers` rejects member names that are equal after decoding, without allocating for up to
  32 names that need no decoding, and `IterateObject` checks an untrusted object and walks it in one
  pass.
- Number decoders that allocate nothing on any input and read every float exactly, however many
  digits or exponent digits it has.
- One string decoding rule, the one `encoding/json` applies: surrogate pairs join, and lone
  surrogates and each invalid byte become U+FFFD.
- `DecodeValue` decodes a document into Go values as `encoding/json` decodes one into an `any`,
  in the one walk that checks it by the `ValidUTF8` rule, refusing repeated member names and handing
  each number's text to the caller, with constant stack use at any depth; the value writers write
  such values back, ints and `uint64`s too, with sorted keys, cycles cut and constant stack use, any
  other type through the caller's function.
- An iterative validator, `ValidUTF8`, that accepts what `json.Valid` accepts at `MaxDepth` but invalid
  UTF-8 and lone surrogate escapes, with a depth bound the caller chooses and constant stack use.
- SWAR scanning and escaping, eight bytes per step; `-tags=purego` reads the words through
  `encoding/binary` instead of an unaligned `unsafe` load.
- Float output byte-identical to `encoding/json`, and RFC 3339 times without `time.Format`.
- Allocation assertions on every hot path, and native fuzz targets with `encoding/json`, `strconv`
  and `math/big` oracles.

Requires **Go 1.25+**.

## Quick start

```go
b := jsonfast.Acquire()
defer jsonfast.Release(b)

b.BeginObject()
b.AddStringField("msg", "hello")
b.AddIntField("sev", 2)
b.AddTimeRFC3339Field("ts", time.Unix(0, 0))
b.BeginArrayField("tags")
b.AddStringElement("auth")
b.AddStringElement("ssh")
b.EndArray()
b.EndObject()

fmt.Println(string(b.Bytes()))
// Output: {"msg":"hello","sev":2,"ts":"1970-01-01T00:00:00Z","tags":["auth","ssh"]}
```

`Build` wraps the pool lifecycle around one document and returns an owned copy:

```go
out := jsonfast.Build(func(b *jsonfast.Builder) {
	b.BeginObject()
	b.BeginArrayField("ids")
	b.AddInt64Element(1)
	b.AddInt64Element(2)
	b.EndArray()
	b.AddBoolField("ok", true)
	b.EndObject()
})
fmt.Println(string(out))
// Output: {"ids":[1,2],"ok":true}
```

### Reading untrusted input

Check an object and walk it in one pass where input enters the program:

```go
const maxDepth = 32
body := []byte(`{"user":"alice","count":2,"user":"mallory"}`)
var (
	user  string
	count int64
)
err := jsonfast.IterateObject(body, maxDepth, func(name, value []byte) error {
	var ok bool
	switch string(name) {
	case "user":
		user, ok = jsonfast.DecodeString(value)
	case "count":
		count, ok = jsonfast.DecodeInt64(value)
	default:
		return nil
	}
	if !ok {
		return errBadRequest
	}
	return nil
})
fmt.Println(user, count, errors.Is(err, jsonfast.ErrDuplicateName), errors.Is(err, jsonfast.ErrMalformed))
// Output: alice 2 true true
```

### NDJSON batching

```go
w := jsonfast.AcquireBatchWriter()
defer jsonfast.ReleaseBatchWriter(w)
b := jsonfast.Acquire()
defer jsonfast.Release(b)

for _, msg := range []string{"first", "second"} {
	b.Reset()
	b.BeginObject()
	b.AddStringField("msg", msg)
	b.EndObject()
	w.Append(b.Bytes())
}
fmt.Print(string(w.Bytes()))
// Output:
// {"msg":"first"}
// {"msg":"second"}
```

Each record is one JSON text without a newline. Only the raw writers of the Builder can write one.

## API

The [package documentation](https://pkg.go.dev/github.com/ubyte-source/go-jsonfast) has the full
godoc.

### Builder lifecycle

| Function | Description |
|----------|-------------|
| `New(capacity int) *Builder` | A Builder with that capacity; 256 bytes below 1. |
| `Acquire() *Builder` / `Release(*Builder)` | A pooled Builder; one whose buffer capacity passes 256 KiB is not kept. |
| `Build(fn func(b *Builder)) []byte` | Runs fn on a pooled Builder and returns an exact, never-nil copy: one allocation. |
| `Reset()` / `Bytes()` / `Len()` / `Grow(n)` | Buffer management; `Bytes` aliases the buffer. |

### Containers

| Method | Output |
|--------|--------|
| `BeginObject()` / `BeginArray()` | `{` / `[`, with no separator, so they compose after any key writer |
| `BeginObjectField(name)` / `BeginArrayField(name)` | `"name":{` / `"name":[` |
| `BeginObjectFieldKey(k)` / `BeginArrayFieldKey(k)` | the same with a `FieldKey` |
| `BeginObjectElement()` / `BeginArrayElement()` | `{` / `[` as the next element, after a comma when one precedes it |
| `EndObject()` / `EndArray()` | `}` / `]`, which close any opener; the next field or element gets its comma |

### Fields and elements

| Method | Output |
|--------|--------|
| `AddStringField(name, v)` / `AddStringElement(v)` | `"name":"v"` / `"v"`, escaped |
| `AddStringBytesField(name, v)` / `AddStringBytesElement(v)` | the same for a `[]byte` v, read where it lies |
| `AddIntField` / `AddInt64Field` / `AddUint64Field` / `AddIntElement` / `AddInt64Element` / `AddUint64Element` | integers |
| `AddFloat64Field` / `AddFloat64Element` | the text of `encoding/json`; NaN and ±Inf are `null` |
| `AddBoolField` / `AddBoolElement` / `AddNullField` / `AddNullElement` | literals |
| `AddRawJSONField(name, raw)` / `AddRawJSONElement(raw)` | `raw` as it is, one valid JSON value; an empty raw leaves the value to the next writer, which then writes no separator: `BeginObject`, `BeginArray` or a raw appender |
| `AddStringArrayField(name, values)` / `AddStringArrayElement(values)` | `"name":["v1",...]` / `["v1",...]`; nil or empty values write `[]` |
| `AddTimeRFC3339Field(name, t)` / `AddTimeRFC3339Element(t)` | `"YYYY-MM-DDThh:mm:ss[.fffffffff]Z"`, t in UTC |
| `AddTimeRFC3339OffsetField(name, t)` / `AddTimeRFC3339OffsetElement(t)` | the same with t's zone offset, truncated to whole minutes |
| `AddRawBytesField(name, raw)` | `AddRawJSONField` with name written as it is, the body of a JSON string |
| `AddRawMembers(raw)` | the members of the JSON object raw as they are, after a comma when a field precedes them; nothing for an object with none |
| `AddValueField(name, v, text)` / `AddValueElement(v, text)` | v, with `text func(v any) (string, bool)`: a `string`, `bool`, `int`, `int64`, `uint64` or `float64` as the Element writers write it, a `map[string]any` with keys sorted by name as written and one key per name, a `[]any` in order, and any other non-nil value as the text `text` reports for it, as it is when `IsNumber` accepts it and as a string otherwise; `null` for `nil`, a nil map or slice, a value `text` does not report or a nil `text`, a container already open on its own path, which ends a cycle, and a container nested deeper than `MaxDepth` |

Every Field writer has an Element twin and an `Add*FieldKey` twin that takes a `FieldKey`, but
`AddRawBytesField`, whose name is already written: a `FieldKey` is that same idea, and
`AddRawJSONElement` its element form. Keys that differ only in bytes written as U+FFFD make one
name, which the value writers write once.

`NewFieldKey(name)` builds the `FieldKey` of the `Add*FieldKey` and `Begin*FieldKey` writers, its
name escaped once, and only it builds one; every name the Builder takes as a Go string is escaped,
so any Go string is safe.

Times are written from year 0 to the end of year 9999, the range of `time.Time.MarshalJSON`; an
instant outside that range is written as its nearest end. When a zone offset spans a day, or would
move the wall clock out of that range, the offset writer writes the instant in UTC.

### Raw appenders

| Method | Description |
|--------|-------------|
| `AppendRaw(p)` / `AppendRawString(s)` | Appends the bytes as they are. |
| `AppendEscaped(p)` / `AppendEscapedString(s)` | Appends with JSON escaping and no quotes; the two write the same bytes. |
| `EscapeString(s) string` | The escaped form of s; s itself when nothing needs escaping. |

Escaping writes the short escapes for `"`, `\`, `\b`, `\f`, `\n`, `\r` and `\t`, `\u00XX` for the other
control bytes, and U+FFFD for each byte `utf8.DecodeRune` rejects. There is no HTML escaping.

### Scanning

| Function | Description |
|----------|-------------|
| `IterateFields(data, fn func(key, value T) error) error` | The quoted key and raw value of each member; compare keys with `EqualString`. |
| `IterateMembers(data, fn func(name, value T) error) error` | Decoded names; `ErrDuplicateName` when two names are equal after decoding. |
| `IterateObject(data, maxDepth, fn func(name, value T) error) error` | `IterateMembers` in one pass that accepts only an object `ValidUTF8(data, maxDepth)` accepts; `ErrMalformed` wins over `ErrDuplicateName`. |
| `IterateDocument(data, maxDepth, fn func(name, value T) error) error` | `IterateObject` that also fails with `ErrDuplicateName`, in the same pass, when an object nested in a member's value repeats a name. |
| `IterateArray(data, fn func(elem T) error) error` | Raw elements. |
| `IterateStringArray(data, fn func(s T) error) error` | Decoded string elements; any other element is `ErrMalformed`. |
| `FindMember(data, name) (T, bool)` | The first member whose decoded name equals name; stops there. |
| `FindElement(data, index) (T, bool)` | The element at index; stops there. |
| `FlattenObject(b, data) error` | The leaves of an object as fields of b, values compacted; with b left as it was, `ErrMalformed` unless `ValidUTF8(data, 64)`, then `ErrDuplicateName` when an object repeats a decoded name or two leaf names decode alike. |
| `TrimWS(data) T` | A view of data without the JSON whitespace at either end. |
The walks but `IterateObject`, `IterateDocument` and `FlattenObject` check strings, numbers and
literals in full, and skip a nested array or object by counting only its own opener and closer, so
`[{]` passes. Malformed input is `ErrMalformed`, and the callback's first error comes back unchanged;
a walk that fails may have called the callback on the items before the fault, so act on them once it
returns nil. Views are sub-slices of the input, capacity capped, or, for decoded strings, carved from
a buffer the call owns and never rewrites, so every view stays valid for as long as the input does; a
callback must not write to them.

### Validation

| Function | Description |
|----------|-------------|
| `ValidUTF8(data, maxDepth) bool` | One JSON value, whitespace around it allowed, at most maxDepth levels deep, every string valid UTF-8 with no lone surrogate escape; `ValidUTF8(d, MaxDepth)` is `json.Valid(d)` for a d no string of which decodes to U+FFFD. |
| `WalkStrings(data, maxDepth, fn func(s T) error) error` | Every string, names included, decoded in document order, under the `ValidUTF8` rule. |
| `WalkTokens(data, maxDepth, fn func(name, token T) error) error` | Every scalar, string, opener and closer in document order, as it lies, with the decoded name of the member it starts, empty for an element, the top value and a closer, under the `ValidUTF8` rule; `ErrDuplicateName` when an object repeats a name. One pass checks the document and hands it out, so a caller can rewrite it in that pass. |
| `KindOf(raw) Kind` | `KindNull`, `KindBool`, `KindNumber`, `KindString`, `KindArray` or `KindObject` from the first byte after whitespace; `KindInvalid` for blank input or any other byte. |
| `MaxDepth` | 10000, the bound of `encoding/json`. |

### Decoding

| Function | Description |
|----------|-------------|
| `DecodeStringView(raw) (T, bool)` | The decoded string; a view of raw when nothing needs decoding. |
| `DecodeString(raw) (string, bool)` | A Go string; string input that needs no decoding is not copied. |
| `EqualString(raw, s) bool` | Whether the decoded content of raw equals s, compared without decoding into memory. |
| `DecodeBool` / `DecodeInt64` / `DecodeUint64` | Literals and integers: no fraction, exponent, `+` or leading zero, and no `-` for `DecodeUint64`; an integer beyond `int64`, or `uint64` for `DecodeUint64`, gives 0 and false. |
| `DecodeFloat64` / `DecodeFloat32` | The nearest float of that size in one rounding, without allocating; a value beyond it gives 0 and false. |
| `IsNumber(raw) bool` | Exactly one JSON number, so a `DecodeFloat64` failure after it means the value lies beyond float64. |
| `DecodeValue(data, maxDepth, number func(raw T) any) (any, error)` | `map[string]any`, `[]any`, `string`, `bool`, `nil`, and `number`'s result for each number; `ErrMalformed` unless `ValidUTF8(data, maxDepth)`, `ErrDuplicateName` for a repeated name; number must not be nil and may have seen the numbers before a fault. |

All decoders share the rule of `encoding/json`: the eight short escapes and `\uXXXX` only, raw control
bytes rejected, surrogate pairs joined, and U+FFFD for a lone surrogate escape and for each byte
`utf8.DecodeRune` rejects, so a truncated `F0 9F` gives two.

### Errors

| Value | Meaning |
|-------|---------|
| `ErrMalformed` | The input is not the JSON the walk expects. |
| `ErrDuplicateName` | A member name repeats after decoding; `errors.Is(ErrDuplicateName, ErrMalformed)` holds. |

Errors never carry input bytes, and their wording is the caller's to choose.

### BatchWriter (NDJSON)

| Function | Description |
|----------|-------------|
| `NewBatchWriter(capacity int) *BatchWriter` | A writer with that capacity; 4 KiB below 1. |
| `AcquireBatchWriter()` / `ReleaseBatchWriter(*BatchWriter)` | The pool; a writer whose buffer capacity passes 4 MiB is not kept. |
| `Append(record)` / `AppendString(record)` | One record, a JSON text without a newline, and a `'\n'`. |
| `Bytes()` / `Len()` / `Count()` / `Reset()` / `Grow(n)` | Buffer management. |

## Benchmarks

Medians of six runs of `go test -run '^$' -bench . -benchmem -count 6` with Go 1.27.3 on
linux/amd64, an Intel Xeon Gold 6442Y pinned to 11 cores with GOMAXPROCS 11 on a shared host,
default build, no PGO. `ValidUTF8EncodingJSON` runs `json.Valid` on the input of `ValidUTF8`,
`BuilderAppendInt64Strconv` runs `strconv.AppendInt` on the values of `BuilderAppendInt64`,
`DecodeInt64Strconv` and `DecodeUint64Strconv` read the texts of `DecodeInt64` and `DecodeUint64`
with `strconv.ParseInt` and `strconv.ParseUint`, `TrimWSBytesTrim` trims the input of `TrimWS`
with `bytes.Trim`, `BuilderAddTimeRFC3339FieldTimeAppendFormat` writes the field of
`BuilderAddTimeRFC3339Field` with `time.Time.AppendFormat`, `PlainRunSharedForm` and
`BodyRunSharedForm` run the inputs of `PlainRun` and `BodyRun` through one generic form of both,
`SkipNameSharedForm` and `StrictEndSharedForm` read the inputs of `SkipName` and `StrictEnd` with
`strictName` and `strictToken`, which also decode them, and `StringEndGenericForm` and
`StrictEndGenericForm` read the inputs of `StringEnd` and `StrictEnd` through one generic form of
both. The cases that allocate are the ones that decode or copy by design: strings and names with
escapes, `EscapeString` with escapes, `Build`, whose copy is the result, `DecodeValue`, whose Go
values are the result, `FlattenObject/Deep64`, whose 126 leaf names pass the 32 its leaf name
set holds inline and whose 63 objects that hold an object take a name set each,
`WalkTokens/Members`, whose 128 names pass the 32 a name set holds inline, `WalkTokens/ToolResult`,
whose four objects open at once pass the two name sets held inline, and the `AtLimit` cases, whose
10,000 levels pass the 256 the walk holds inline. The `Tokens` cases and the kinds `WalkTokens` adds
to `Flat` and `Nested` hold one kind of token each, nesting at `MaxDepth`, or a 64 KiB tool result.

| Benchmark | ns/op | MB/s | B/op | allocs/op |
|-----------|------:|-----:|-----:|----------:|
| `BuilderAddStringField` | 92.4 | 531 | 0 | 0 |
| `BuilderSyslogObject` | 228 | 1,025 | 0 | 0 |
| `BuilderArray` | 1,200 | 464 | 0 | 0 |
| `BuilderFields` | 332 | 553 | 0 | 0 |
| `BuilderElements` | 155 | 735 | 0 | 0 |
| `BuilderAppendRaw` | 6.54 | 1,987 | 0 | 0 |
| `DecodeStringView/Clean` | 21.1 | 2,838 | 0 | 0 |
| `DecodeStringView/Escaped` | 97.1 | 680 | 80 | 1 |
| `DecodeStringView/Unicode` | 413 | 494 | 208 | 1 |
| `DecodeStringView/UnicodeEscapes` | 1,044 | 362 | 384 | 1 |
| `DecodeStringView/EscapeRun` | 390 | 210 | 96 | 1 |
| `DecodeString/Clean` | 21.2 | 2,829 | 0 | 0 |
| `DecodeString/Escaped` | 105 | 626 | 80 | 1 |
| `DecodeString/Unicode` | 441 | 463 | 208 | 1 |
| `DecodeString/UnicodeEscapes` | 1,147 | 330 | 384 | 1 |
| `DecodeString/EscapeRun` | 409 | 201 | 96 | 1 |
| `EqualString/Clean` | 25.2 | 2,380 | 0 | 0 |
| `EqualString/Escaped` | 67.1 | 983 | 0 | 0 |
| `EqualString/Unicode` | 348 | 586 | 0 | 0 |
| `EqualString/UnicodeEscapes` | 1,010 | 374 | 0 | 0 |
| `EqualString/EscapeRun` | 291 | 282 | 0 | 0 |
| `DecodeBool` | 1.75 |  | 0 | 0 |
| `DecodeInt64` | 26.6 |  | 0 | 0 |
| `DecodeInt64Strconv` | 29.9 |  | 0 | 0 |
| `DecodeUint64` | 31.4 |  | 0 | 0 |
| `DecodeUint64Strconv` | 31.3 |  | 0 | 0 |
| `DecodeFloat64` | 67.8 |  | 0 | 0 |
| `DecodeFloat32` | 60.0 |  | 0 | 0 |
| `IsNumber` | 13.0 |  | 0 | 0 |
| `BuilderAppendEscaped/Short` | 12.0 | 333 | 0 | 0 |
| `BuilderAppendEscaped/Field` | 12.1 | 1,405 | 0 | 0 |
| `BuilderAppendEscaped/ASCII` | 24.7 | 3,357 | 0 | 0 |
| `BuilderAppendEscaped/Escapes` | 54.6 | 1,318 | 0 | 0 |
| `BuilderAppendEscaped/Unicode` | 72.7 | 1,129 | 0 | 0 |
| `BuilderAppendEscaped/Long` | 1,018 | 3,143 | 0 | 0 |
| `BuilderAppendEscapedString/Short` | 12.2 | 329 | 0 | 0 |
| `BuilderAppendEscapedString/Field` | 12.9 | 1,320 | 0 | 0 |
| `BuilderAppendEscapedString/ASCII` | 32.1 | 2,594 | 0 | 0 |
| `BuilderAppendEscapedString/Escapes` | 60.8 | 1,185 | 0 | 0 |
| `BuilderAppendEscapedString/Unicode` | 91.3 | 899 | 0 | 0 |
| `BuilderAppendEscapedString/Long` | 1,048 | 3,055 | 0 | 0 |
| `EscapeString/Plain` | 17.1 | 3,864 | 0 | 0 |
| `EscapeString/Escapes` | 80.3 | 598 | 64 | 1 |
| `EscapeString/EscapeRun` | 188 | 213 | 80 | 1 |
| `EscapeString/Unicode` | 290 | 691 | 0 | 0 |
| `FieldKeySyslogObject` | 141 | 1,657 | 0 | 0 |
| `FieldKeyFields` | 653 | 395 | 0 | 0 |
| `FlattenObject/Record` | 656 | 177 | 0 | 0 |
| `FlattenObject/Deep64` | 65,050 | 27 | 87,416 | 11 |
| `KindOf` | 5.65 |  | 0 | 0 |
| `BatchWriterAppend` | 111 | 11,818 | 0 | 0 |
| `BatchWriterAppendString` | 119 | 10,996 | 0 | 0 |
| `BuilderAppendInt64/7` | 4.43 | 226 | 0 | 0 |
| `BuilderAppendInt64/512` | 4.52 | 664 | 0 | 0 |
| `BuilderAppendInt64/1705321845` | 13.8 | 722 | 0 | 0 |
| `BuilderAppendInt64/-9223372036854775808` | 20.3 | 983 | 0 | 0 |
| `BuilderAppendInt64Strconv/7` | 6.69 | 149 | 0 | 0 |
| `BuilderAppendInt64Strconv/512` | 12.2 | 245 | 0 | 0 |
| `BuilderAppendInt64Strconv/1705321845` | 14.4 | 694 | 0 | 0 |
| `BuilderAppendInt64Strconv/-9223372036854775808` | 17.7 | 1,129 | 0 | 0 |
| `BuilderAppendFloat64/Integral` | 15.0 | 666 | 0 | 0 |
| `BuilderAppendFloat64/Fraction` | 44.0 | 387 | 0 | 0 |
| `BuilderAppendFloat64/Exponent` | 38.7 | 543 | 0 | 0 |
| `Acquire` | 67.3 |  | 0 | 0 |
| `AcquireParallel` | 11.9 |  | 0 | 0 |
| `Build` | 58.9 |  | 16 | 1 |
| `AcquireBatchWriterParallel` | 28.1 |  | 0 | 0 |
| `TrimWS` | 10.0 | 1,493 | 0 | 0 |
| `TrimWSBytesTrim` | 20.8 | 720 | 0 | 0 |
| `SkipValueAt` | 91.8 | 653 | 0 | 0 |
| `SkipStringAt` | 24.1 | 2,619 | 0 | 0 |
| `SkipBraced` | 128 | 570 | 0 | 0 |
| `IterateFields` | 169 | 592 | 0 | 0 |
| `IterateMembers/Plain` | 205 | 488 | 0 | 0 |
| `IterateMembers/Escaped` | 263 | 205 | 64 | 1 |
| `IterateMembers/Names32` | 1,032 | 261 | 0 | 0 |
| `IterateObject/Plain` | 323 | 309 | 0 | 0 |
| `IterateObject/Nested` | 778 | 400 | 0 | 0 |
| `IterateObject/Names32` | 1,526 | 176 | 0 | 0 |
| `IterateDocument/Flat` | 364 | 275 | 0 | 0 |
| `IterateDocument/Nested` | 506 | 184 | 0 | 0 |
| `WalkTokens/Flat` | 413 | 242 | 0 | 0 |
| `WalkTokens/Nested` | 565 | 164 | 0 | 0 |
| `WalkTokens/Arrays` | 5,628 | 45 | 0 | 0 |
| `WalkTokens/Members` | 19,752 | 53 | 10,104 | 6 |
| `WalkTokens/Digits1` | 3,669 | 70 | 0 | 0 |
| `WalkTokens/Digits8` | 3,808 | 303 | 0 | 0 |
| `WalkTokens/Digits64` | 7,633 | 1,093 | 0 | 0 |
| `WalkTokens/Strings` | 3,338 | 729 | 0 | 0 |
| `WalkTokens/Escapes` | 6,986 | 367 | 0 | 0 |
| `WalkTokens/Literals` | 10,348 | 198 | 0 | 0 |
| `WalkTokens/Spaces` | 37,818 | 149 | 0 | 0 |
| `WalkTokens/AtLimit` | 458,948 | 44 | 4,088 | 9 |
| `WalkTokens/ToolResult` | 271,386 | 242 | 2,688 | 1 |
| `SkipName` | 35.5 | 619 | 0 | 0 |
| `SkipNameSharedForm` | 87.0 | 253 | 24 | 1 |
| `IterateArray` | 117 | 333 | 0 | 0 |
| `IterateStringArray` | 1,460 | 887 | 0 | 0 |
| `FindMember` | 136 | 609 | 0 | 0 |
| `FindElement` | 110 | 372 | 0 | 0 |
| `PlainRun` | 54.1 | 4,748 | 0 | 0 |
| `PlainRunSharedForm` | 82.3 | 3,125 | 0 | 0 |
| `BodyRun` | 60.4 | 4,787 | 0 | 0 |
| `BodyRunSharedForm` | 94.1 | 3,070 | 0 | 0 |
| `BuilderAddTimeRFC3339Field` | 53.9 | 816 | 0 | 0 |
| `BuilderAddTimeRFC3339Element` | 95.6 | 701 | 0 | 0 |
| `BuilderAddTimeRFC3339FieldTimeAppendFormat` | 57.1 | 771 | 0 | 0 |
| `BuilderAddTimeRFC3339OffsetField` | 64.9 | 755 | 0 | 0 |
| `ValidUTF8EncodingJSON` | 509 | 611 | 0 | 0 |
| `ValidUTF8` | 675 | 461 | 0 | 0 |
| `ValidUTF8Tokens/Arrays` | 1,342 | 191 | 0 | 0 |
| `ValidUTF8Tokens/Members` | 3,624 | 288 | 0 | 0 |
| `ValidUTF8Tokens/Digits1` | 1,764 | 146 | 0 | 0 |
| `ValidUTF8Tokens/Digits8` | 2,350 | 491 | 0 | 0 |
| `ValidUTF8Tokens/Digits64` | 6,784 | 1,226 | 0 | 0 |
| `ValidUTF8Tokens/Strings` | 2,086 | 1,166 | 0 | 0 |
| `ValidUTF8Tokens/Escapes` | 5,680 | 451 | 0 | 0 |
| `ValidUTF8Tokens/Literals` | 3,466 | 591 | 0 | 0 |
| `ValidUTF8Tokens/Spaces` | 13,498 | 417 | 0 | 0 |
| `ValidUTF8Tokens/AtLimit` | 106,768 | 187 | 4,088 | 9 |
| `ValidUTF8Tokens/ToolResult` | 117,354 | 560 | 0 | 0 |
| `StringEnd` | 20.5 | 1,124 | 0 | 0 |
| `StringEndGenericForm` | 25.2 | 914 | 0 | 0 |
| `StrictEnd` | 28.3 | 814 | 0 | 0 |
| `StrictEndSharedForm` | 69.0 | 334 | 24 | 1 |
| `StrictEndGenericForm` | 30.9 | 744 | 0 | 0 |
| `WalkStrings` | 946 | 329 | 224 | 1 |
| `DecodeValue/String` | 1,118 | 83 | 840 | 13 |
| `DecodeValue/Bytes` | 1,236 | 75 | 936 | 14 |
| `BuilderAddValueElement` | 676 | 138 | 0 | 0 |
| `BuilderAddValueField` | 720 | 143 | 0 | 0 |

## Testing and gates

`make ci` runs every gate the workflows run; `make help` lists the targets.

```bash
make test       # the tests of the default, the purego and the 386 build, allocation counts included
make race       # the same under the race detector, which skips the counts, writing both coverage profiles
make cover      # race, then fails when either build covers less than 100% of the statements
make lint       # golangci-lint v2.14.0 on both builds
make deadcode   # deadcode -test on both builds
make vuln       # govulncheck
make bench      # every benchmark of both builds
make fuzz       # every fuzz target, FUZZTIME each (30s by default)
```

## Operational limits

| Parameter | Value | Rationale |
|-----------|-------|-----------|
| Builder pool | Builders whose buffer capacity is at most 256 KiB are kept | Bounds what the pool retains. |
| BatchWriter pool | writers whose buffer capacity is at most 4 MiB are kept | Bounds what the pool retains. |
| Validator depth | the caller's `maxDepth`; `MaxDepth` is 10000 | Constant stack: one bit per level, 256 levels inline and a spill that grows with the depth reached. |
| `IterateMembers`, `IterateObject` and `IterateDocument` name set | 32 names inline | More names use one map keyed by the name views, with no copy. |
| `IterateDocument` nested name sets | 2 inline, one for each object open inside a member's value | Deeper objects use heap stacks that grow with the depth. Decoded nested names take chunks of their own, apart from the member names, and give their room back when their object closes, so memory follows the objects open at once, not the document. A set reuses the map of the set closed at its level, cleared, when it held at most 1024 names; a larger map is dropped. |
| Decoded strings of `IterateStringArray` and `WalkStrings` | chunks that start at the first string to decode; a new chunk takes the rest of the document up to 4 KiB, twice the last chunk or the string it must hold, whichever is most | Each string is decoded in one pass. A document of up to 4 KiB whose strings hold valid UTF-8 decodes into one chunk; a larger one allocates in proportion to the strings it decodes, plus at most 4 KiB, not to its size. |
| `WalkTokens` name sets | 2 inline, one for each open object, 32 names each | Deeper objects use a heap stack that grows with the depth, and more names one map keyed by the name views. A set reuses the map of the set closed at its level, cleared, when it held at most 1024 names; a larger map is dropped. |
| Decoded names of `IterateMembers`, `IterateObject`, `IterateDocument`, `FlattenObject` and `WalkTokens` | the same chunks, for the names | A large object allocates in proportion to the names it decodes, plus at most 4 KiB (8 KiB for `IterateDocument`, whose nested names take chunks of their own, and for `FlattenObject`, whose names of objects do), not to its size. `FlattenObject` keeps its leaf names for the call; the names of an object inside a leaf give their room back when it closes, and those of the objects outside the leaves when the object that holds them closes. |
| `FlattenObject` depth | 64 levels, arrays included | Bounds the one walk that checks the object and writes its leaves. |
| `FlattenObject` leaf names | 32 names inline | More use one map keyed by the name views. |
| `FlattenObject` object names | 2 name sets inline, 32 names each | An object takes a set and a mark of where its names start once it holds an object or more than 32 leaves, and so does every object inside a leaf; more sets use a heap stack that grows with the depth, and the marks, one per level at most, stay inline. The next set at the same level reuses the map of the set closed there, cleared, when it held at most 1024 names; a larger map is dropped, since clearing a map takes time in proportion to its capacity. |
| `DecodeValue` open containers | 16 inline | Deeper nesting grows a heap stack, so the goroutine stack stays constant at any `maxDepth`. |
| Decoded strings of `DecodeValue` | the same chunks, for every string of a byte slice input and the escaped ones of a string input | A byte slice input may change once the call returns. |
| Value writers open containers | 16 frames and 32 keys inline, then heap stacks; past 16 frames, chained buckets, at least twice as many as the frames, find a container by identity | Constant goroutine stack at any depth. A container already open on its path is written as `null`, so a cycle ends where it closes, and so is one nested deeper than `MaxDepth`. |
| `EscapeString` | 512 bytes of output on the stack | A longer result allocates more than once. |

## Design principles

1. Zero allocation is a constraint, not a goal: every hot path asserts its allocation count.
2. Scanners walk the bytes and the Builder writes them, with no DOM between. `DecodeValue` is the
   one `map[string]any` decoder, for code that needs Go values, and the value writers the one
   encoder of the values it builds; encode any other Go value with `encoding/json` and splice the
   result in with `AppendRaw` or `AddRawJSONField`.
3. Parity with `encoding/json` wherever both define a result: the grammar at `MaxDepth`, string
   decoding and float text.
4. Deterministic output: keys are sorted wherever a caller may compare or cache the output.
5. No dependencies: pure Go, no cgo and no code generation.

## Project layout

```
go-jsonfast/
├── doc.go             # Package documentation
├── text.go            # Text and the zero-copy views
├── errors.go          # ErrMalformed and ErrDuplicateName
├── swar.go            # SWAR byte classes and runs
├── swar_unsafe.go     # One unaligned 8-byte load (amd64, arm64, ppc64le)
├── swar_purego.go     # encoding/binary load (other architectures or -tags=purego)
├── scan.go            # TrimWS and the number and string grammar
├── iterate.go         # Iterate*, Find* and WalkTokens
├── names.go           # The set of decoded member names
├── validate.go        # ValidUTF8, WalkStrings and MaxDepth
├── decode.go          # DecodeStringView, DecodeString and EqualString
├── scalar.go          # DecodeBool, DecodeInt64, DecodeUint64, DecodeFloat64, DecodeFloat32 and IsNumber
├── value.go           # DecodeValue
├── encode.go          # AddValueElement, AddValueField and the order of map keys
├── kind.go            # Kind and KindOf
├── builder.go         # Builder: buffer, containers, fields and elements
├── fieldkey.go        # FieldKey and the Add*FieldKey writers
├── escape.go          # AppendEscaped, AppendEscapedString and EscapeString
├── number.go          # Integer and float formatting
├── time.go            # RFC 3339 time formatting
├── flatten.go         # FlattenObject
├── pool.go            # Acquire, Release and Build
├── ndjson.go          # BatchWriter, AcquireBatchWriter and ReleaseBatchWriter
├── *_test.go          # One sibling per source file; doc_test.go holds shared fixtures
├── .golangci.yml      # Linter configuration
└── Makefile           # The gates, the benchmarks and the fuzz runs
```

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). For security issues, see [SECURITY.md](SECURITY.md).

## Versioning

We use [SemVer](https://semver.org/). Releases are tracked in the repository
[tags](https://github.com/ubyte-source/go-jsonfast/tags).

## Authors

- **Paolo Fabris** — [ubyte.it](https://ubyte.it/)

See also the list of [contributors](https://github.com/ubyte-source/go-jsonfast/contributors).

## License

MIT — see [LICENSE](LICENSE).

## Support

If go-jsonfast is useful for your pipelines, consider supporting the work:

[![Buy Me A Coffee](https://img.shields.io/badge/Buy%20Me%20A%20Coffee-Support-orange?style=for-the-badge&logo=buy-me-a-coffee)](https://coff.ee/ubyte)
