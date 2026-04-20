package jsonfast

import (
	"io"
	"math"
	"slices"
	"strconv"
	"sync"
	"time"
	"unsafe"
)

// Builder is a minimal JSON builder that operates on a reusable byte slice.
// It avoids allocations by appending directly into the buffer. Not a fully
// general-purpose JSON writer; tailored for known field sets.
//
// A Builder is not safe for concurrent use.
type Builder struct {
	buf   []byte
	first bool
}

// New creates a new Builder with the given initial capacity. A non-positive
// capacity is clamped to 256 bytes.
func New(capacity int) *Builder {
	if capacity <= 0 {
		capacity = 256
	}
	return &Builder{
		buf:   make([]byte, 0, capacity),
		first: true,
	}
}

// Reset clears the Builder for reuse. The underlying buffer is retained.
func (b *Builder) Reset() {
	b.buf = b.buf[:0]
	b.first = true
}

// Bytes returns the accumulated bytes. The returned slice aliases the
// internal buffer; do not modify it, and do not use it after Reset or
// Release.
func (b *Builder) Bytes() []byte {
	return b.buf
}

// Len returns the current byte length of the buffer.
func (b *Builder) Len() int {
	return len(b.buf)
}

// Grow ensures the buffer has at least n bytes of spare capacity. Uses
// slices.Grow which leverages the runtime's optimized growslice.
func (b *Builder) Grow(n int) {
	if cap(b.buf)-len(b.buf) < n {
		b.buf = slices.Grow(b.buf, n)
	}
}

// Write implements io.Writer. Bytes are appended unchanged to the
// buffer; Write never returns an error.
func (b *Builder) Write(p []byte) (int, error) {
	b.buf = append(b.buf, p...)
	return len(p), nil
}

// WriteTo implements io.WriterTo. The accumulated bytes are written to w
// in a single call. The Builder state is unchanged.
func (b *Builder) WriteTo(w io.Writer) (int64, error) {
	n, err := w.Write(b.buf)
	return int64(n), err
}

// BeginObject writes '{' and resets the field separator state.
func (b *Builder) BeginObject() {
	b.buf = append(b.buf, '{')
	b.first = true
}

// EndObject writes '}'.
func (b *Builder) EndObject() {
	b.buf = append(b.buf, '}')
}

// sep writes a comma before non-first fields.
func (b *Builder) sep() {
	if b.first {
		b.first = false
		return
	}
	b.buf = append(b.buf, ',')
}

// fieldKey writes the JSON field-key prefix, optionally preceded by the
// separator comma. name must be safe ASCII (no escaping required).
func (b *Builder) fieldKey(name string) {
	b.sep()
	b.buf = append(b.buf, '"')
	b.buf = append(b.buf, name...)
	b.buf = append(b.buf, '"', ':')
}

// AddStringField adds a "name":"value" field. The value is JSON-escaped.
func (b *Builder) AddStringField(name, value string) {
	b.fieldKey(name)
	b.buf = append(b.buf, '"')
	b.escapeString(value)
	b.buf = append(b.buf, '"')
}

// AddRawJSONField adds a "name":<raw json> field without escaping. The
// caller must ensure rawJSON is well-formed JSON.
func (b *Builder) AddRawJSONField(name string, rawJSON []byte) {
	b.fieldKey(name)
	b.buf = append(b.buf, rawJSON...)
}

// AddRawJSONFieldString is like AddRawJSONField but takes a string,
// avoiding the []byte conversion allocation at the call site.
func (b *Builder) AddRawJSONFieldString(name, rawJSON string) {
	b.fieldKey(name)
	b.buf = append(b.buf, rawJSON...)
}

// AddBoolField adds a "name":true/false field.
func (b *Builder) AddBoolField(name string, v bool) {
	b.fieldKey(name)
	if v {
		b.buf = append(b.buf, "true"...)
	} else {
		b.buf = append(b.buf, "false"...)
	}
}

// AddNullField adds a "name":null field.
func (b *Builder) AddNullField(name string) {
	b.fieldKey(name)
	b.buf = append(b.buf, "null"...)
}

// AddIntField adds a "name":<int> field.
func (b *Builder) AddIntField(name string, v int) {
	b.fieldKey(name)
	b.appendInt(v)
}

