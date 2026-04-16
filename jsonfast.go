package jsonfast

import (
	"slices"
	"strconv"
	"sync"
	"time"
	"unsafe"
)

// Builder is a minimal JSON builder that operates on a reusable byte slice.
// It avoids allocations by appending directly into the buffer.
// Not a fully general-purpose JSON writer; tailored for known field sets.
type Builder struct {
	buf   []byte
	first bool
}

// New creates a new builder with initial capacity.
func New(capacity int) *Builder {
	if capacity <= 0 {
		capacity = 256
	}
	return &Builder{
		buf:   make([]byte, 0, capacity),
		first: true,
	}
}

// Reset clears the builder for reuse.
func (b *Builder) Reset() {
	b.buf = b.buf[:0]
	b.first = true
}

// Bytes returns the underlying buffer (do not modify after use).
func (b *Builder) Bytes() []byte {
	return b.buf
}

// Len returns the current length of the builder's buffer.
// Useful for pre-flight capacity checks without calling Bytes().
func (b *Builder) Len() int {
	return len(b.buf)
}

// Grow ensures the buffer has at least n bytes of spare capacity.
// Uses slices.Grow which leverages the runtime's optimized growslice,
// rounding up to size-classes and benefiting from PGO.
func (b *Builder) Grow(n int) {
	if cap(b.buf)-len(b.buf) < n {
		b.buf = slices.Grow(b.buf, n)
	}
}

// BeginObject starts a JSON object.
func (b *Builder) BeginObject() {
	b.buf = append(b.buf, '{')
	b.first = true
}

// EndObject ends a JSON object.
func (b *Builder) EndObject() {
	b.buf = append(b.buf, '}')
}

// AddStringField adds a "name":"value" string field with escaping.
func (b *Builder) AddStringField(name, value string) {
	b.fieldKey(name)
	b.buf = append(b.buf, '"')
	b.escapeString(value)
	b.buf = append(b.buf, '"')
}