// AddInt64Field adds a "name":<int64> field.
func (b *Builder) AddInt64Field(name string, v int64) {
	b.fieldKey(name)
	if v < 0 {
		b.buf = append(b.buf, '-')
		b.appendUint(absInt64AsUint64(v))
		return
	}
	b.appendUint(uint64(v))
}

// AddUint8Field adds a "name":<uint8> field.
func (b *Builder) AddUint8Field(name string, v uint8) {
	b.fieldKey(name)
	b.appendUint(uint64(v))
}

// AddUint16Field adds a "name":<uint16> field.
func (b *Builder) AddUint16Field(name string, v uint16) {
	b.fieldKey(name)
	b.appendUint(uint64(v))
}

// AddUint32Field adds a "name":<uint32> field.
func (b *Builder) AddUint32Field(name string, v uint32) {
	b.fieldKey(name)
	b.appendUint(uint64(v))
}

// AddUint64Field adds a "name":<uint64> field.
func (b *Builder) AddUint64Field(name string, v uint64) {
	b.fieldKey(name)
	b.appendUint(v)
}

// AddFloat64Field adds a "name":<float64> field. NaN and ±Inf are emitted
// as null because JSON (RFC 8259) does not represent non-finite numbers.
// Integer values are emitted without a decimal point; fractional values
// use strconv.AppendFloat with full precision.
func (b *Builder) AddFloat64Field(name string, v float64) {
	b.fieldKey(name)
	b.appendFloat64(v)
}

// AddTimeRFC3339Field adds a "name":"<RFC3339>" field. The output is
// always in UTC regardless of the input timezone because computation
// uses t.Unix() (timezone-independent). Negative timestamps are clamped
// to the epoch; years beyond 9999 are clamped to 9999.
func (b *Builder) AddTimeRFC3339Field(name string, t time.Time) {
	b.fieldKey(name)
	b.appendTimeRFC3339(t)
}

// AppendRaw appends raw bytes to the buffer without escaping or framing.
func (b *Builder) AppendRaw(p []byte) {
	b.buf = append(b.buf, p...)
}

// AppendRawString appends a raw string to the buffer without escaping or
// framing.
func (b *Builder) AppendRawString(s string) {
	b.buf = append(b.buf, s...)
}

// AppendEscapedString appends s with JSON special characters escaped
// directly into the buffer (no enclosing quotes).
func (b *Builder) AppendEscapedString(s string) {
	b.escapeString(s)
}

// AddRawBytesField adds a "name":<value> field where name is raw bytes
// (no quoting or escaping is performed) and value is raw JSON bytes.
func (b *Builder) AddRawBytesField(name, value []byte) {
	b.sep()
	b.buf = append(b.buf, '"')
	b.buf = append(b.buf, name...)
	b.buf = append(b.buf, '"', ':')
	b.buf = append(b.buf, value...)
}

// AddStringMapObject writes m as a JSON object {...} at the current
// position. Keys are sorted for deterministic output. If rawJSONKey is
// non-empty, values for that key are embedded as raw JSON when they pass
// structural validation via IsStructuralJSON. This method does not add a
// field name — it writes just the object.
//
// Zero allocation for maps with at most 8 keys (stack-allocated sort buffer).
func (b *Builder) AddStringMapObject(m map[string]string, rawJSONKey string) {
	b.BeginObject()
	var stack [8]string
	var keys []string
	if len(m) <= len(stack) {
		keys = stack[:0]
	} else {
		keys = make([]string, 0, len(m))
	}
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		v := m[k]
		if rawJSONKey != "" && k == rawJSONKey && IsStructuralJSON(v) {
			b.AddRawJSONFieldString(k, v)
			continue
		}
		b.AddStringField(k, v)
	}
	b.EndObject()
}