// AddRawJSONField adds a "name":<raw json> field without escaping.
// The value must be valid JSON.
func (b *Builder) AddRawJSONField(name string, rawJSON []byte) {
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

// AppendRaw appends raw bytes to the buffer without any escaping or framing.
func (b *Builder) AppendRaw(p []byte) {
	b.buf = append(b.buf, p...)
}

// AppendRawString appends a raw string to the buffer without any escaping or framing.
func (b *Builder) AppendRawString(s string) {
	b.buf = append(b.buf, s...)
}

// AppendEscapedString appends s with JSON special characters escaped directly
// into the builder's buffer. Unlike EscapeString, this avoids creating a
// temporary Builder and string allocation — zero-alloc on the hot path.
func (b *Builder) AppendEscapedString(s string) {
	b.escapeString(s)
}

// AddRawJSONFieldString adds a "name":<raw json> field where the raw JSON source is a string.
// Avoids the []byte(s) allocation of AddRawJSONField.
func (b *Builder) AddRawJSONFieldString(name, rawJSON string) {
	b.fieldKey(name)
	b.buf = append(b.buf, rawJSON...)
}

// AddIntField adds a "name":int field.
func (b *Builder) AddIntField(name string, v int) {
	b.fieldKey(name)
	b.appendInt(v)
}

// AddInt64Field adds a "name":int64 field.
func (b *Builder) AddInt64Field(name string, v int64) {
	b.fieldKey(name)
	if v < 0 {
		b.buf = append(b.buf, '-')
		b.appendUint(absInt64AsUint64(v))
	} else {
		b.appendUint(uint64(v))
	}
}

// AddUint8Field adds a "name":uint8 field without heap allocation.
func (b *Builder) AddUint8Field(name string, v uint8) {
	b.fieldKey(name)
	b.appendUint(uint64(v))
}

// AddUint16Field adds a "name":uint16 field without heap allocation.
func (b *Builder) AddUint16Field(name string, v uint16) {
	b.fieldKey(name)
	b.appendUint(uint64(v))
}

// AddUint32Field adds a "name":uint32 field without heap allocation.
func (b *Builder) AddUint32Field(name string, v uint32) {
	b.fieldKey(name)
	b.appendUint(uint64(v))
}

// AddUint64Field adds a "name":uint64 field without heap allocation.
func (b *Builder) AddUint64Field(name string, v uint64) {
	b.fieldKey(name)
	b.appendUint(v)
}

// AddFloat64Field adds a "name":float64 field.
// Integer values are written without decimal point.
// Fractional values use strconv.AppendFloat with full precision.
func (b *Builder) AddFloat64Field(name string, v float64) {
	b.fieldKey(name)
	b.appendFloat64(v)
}

// AddNestedStringMapField adds a "name":{"key1":{"k":"v"},"key2":{...}} field.
// Specifically handles map[string]map[string]string as found in RFC5424 structured data.
// Keys are sorted to produce deterministic JSON output for caching and deduplication.
func (b *Builder) AddNestedStringMapField(name string, m map[string]map[string]string) {
	if len(m) == 0 {
		return
	}
	b.fieldKey(name)
	b.buf = append(b.buf, '{')

	outerKeys := sortedMapKeys(m)

	firstOuter := true
	for _, outerKey := range outerKeys {
		innerMap := m[outerKey]
		if !firstOuter {
			b.buf = append(b.buf, ',')
		}
		firstOuter = false
		b.buf = append(b.buf, '"')
		b.escapeString(outerKey)
		b.buf = append(b.buf, '"', ':', '{')
		b.writeInnerMap(innerMap)
		b.buf = append(b.buf, '}')
	}

	b.buf = append(b.buf, '}')
}

// sortedMapKeys returns the keys of a map sorted in ascending order.
// Uses a stack-allocated array for small maps (≤8 keys).
func sortedMapKeys[V any](m map[string]V) []string {
	var keys []string
	var buf [8]string
	if len(m) <= len(buf) {
		keys = buf[:0]
	} else {
		keys = make([]string, 0, len(m))
	}
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// writeInnerMap writes a sorted map[string]string as JSON key-value pairs.
func (b *Builder) writeInnerMap(m map[string]string) {
	keys := sortedMapKeys(m)
	for i, innerKey := range keys {
		if i > 0 {
			b.buf = append(b.buf, ',')
		}
		b.buf = append(b.buf, '"')
		b.escapeString(innerKey)
		b.buf = append(b.buf, '"', ':', '"')
		b.escapeString(m[innerKey])
		b.buf = append(b.buf, '"')
	}
}

// sep writes a comma separator before non-first fields.
func (b *Builder) sep() {
	if b.first {
		b.first = false
		return
	}
	b.buf = append(b.buf, ',')
}

// FieldKey is a pre-computed JSON field key prefix.
// Stores `,"name":` as a single string; the non-comma variant is a substring.
// Total size: 16 bytes (one string header), same as passing a string argument.
//
// Usage:
//
//	var keyMessage = jsonfast.NewFieldKey("message")
//	b.AddStringFieldKey(keyMessage, "hello")
type FieldKey struct {
	s string // `,"name":`  — comma-prefixed; s[1:] gives `"name":`
}

// NewFieldKey creates a pre-computed field key for the given name.
// The name must be a safe ASCII string (no escaping needed).
// Call this at init time, not on the hot path.
func NewFieldKey(name string) FieldKey {
	return FieldKey{s: `,"` + name + `":`}
}

// precomputedKey writes the pre-computed field key prefix.
// Single append of the entire prefix string eliminates per-byte overhead.
func (b *Builder) precomputedKey(k FieldKey) {
	if b.first {
		b.first = false
		b.buf = append(b.buf, k.s[1:]...) // skip leading comma
	} else {
		b.buf = append(b.buf, k.s...)
	}
}

// AddStringFieldKey adds a "name":"value" field using a pre-computed key.
// Eliminates the quoting overhead of fieldKey on every call.
func (b *Builder) AddStringFieldKey(k FieldKey, value string) {
	b.precomputedKey(k)
	b.buf = append(b.buf, '"')
	b.escapeString(value)
	b.buf = append(b.buf, '"')
}

// AddIntFieldKey adds a "name":int field using a pre-computed key.
func (b *Builder) AddIntFieldKey(k FieldKey, v int) {
	b.precomputedKey(k)
	b.appendInt(v)
}

// AddInt64FieldKey adds a "name":int64 field using a pre-computed key.
func (b *Builder) AddInt64FieldKey(k FieldKey, v int64) {
	b.precomputedKey(k)
	if v < 0 {
		b.buf = append(b.buf, '-')
		b.appendUint(absInt64AsUint64(v))
	} else {
		b.appendUint(uint64(v))
	}
}

// AddUint64FieldKey adds a "name":uint64 field using a pre-computed key.
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

// AddRawJSONFieldKeyString adds a "name":<raw json string> field using a pre-computed key.
func (b *Builder) AddRawJSONFieldKeyString(k FieldKey, rawJSON string) {
	b.precomputedKey(k)
	b.buf = append(b.buf, rawJSON...)
}

// AddTimeRFC3339FieldKey adds a "name":"RFC3339" field using a pre-computed key.
func (b *Builder) AddTimeRFC3339FieldKey(k FieldKey, t time.Time) {
	b.precomputedKey(k)
	b.appendTimeRFC3339(t)
}

// AddFloat64FieldKey adds a "name":float64 field using a pre-computed key.
func (b *Builder) AddFloat64FieldKey(k FieldKey, v float64) {
	b.precomputedKey(k)
	b.appendFloat64(v)
}

// AddNullFieldKey adds a "name":null field using a pre-computed key.
func (b *Builder) AddNullFieldKey(k FieldKey) {
	b.precomputedKey(k)
	b.buf = append(b.buf, "null"...)
}

// fieldKey writes the JSON field-key prefix: ,"name":
// Called by all Add*Field methods to avoid repeating the 4-line preamble.
func (b *Builder) fieldKey(name string) {
	b.sep()
	b.buf = append(b.buf, '"')
	b.buf = append(b.buf, name...)
	b.buf = append(b.buf, '"', ':')
}

// swarScanSafe scans s starting at offset i and returns the index of the
// first byte that requires escaping (or len(s) if all bytes are safe).
// Uses swarSpecialEscape (see swar.go) for the detection formula.
//
//nolint:gosec,cyclop // unsafe for SWAR throughput; complexity from unrolled SWAR loop
func swarScanSafe(s string, i int) int {
	n := len(s)
	j := i
	if n-j >= 8 {
		p := unsafe.Pointer(unsafe.StringData(s))
		for j+16 <= n {
			w1 := *(*uint64)(unsafe.Add(p, j))
			if swarSpecialEscape(w1) != 0 {
				break
			}
			w2 := *(*uint64)(unsafe.Add(p, j+8))
			if swarSpecialEscape(w2) != 0 {
				j += 8
				break
			}
			j += 16
		}
		for j+8 <= n {
			w := *(*uint64)(unsafe.Add(p, j))
			if swarSpecialEscape(w) != 0 {
				break
			}
			j += 8
		}
	}
	for j < n && s[j] >= 0x20 && s[j] < 0x80 && s[j] != '"' && s[j] != '\\' {
		j++
	}
	return j
}

// escapeString escapes JSON special characters and validates UTF-8.
// Invalid UTF-8 → U+FFFD. Uses SWAR scanning for maximum throughput.
func (b *Builder) escapeString(s string) {
	// Fast path: short, pure-safe-ASCII strings bypass the non-inlineable
	// swarScanSafe call. Covers typical syslog field values (hostname, IP,
	// short hostnames like "webserver-prod-01" which are 17+ chars).
	if len(s) <= 32 {
		j := 0
		for j < len(s) {
			c := s[j]
			if c < 0x20 || c >= 0x80 || c == '"' || c == '\\' {
				goto slow
			}
			j++
		}
		b.buf = append(b.buf, s...)
		return
	}
slow:
	i := 0
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
		} else {
			i += b.escapeMultiByte(s, i)
		}
	}
}