// AddNestedStringMapField adds a "name":{outer:{inner:"v",...},...} field
// with keys sorted at both levels for deterministic output.
//
// Zero allocation when every map (outer and each inner) has at most 8
// keys; larger maps fall back to one heap allocation per oversized map
// for the sort buffer.
func (b *Builder) AddNestedStringMapField(name string, m map[string]map[string]string) {
	if len(m) == 0 {
		return
	}
	b.fieldKey(name)
	b.buf = append(b.buf, '{')

	var outerStack [8]string
	var outerKeys []string
	if len(m) <= len(outerStack) {
		outerKeys = outerStack[:0]
	} else {
		outerKeys = make([]string, 0, len(m))
	}
	for k := range m {
		outerKeys = append(outerKeys, k)
	}
	slices.Sort(outerKeys)

	for i, outerKey := range outerKeys {
		if i > 0 {
			b.buf = append(b.buf, ',')
		}
		b.buf = append(b.buf, '"')
		b.escapeString(outerKey)
		b.buf = append(b.buf, '"', ':', '{')
		b.writeInnerMap(m[outerKey])
		b.buf = append(b.buf, '}')
	}

	b.buf = append(b.buf, '}')
}

// writeInnerMap writes a sorted map[string]string as key-value pairs.
// Zero allocation when len(m) ≤ 8.
func (b *Builder) writeInnerMap(m map[string]string) {
	var stack [8]string
	var keys []string
	if len(m) <= len(stack) {
		keys = stack[:0]
	} else {
		keys = make([]string, 0, len(m))
	}
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for i, k := range keys {
		if i > 0 {
			b.buf = append(b.buf, ',')
		}
		b.buf = append(b.buf, '"')
		b.escapeString(k)
		b.buf = append(b.buf, '"', ':', '"')
		b.escapeString(m[k])
		b.buf = append(b.buf, '"')
	}
}

// FieldKey is a pre-computed JSON field key prefix that stores `,"name":`.
// The non-comma form is obtained by slicing the first byte. Instances
// should be constructed via NewFieldKey, or as typed string literals
// matching the documented layout (`,"name":` where name is safe ASCII).
type FieldKey string

// NewFieldKey returns a pre-computed key for the given name. The name
// must be safe ASCII (no escaping required). Call at init time, not on
// the hot path; the returned value can be stored in a package-level var.
func NewFieldKey(name string) FieldKey {
	return FieldKey(`,"` + name + `":`)
}

// precomputedKey writes the pre-computed field key prefix.
func (b *Builder) precomputedKey(k FieldKey) {
	if b.first {
		b.first = false
		b.buf = append(b.buf, k[1:]...) // skip leading comma
		return
	}
	b.buf = append(b.buf, k...)
}

// AddStringFieldKey adds a "name":"value" field using a pre-computed key.
func (b *Builder) AddStringFieldKey(k FieldKey, value string) {
	b.precomputedKey(k)
	b.buf = append(b.buf, '"')
	b.escapeString(value)
	b.buf = append(b.buf, '"')
}

// AddIntFieldKey adds a "name":<int> field using a pre-computed key.
func (b *Builder) AddIntFieldKey(k FieldKey, v int) {
	b.precomputedKey(k)
	b.appendInt(v)
}

// AddInt64FieldKey adds a "name":<int64> field using a pre-computed key.
func (b *Builder) AddInt64FieldKey(k FieldKey, v int64) {
	b.precomputedKey(k)
	if v < 0 {
		b.buf = append(b.buf, '-')
		b.appendUint(absInt64AsUint64(v))
		return
	}
	b.appendUint(uint64(v))
}

// AddUint64FieldKey adds a "name":<uint64> field using a pre-computed key.
func (b *Builder) AddUint64FieldKey(k FieldKey, v uint64) {
	b.precomputedKey(k)
	b.appendUint(v)
}

// AddBoolFieldKey adds a "name":true/false field using a pre-computed key.
func (b *Builder) AddBoolFieldKey(k FieldKey, v bool) {
	b.precomputedKey(k)
	if v {
		b.buf = append(b.buf, "true"...)
	} else {
		b.buf = append(b.buf, "false"...)
	}
}

// AddRawJSONFieldKey adds a "name":<raw json> field using a pre-computed key.
func (b *Builder) AddRawJSONFieldKey(k FieldKey, rawJSON []byte) {
	b.precomputedKey(k)
	b.buf = append(b.buf, rawJSON...)
}

// AddRawJSONFieldKeyString adds a "name":<raw json> field using a
// pre-computed key, taking the raw JSON as a string.
func (b *Builder) AddRawJSONFieldKeyString(k FieldKey, rawJSON string) {
	b.precomputedKey(k)
	b.buf = append(b.buf, rawJSON...)
}

// AddTimeRFC3339FieldKey adds a "name":"<RFC3339>" field in UTC using a
// pre-computed key. See AddTimeRFC3339Field for clamping semantics.
func (b *Builder) AddTimeRFC3339FieldKey(k FieldKey, t time.Time) {
	b.precomputedKey(k)
	b.appendTimeRFC3339(t)
}

// AddFloat64FieldKey adds a "name":<float64> field using a pre-computed
// key. See AddFloat64Field for NaN/Inf handling.
func (b *Builder) AddFloat64FieldKey(k FieldKey, v float64) {
	b.precomputedKey(k)
	b.appendFloat64(v)
}

// AddNullFieldKey adds a "name":null field using a pre-computed key.
func (b *Builder) AddNullFieldKey(k FieldKey) {
	b.precomputedKey(k)
	b.buf = append(b.buf, "null"...)
}

// ---------------------------------------------------------------------------
// String escaping
// ---------------------------------------------------------------------------

// safeASCII reports whether a byte is safe inside a JSON string without
// escaping: printable ASCII in [0x20, 0x7F) excluding '"' and '\\'.
var safeASCII = buildSafeASCII()

func buildSafeASCII() [256]bool {
	var t [256]bool
	for c := 0x20; c < 0x80; c++ {
		if c != '"' && c != '\\' {
			t[c] = true
		}
	}
	return t
}

// escapeString escapes JSON special characters and validates UTF-8.
// Invalid UTF-8 bytes are replaced with U+FFFD per RFC 8259 §8.
func (b *Builder) escapeString(s string) {
	// Fast path: short strings are classified by table lookup; the SWAR
	// setup cost is not amortized below 33 bytes.
	if len(s) <= 32 {
		for i := 0; i < len(s); i++ {
			if !safeASCII[s[i]] {
				b.escapeSlow(s, i)
				return
			}
		}
		b.buf = append(b.buf, s...)
		return
	}
	b.escapeSlow(s, 0)
}

// escapeSlow is the full escape path. The already-verified prefix s[:start]
// is emitted as a single append, then SWAR scanning and per-byte emission
// handle the remainder.
func (b *Builder) escapeSlow(s string, start int) {
	if start > 0 {
		b.buf = append(b.buf, s[:start]...)
	}
	i := start
	for i < len(s) {
		j := swarScanSafe(s, i)
		if j > i {
			b.buf = append(b.buf, s[i:j]...)
			i = j
			continue
		}
		c := s[i]
		if c < 0x80 {
			b.escapeASCIIByte(c)
			i++
			continue
		}
		i += b.escapeMultiByte(s, i)
	}
}

// swarScanSafe returns the index of the first byte at or after i that
// requires JSON escaping (or len(s) if all bytes are safe). Uses word-
// at-a-time SWAR for the 8-byte-aligned main run and a byte table for
// the tail.
//
//nolint:gosec // unsafe pointer math is load-bearing for SWAR throughput
func swarScanSafe(s string, i int) int {
	n := len(s)
	j := i
	if n-j >= 8 {
		p := unsafe.Pointer(unsafe.StringData(s))
		for j+8 <= n {
			w := *(*uint64)(unsafe.Add(p, j))
			if swarSpecialEscape(w) != 0 {
				break
			}
			j += 8
		}
	}
	for j < n && safeASCII[s[j]] {
		j++
	}
	return j
}

// escapeASCIIByte writes the JSON escape sequence for a single ASCII byte
// that requires escaping.
func (b *Builder) escapeASCIIByte(c byte) {
	if e := shortEscape[c]; e != 0 {
		b.buf = append(b.buf, '\\', e)
		return
	}
	b.buf = append(b.buf, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0x0f])
}

// shortEscape maps bytes to their single-char JSON escape character.
// Non-zero entries mean the byte is escaped as \X where X is the value.
var shortEscape = [256]byte{
	'"':  '"',
	'\\': '\\',
	'\b': 'b',
	'\f': 'f',
	'\n': 'n',
	'\r': 'r',
	'\t': 't',
}

// hexDigits maps a nibble [0..15] to its hex character. Using a fixed
// [16]byte eliminates bounds checks since c>>4 and c&0x0f are always <16.
var hexDigits = [16]byte{
	'0', '1', '2', '3', '4', '5', '6', '7',
	'8', '9', 'a', 'b', 'c', 'd', 'e', 'f',
}