// escapeASCIIByte writes the JSON escape sequence for a single ASCII byte.
func (b *Builder) escapeASCIIByte(c byte) {
	if e := shortEscape[c]; e != 0 {
		b.buf = append(b.buf, '\\', e)
	} else {
		b.buf = append(b.buf, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0x0f])
	}
}

// shortEscape maps bytes to their single-char JSON escape character.
// Non-zero entries mean the byte gets escaped as \X where X is the value.
var shortEscape = [256]byte{
	'"':  '"',
	'\\': '\\',
	'\b': 'b',
	'\f': 'f',
	'\n': 'n',
	'\r': 'r',
	'\t': 't',
}

// hexDigits maps a nibble [0..15] to its hex character. Using a fixed-size
// [16]byte array eliminates bounds checks since c>>4 and c&0x0f are always < 16.
var hexDigits = [16]byte{'0', '1', '2', '3', '4', '5', '6', '7', '8', '9', 'a', 'b', 'c', 'd', 'e', 'f'}

// escapeMultiByte validates and writes a multi-byte UTF-8 sequence.
// Returns the number of bytes consumed.
// Rejects overlong encodings, surrogate halves (U+D800–U+DFFF),
// and codepoints above U+10FFFF per RFC 8259 §8.
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

// validCodepoint checks for overlong encodings, surrogates, and out-of-range codepoints.
// Called after validContinuation confirms byte-level structure.
func validCodepoint(s string, i, size int) bool {
	_ = s[i+size-1] // BCE hint: caller guarantees i+size <= len(s)
	switch size {
	case 3:
		cp := rune(s[i]&0x0F)<<12 | rune(s[i+1]&0x3F)<<6 | rune(s[i+2]&0x3F)
		return cp >= 0x0800 && (cp < 0xD800 || cp > 0xDFFF)
	case 4:
		cp := rune(s[i]&0x07)<<18 | rune(s[i+1]&0x3F)<<12 |
			rune(s[i+2]&0x3F)<<6 | rune(s[i+3]&0x3F)
		return cp >= 0x10000 && cp <= 0x10FFFF
	}
	return true
}