// escapeMultiByte validates and writes a multi-byte UTF-8 sequence.
// Rejects overlong encodings, surrogate halves (U+D800–U+DFFF), and
// codepoints above U+10FFFF per RFC 8259 §8. Returns the number of
// bytes consumed.
func (b *Builder) escapeMultiByte(s string, i int) int {
	size := utf8SeqLen(s[i])
	if size == 0 || i+size > len(s) || !validContinuation(s, i, size) {
		b.buf = append(b.buf, 0xEF, 0xBF, 0xBD) // U+FFFD
		return 1
	}
	if !validCodepoint(s, i, size) {
		b.buf = append(b.buf, 0xEF, 0xBF, 0xBD) // U+FFFD
		return size
	}
	b.buf = append(b.buf, s[i:i+size]...)
	return size
}

// validCodepoint checks for overlong encodings, surrogates, and out-of-
// range codepoints. Called after validContinuation confirms byte-level
// structure.
func validCodepoint(s string, i, size int) bool {
	_ = s[i+size-1] // BCE hint: caller guarantees i+size <= len(s)
	switch size {
	case 3:
		_ = s[i+2] // BCE hint for the three accesses below
		cp := rune(s[i]&0x0F)<<12 | rune(s[i+1]&0x3F)<<6 | rune(s[i+2]&0x3F)
		return cp >= 0x0800 && (cp < 0xD800 || cp > 0xDFFF)
	case 4:
		_ = s[i+3] // BCE hint for the four accesses below
		cp := rune(s[i]&0x07)<<18 | rune(s[i+1]&0x3F)<<12 |
			rune(s[i+2]&0x3F)<<6 | rune(s[i+3]&0x3F)
		return cp >= 0x10000 && cp <= 0x10FFFF
	}
	return true
}

// utf8SeqLen returns the expected byte length for a UTF-8 leading byte,
// or 0 if invalid. Rejects overlong 2-byte leaders (0xC0-0xC1) and
// out-of-range 4-byte leaders (0xF5+).
func utf8SeqLen(c byte) int {
	switch {
	case c >= 0xC2 && c <= 0xDF: // 2-byte; reject 0xC0-0xC1 (overlong ASCII)
		return 2
	case c&0xF0 == 0xE0: // 3-byte (0xE0-0xEF)
		return 3
	case c >= 0xF0 && c <= 0xF4: // 4-byte; reject 0xF5+ (> U+10FFFF)
		return 4
	default:
		return 0
	}
}

// validContinuation checks that bytes s[i+1..i+size-1] are valid
// continuation bytes (10xxxxxx).
func validContinuation(s string, i, size int) bool {
	_ = s[i+size-1] // BCE hint: caller guarantees i+size <= len(s)
	switch size {
	case 2:
		return s[i+1]&0xC0 == 0x80
	case 3:
		return s[i+1]&0xC0 == 0x80 && s[i+2]&0xC0 == 0x80
	case 4:
		return s[i+1]&0xC0 == 0x80 && s[i+2]&0xC0 == 0x80 && s[i+3]&0xC0 == 0x80
	}
	return false
}

// EscapeString returns s with JSON special characters escaped per
// RFC 8259. If no escaping is needed (pure safe ASCII), s is returned
// unchanged (zero allocation). Invalid UTF-8 bytes are replaced with
// U+FFFD, matching the escape behavior used by Builder field methods.
func EscapeString(s string) string {
	if swarScanSafe(s, 0) == len(s) {
		return s
	}
	var b Builder
	var stackBuf [512]byte
	if len(s)+6 <= len(stackBuf) {
		b.buf = stackBuf[:0]
	} else {
		b.buf = make([]byte, 0, len(s)+6)
	}
	b.escapeString(s)
	return string(b.buf)
}

// ---------------------------------------------------------------------------
// Integer formatting
// ---------------------------------------------------------------------------

// digitPairs provides two-character representations for values 00–99.
// Used by appendInt/appendUint/appendNano to halve the number of
// divisions. Sized at 256 (not 200) so index expressions masked with
// &0x7F are provably in-bounds, letting the compiler eliminate bounds
// checks.
var digitPairs [256]byte

func init() {
	for i := range 100 {
		digitPairs[i*2] = byte('0' + i/10)
		digitPairs[i*2+1] = byte('0' + i%10)
	}
}