// utf8SeqLen returns the expected byte length for a UTF-8 leading byte, or 0 if invalid.
// Rejects overlong 2-byte leaders (0xC0-0xC1) and out-of-range 4-byte leaders (0xF5+).
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

// validContinuation checks that bytes s[i+1..i+size-1] are valid continuation bytes (10xxxxxx).
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

// digitPairs provides two-character representations for values 00–99.
// Used by appendInt/appendUint/appendNano to halve the number of divisions.
// Sized at 256 (not 200) so that index expressions masked with &0x7F
// are provably in-bounds, letting the compiler eliminate bounds checks.
var digitPairs [256]byte

func init() {
	for i := range 100 {
		digitPairs[i*2] = byte('0' + i/10)
		digitPairs[i*2+1] = byte('0' + i%10)
	}
}

// civilDate converts a Unix timestamp (seconds since epoch) to year, month, day.
// Algorithm: http://howardhinnant.github.io/date_algorithms.html
// Negative timestamps are clamped to zero; years > 9999 are clamped to 9999.
func civilDate(sec int64) (year, month, day int) {
	sec = max(sec, 0)
	days := sec / 86400
	z := days + 719468
	era := z / 146097
	if z < 0 {
		era = (z - 146096) / 146097
	}
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

// AddTimeRFC3339Field adds a "name":"RFC3339" field without using time.Format.
// The output is ALWAYS in UTC (with 'Z' suffix) regardless of the input
// timezone, because computation uses t.Unix() which is timezone-independent.
// Uses civil-date arithmetic to avoid double absSec() computation
// that results from calling both t.Date() and t.Clock().
func (b *Builder) AddTimeRFC3339Field(name string, t time.Time) {
	b.fieldKey(name)
	b.appendTimeRFC3339(t)
}

// appendTimeRFC3339 writes "YYYY-MM-DDThh:mm:ss[.nnnnnnnnn]Z" into the buffer.
// Shared by AddTimeRFC3339Field and AddTimeRFC3339FieldKey.
func (b *Builder) appendTimeRFC3339(t time.Time) {
	b.buf = append(b.buf, '"')

	unix := t.Unix()
	ns := t.Nanosecond()

	sec := unix
	sec = max(sec, 0)
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
	ycI0, ycI1 := digitPairs[yc*2], digitPairs[yc*2+1]
	yrI0, yrI1 := digitPairs[yr*2], digitPairs[yr*2+1]
	mI0, mI1 := digitPairs[mon*2], digitPairs[mon*2+1]
	dI0, dI1 := digitPairs[d*2], digitPairs[d*2+1]
	hI0, hI1 := digitPairs[h*2], digitPairs[h*2+1]
	minI0, minI1 := digitPairs[mn*2], digitPairs[mn*2+1]
	sI0, sI1 := digitPairs[s*2], digitPairs[s*2+1]

	// YYYY-MM-DDThh:mm:ss
	b.buf = append(b.buf,
		ycI0, ycI1, yrI0, yrI1, '-',
		mI0, mI1, '-',
		dI0, dI1, 'T',
		hI0, hI1, ':',
		minI0, minI1, ':',
		sI0, sI1,
	)

	if ns > 0 {
		b.buf = append(b.buf, '.')
		b.appendNano(ns)
	}
	b.buf = append(b.buf, 'Z', '"')
}

// appendNano writes a nanosecond value with trailing zeros trimmed.
// Uses digit pairs to halve the number of divisions.
// Pre-computes all indices into local vars to eliminate bounds checks.
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

	// Load all digit pairs into locals to eliminate per-index bounds checks.
	p0, p1 := digitPairs[d01*2], digitPairs[d01*2+1]
	p2, p3 := digitPairs[d23*2], digitPairs[d23*2+1]
	p4, p5 := digitPairs[d45*2], digitPairs[d45*2+1]
	p6, p7 := digitPairs[d67*2], digitPairs[d67*2+1]

	b.buf = append(b.buf, p0, p1, p2, p3, p4, p5, p6, p7, byte('0'+d8))
	end := len(b.buf)
	for end > dotPos && b.buf[end-1] == '0' {
		end--
	}
	b.buf = b.buf[:end]
}

// appendInt writes an integer directly into the builder's buffer without heap allocation.
// Uses digit-pair table to halve divisions. Fast path for small values [0,99].
func (b *Builder) appendInt(x int) {
	if x >= 0 && x < 100 {
		if x < 10 {
			b.buf = append(b.buf, byte('0'+x))
		} else {
			b.buf = append(b.buf, digitPairs[x*2], digitPairs[x*2+1])
		}
		return
	}
	if x < 0 {
		b.buf = append(b.buf, '-')
		b.appendUint(absInt64AsUint64(int64(x)))
		return
	}
	b.appendUint(uint64(x))
}

// appendUint writes an unsigned integer using digit-pair table.
// Fast paths for values < 10, < 100, and < 1000 avoid the division loop.
func (b *Builder) appendUint(x uint64) {
	if x < 100 {
		if x < 10 {
			b.buf = append(b.buf, byte('0'+x))
		} else {
			b.buf = append(b.buf, digitPairs[x*2], digitPairs[x*2+1])
		}
		return
	}
	_ = digitPairs[255] // BCE hint: prove [256]byte bounds for all indexed accesses
	if x < 1000 {
		d := x / 100
		r := x % 100
		b.buf = append(b.buf, byte('0'+d), digitPairs[r*2], digitPairs[r*2+1])
		return
	}
	var tmp [20]byte
	i := len(tmp)
	for x >= 100 {
		j := x % 100
		x /= 100
		i -= 2
		tmp[i] = digitPairs[j*2]
		tmp[i+1] = digitPairs[j*2+1]
	}
	if x >= 10 {
		i -= 2
		tmp[i] = digitPairs[x*2]
		tmp[i+1] = digitPairs[x*2+1]
	} else {
		i--
		tmp[i] = byte('0' + x)
	}
	b.buf = append(b.buf, tmp[i:]...)
}

// appendFloat64 writes a float64 to the buffer.
// Integers are written without decimal point.
// Fractional values use strconv.AppendFloat (Ryū algorithm) for full precision.
func (b *Builder) appendFloat64(v float64) {
	if v == float64(int64(v)) && v < 1e18 && v > -1e18 {
		if v < 0 {
			b.buf = append(b.buf, '-')
			b.appendUint(uint64(-int64(v))) //nolint:gosec // safe: v > -1e18 guarantees no overflow
		} else {
			b.appendUint(uint64(int64(v))) //nolint:gosec // safe: v < 1e18 guarantees no overflow
		}
		return
	}
	b.buf = strconv.AppendFloat(b.buf, v, 'f', -1, 64)
}