// appendInt writes an integer into the builder's buffer. Fast path for
// values in [0, 99].
func (b *Builder) appendInt(x int) {
	if x >= 0 && x < 100 {
		if x < 10 {
			b.buf = append(b.buf, byte('0'+x))
			return
		}
		b.buf = append(b.buf, digitPairs[x*2], digitPairs[x*2+1])
		return
	}
	if x < 0 {
		b.buf = append(b.buf, '-')
		b.appendUint(absInt64AsUint64(int64(x)))
		return
	}
	b.appendUint(uint64(x))
}

// appendUint writes an unsigned integer using the digit-pair table.
// Fast paths for values < 10, < 100, and < 1000 avoid the division loop.
func (b *Builder) appendUint(x uint64) {
	if x < 100 {
		if x < 10 {
			b.buf = append(b.buf, byte('0'+x))
			return
		}
		b.buf = append(b.buf, digitPairs[x*2], digitPairs[x*2+1])
		return
	}
	if x < 1000 {
		d := x / 100
		r := (x % 100) & 0x7F // mask forces r*2+1 < 256 at BCE
		b.buf = append(b.buf, byte('0'+d), digitPairs[r*2], digitPairs[r*2+1])
		return
	}
	var tmp [20]byte
	i := len(tmp)
	for x >= 100 {
		j := (x % 100) & 0x7F // mask forces j*2+1 < 256
		x /= 100
		i -= 2
		tmp[i] = digitPairs[j*2]
		tmp[i+1] = digitPairs[j*2+1]
	}
	if x >= 10 {
		x &= 0x7F // mask forces x*2+1 < 256
		i -= 2
		tmp[i] = digitPairs[x*2]
		tmp[i+1] = digitPairs[x*2+1]
	} else {
		i--
		tmp[i] = byte('0' + x)
	}
	b.buf = append(b.buf, tmp[i:]...)
}

// appendFloat64 writes a float64. NaN and ±Inf are emitted as null.
// Integer values in (-1e18, 1e18) are emitted without a decimal point;
// fractional values use strconv.AppendFloat with full precision.
func (b *Builder) appendFloat64(v float64) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		b.buf = append(b.buf, "null"...)
		return
	}
	if v == float64(int64(v)) && v < 1e18 && v > -1e18 {
		if v < 0 {
			b.buf = append(b.buf, '-')
			b.appendUint(uint64(-v))
			return
		}
		b.appendUint(uint64(v))
		return
	}
	b.buf = strconv.AppendFloat(b.buf, v, 'f', -1, 64)
}

// absInt64AsUint64 returns the absolute value of v as a uint64. Handles
// math.MinInt64 correctly: -(MinInt64+1) is MaxInt63, then +1 gives 2^63.
func absInt64AsUint64(v int64) uint64 {
	if v >= 0 {
		return uint64(v)
	}
	return uint64(-(v + 1)) + 1 //nolint:gosec // intentional: avoids MinInt64 overflow
}

// ---------------------------------------------------------------------------
// RFC 3339 time formatting
// ---------------------------------------------------------------------------

// civilDate converts a Unix timestamp (seconds since epoch) to year,
// month and day via the days-from-civil algorithm. Negative timestamps
// are clamped to zero; years > 9999 are clamped to 9999.
func civilDate(sec int64) (year, month, day int) {
	sec = max(sec, 0)
	// sec ≥ 0 guarantees days ≥ 0 and z ≥ 719468, so the negative-z
	// correction of the algorithm is unreachable and omitted.
	days := sec / 86400
	z := days + 719468
	era := z / 146097
	doe := z - era*146097
	yoe := (doe - doe/1460 + doe/36524 - doe/146096) / 365
	y := yoe + era*400
	doy := doe - (365*yoe + yoe/4 - yoe/100)
	mp := (5*doy + 2) / 153
	d := doy - (153*mp+2)/5 + 1
	m := mp
	if mp < 10 {
		m += 3
	} else {
		m -= 9
	}
	if m <= 2 {
		y++
	}
	year = int(y)
	year = max(year, 0)
	year = min(year, 9999)
	month = int(m)
	day = int(d)
	return year, month, day
}