// absInt64AsUint64 converts an int64 to its absolute value as uint64.
// This handles math.MinInt64 correctly without relying on two's-complement
// overflow: -(MinInt64+1) is MaxInt63, then +1 gives 2^63.
func absInt64AsUint64(v int64) uint64 {
	if v >= 0 {
		return uint64(v)
	}
	return uint64(-(v + 1)) + 1 //nolint:gosec // intentional: avoids MinInt64 overflow
}

// AddNullField adds a "name":null field.
func (b *Builder) AddNullField(name string) {
	b.fieldKey(name)
	b.buf = append(b.buf, "null"...)
}

// AddStringMapObject writes a map[string]string as a JSON object {...} at the
// current position. Keys are sorted for deterministic output.
// If rawJSONKey is non-empty, values for that key are
// embedded as raw JSON when they pass structural validation (via IsLikelyJSON).
// This method does NOT add a field name — it writes just the object.
func (b *Builder) AddStringMapObject(m map[string]string, rawJSONKey string) {
	b.BeginObject()
	keys := sortedMapKeys(m)
	for _, k := range keys {
		v := m[k]
		if rawJSONKey != "" && k == rawJSONKey && IsLikelyJSON(v) {
			b.AddRawJSONFieldString(k, v)
			continue
		}
		b.AddStringField(k, v)
	}
	b.EndObject()
}

// IsLikelyJSON reports whether s is a structurally valid JSON object or array
// with no trailing content. For objects, validates "key":value grammar via the
// SWAR-accelerated iterateRawFields parser. For arrays, validates element
// separation via iterateRawArray. Zero allocation.
// Does NOT validate semantic correctness (e.g., duplicate keys, number formats).
//
//nolint:gosec // unsafe.Slice: read-only zero-alloc string→[]byte view; s is not mutated
func IsLikelyJSON(s string) bool {
	if len(s) < 2 || (s[0] != '{' && s[0] != '[') {
		return false
	}
	data := unsafe.Slice(unsafe.StringData(s), len(s))
	// Grammar-validate AND reject trailing garbage: SkipValueAt returns the
	// end position; if it doesn't consume the entire string, there's trailing
	// content. SkipValueAt delegates to SkipBracedAt for objects/arrays, which
	// validates balanced delimiters and strings but not key:value grammar.
	// We run both checks: SkipValueAt for delimiter balance + no trailing,
	// and the grammar iterator for structural correctness.
	end, ok := SkipValueAt(data, 0)
	if !ok || end != len(data) {
		return false
	}
	if data[0] == '{' {
		return iterateRawFields(data, func(_ []byte, _, _, _, _ int) bool { return true })
	}
	return iterateRawArray(data, func(_ []byte) bool { return true })
}

// EscapeString returns s with JSON special characters escaped per RFC 8259.
// If no escaping is needed (pure safe ASCII), it returns s unchanged (zero allocation).
// Invalid UTF-8 bytes are replaced with U+FFFD, consistent with Builder.escapeString.
func EscapeString(s string) string {
	// Fast path: check if escaping is needed at all using SWAR.
	if swarScanSafe(s, 0) == len(s) {
		return s
	}
	// Use a stack buffer for short strings to avoid heap allocation.
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

// pool is a sync.Pool for reusing Builder instances.
var pool = sync.Pool{
	New: func() any {
		return &Builder{
			buf:   make([]byte, 0, 2048),
			first: true,
		}
	},
}

// Acquire returns a Builder from the pool, ready for use.
// Call Release when done to return it for reuse.
func Acquire() *Builder {
	b, ok := pool.Get().(*Builder)
	if !ok {
		return New(2048)
	}
	b.Reset()
	return b
}

// Release returns the Builder to the pool for reuse.
// The builder must not be used after calling Release.
// Buffers larger than 256 KB are discarded to bound memory retention.
func Release(b *Builder) {
	if b == nil {
		return
	}
	if cap(b.buf) <= 1<<18 {
		pool.Put(b)
	}
}