// appendTimeRFC3339 writes "YYYY-MM-DDThh:mm:ss[.nnnnnnnnn]Z" into the
// buffer. Shared by AddTimeRFC3339Field and AddTimeRFC3339FieldKey.
func (b *Builder) appendTimeRFC3339(t time.Time) {
	b.buf = append(b.buf, '"')

	unix := t.Unix()
	sec := max(unix, 0)
	daySeconds := sec % 86400
	hour := int(daySeconds / 3600)
	minute := int(daySeconds % 3600 / 60)
	secs := int(daySeconds % 60)

	year, month, day := civilDate(unix)

	// Mask all values to &0x7F so the compiler can prove *2+1 < 256,
	// eliminating all bounds checks against the [256]byte digitPairs table.
	yc := (year / 100) & 0x7F
	yr := (year % 100) & 0x7F
	mon := month & 0x7F
	d := day & 0x7F
	h := hour & 0x7F
	mn := minute & 0x7F
	s := secs & 0x7F

	b.buf = append(b.buf,
		digitPairs[yc*2], digitPairs[yc*2+1],
		digitPairs[yr*2], digitPairs[yr*2+1], '-',
		digitPairs[mon*2], digitPairs[mon*2+1], '-',
		digitPairs[d*2], digitPairs[d*2+1], 'T',
		digitPairs[h*2], digitPairs[h*2+1], ':',
		digitPairs[mn*2], digitPairs[mn*2+1], ':',
		digitPairs[s*2], digitPairs[s*2+1],
	)

	if ns := t.Nanosecond(); ns > 0 {
		b.buf = append(b.buf, '.')
		b.appendNano(ns)
	}
	b.buf = append(b.buf, 'Z', '"')
}

// appendNano writes a nanosecond value with trailing zeros trimmed.
// Uses digit pairs to halve the number of divisions.
func (b *Builder) appendNano(v int) {
	dotPos := len(b.buf)
	d01 := (v / 10000000) & 0x7F
	rem := v % 10000000
	d23 := (rem / 100000) & 0x7F
	rem %= 100000
	d45 := (rem / 1000) & 0x7F
	rem %= 1000
	d67 := (rem / 10) & 0x7F
	d8 := rem % 10

	b.buf = append(b.buf,
		digitPairs[d01*2], digitPairs[d01*2+1],
		digitPairs[d23*2], digitPairs[d23*2+1],
		digitPairs[d45*2], digitPairs[d45*2+1],
		digitPairs[d67*2], digitPairs[d67*2+1],
		byte('0'+d8),
	)
	end := len(b.buf)
	for end > dotPos && b.buf[end-1] == '0' {
		end--
	}
	b.buf = b.buf[:end]
}

// ---------------------------------------------------------------------------
// Builder pool
// ---------------------------------------------------------------------------

// poolBufferSize is the default capacity of pooled buffers.
const poolBufferSize = 2048

// poolMaxRetain is the largest buffer capacity returned to the pool;
// larger buffers are discarded to bound per-instance memory retention.
const poolMaxRetain = 1 << 18 // 256 KB

// pool is a sync.Pool for reusing Builder instances.
var pool = sync.Pool{
	New: func() any {
		return &Builder{
			buf:   make([]byte, 0, poolBufferSize),
			first: true,
		}
	},
}

// Acquire returns a Builder from the pool, ready for use. Call Release
// when done to return it for reuse.
func Acquire() *Builder {
	b, ok := pool.Get().(*Builder)
	if !ok {
		return New(poolBufferSize)
	}
	b.Reset()
	return b
}

// Release returns the Builder to the pool for reuse. The Builder must
// not be used after calling Release. Buffers larger than 256 KB are
// discarded to bound memory retention.
func Release(b *Builder) {
	if b == nil {
		return
	}
	if cap(b.buf) <= poolMaxRetain {
		pool.Put(b)
	}
}

// WarmPool pre-allocates n Builder instances and returns them to the
// pool. Use before entering the hot path to smooth tail latency during
// warm-up. Values of n ≤ 0 are a no-op.
func WarmPool(n int) {
	if n <= 0 {
		return
	}
	builders := make([]*Builder, n)
	for i := range builders {
		builders[i] = &Builder{
			buf:   make([]byte, 0, poolBufferSize),
			first: true,
		}
	}
	for _, b := range builders {
		pool.Put(b)
	}
}
