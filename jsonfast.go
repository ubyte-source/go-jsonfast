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

// Builder appends JSON into a reusable byte slice. Not safe for
// concurrent use.
type Builder struct {
	buf   []byte
	first bool
}

// New returns a Builder with the given initial capacity. Non-positive
// capacities are clamped to 256.
func New(capacity int) *Builder {
	if capacity <= 0 {
		capacity = 256
	}
	return &Builder{
		buf:   make([]byte, 0, capacity),
		first: true,
	}
}

// Reset clears the buffer contents while retaining the backing array.
func (b *Builder) Reset() {
	b.buf = b.buf[:0]
	b.first = true
}

// Bytes returns the accumulated bytes. The slice aliases the internal
// buffer and must not be used after Reset or Release.
func (b *Builder) Bytes() []byte { return b.buf }

// Len returns the current buffer length.
func (b *Builder) Len() int { return len(b.buf) }

// Grow ensures at least n bytes of spare capacity.
func (b *Builder) Grow(n int) {
	if cap(b.buf)-len(b.buf) < n {
		b.buf = slices.Grow(b.buf, n)
	}
}

// Write implements io.Writer. It never returns an error.
func (b *Builder) Write(p []byte) (int, error) {
	b.buf = append(b.buf, p...)
	return len(p), nil
}

// WriteTo implements io.WriterTo. The Builder state is unchanged.
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
func (b *Builder) EndObject() { b.buf = append(b.buf, '}') }

// BeginObjectField opens a nested object as "name":{, ready for inner
// Add*Field calls. Pair with EndObjectField.
func (b *Builder) BeginObjectField(name string) {
	b.fieldKey(name)
	b.buf = append(b.buf, '{')
	b.first = true
}

// BeginObjectFieldKey opens a nested object with a pre-computed key.
func (b *Builder) BeginObjectFieldKey(k FieldKey) {
	b.precomputedKey(k)
	b.buf = append(b.buf, '{')
	b.first = true
}

// EndObjectField closes a nested object opened by BeginObjectField or
// BeginObjectFieldKey and restores the outer separator state.
func (b *Builder) EndObjectField() {
	b.buf = append(b.buf, '}')
	b.first = false
}

func (b *Builder) sep() {
	if b.first {
		b.first = false
		return
	}
	b.buf = append(b.buf, ',')
}

// fieldKey writes [,]"name": with name escaped.
func (b *Builder) fieldKey(name string) {
	b.sep()
	b.buf = append(b.buf, '"')
	b.escapeString(name)
	b.buf = append(b.buf, '"', ':')
}

// AddStringField adds "name":"value" with value escaping.
func (b *Builder) AddStringField(name, value string) {
	b.fieldKey(name)
	b.buf = append(b.buf, '"')
	b.escapeString(value)
	b.buf = append(b.buf, '"')
}

// AddStringArrayField adds "name":["v1","v2",...] with value escaping.
func (b *Builder) AddStringArrayField(name string, values []string) {
	b.fieldKey(name)
	b.appendStringArray(values)
}

// AddRawJSONField adds "name":<raw> without escaping. rawJSON must be
// well-formed JSON.
func (b *Builder) AddRawJSONField(name string, rawJSON []byte) {
	b.fieldKey(name)
	b.buf = append(b.buf, rawJSON...)
}

// AddBoolField adds "name":true or "name":false.
func (b *Builder) AddBoolField(name string, v bool) {
	b.fieldKey(name)
	if v {
		b.buf = append(b.buf, "true"...)
	} else {
		b.buf = append(b.buf, "false"...)
	}
}

// AddNullField adds "name":null.
func (b *Builder) AddNullField(name string) {
	b.fieldKey(name)
	b.buf = append(b.buf, "null"...)
}

// AddIntField adds "name":<int>.
func (b *Builder) AddIntField(name string, v int) {
	b.fieldKey(name)
	b.appendInt(v)
}

// AddInt64Field adds "name":<int64>.
func (b *Builder) AddInt64Field(name string, v int64) {
	b.fieldKey(name)
	if v < 0 {
		b.buf = append(b.buf, '-')
		b.appendUint(absInt64AsUint64(v))
		return
	}
	b.appendUint(uint64(v))
}

// AddFloat64Field adds "name":<float64>. NaN and ±Inf are emitted as null.
func (b *Builder) AddFloat64Field(name string, v float64) {
	b.fieldKey(name)
	b.appendFloat64(v)
}

// AddTimeRFC3339Field adds "name":"<RFC3339>" in UTC. Years are clamped
// to [0, 9999]; pre-epoch timestamps are clamped to the epoch.
func (b *Builder) AddTimeRFC3339Field(name string, t time.Time) {
	b.fieldKey(name)
	b.appendTimeRFC3339(t)
}

// AddTimeRFC3339OffsetField adds "name":"<RFC3339>" preserving the
// input timezone offset (Z, +HH:MM, or -HH:MM).
func (b *Builder) AddTimeRFC3339OffsetField(name string, t time.Time) {
	b.fieldKey(name)
	b.appendTimeRFC3339Offset(t)
}

// AppendRaw appends raw bytes.
func (b *Builder) AppendRaw(p []byte) { b.buf = append(b.buf, p...) }

// AppendRawString appends a raw string.
func (b *Builder) AppendRawString(s string) { b.buf = append(b.buf, s...) }

// AppendEscapedString appends s with JSON escaping (no surrounding quotes).
func (b *Builder) AppendEscapedString(s string) { b.escapeString(s) }

// AddRawBytesField adds "name":<value> where name and value are copied
// verbatim.
func (b *Builder) AddRawBytesField(name, value []byte) {
	b.sep()
	b.buf = append(b.buf, '"')
	b.buf = append(b.buf, name...)
	b.buf = append(b.buf, '"', ':')
	b.buf = append(b.buf, value...)
}

// AddStringMapObject writes m as a standalone JSON object (no name, no
// leading comma). Keys are sorted. If rawJSONKey is non-empty, values
// for that key are embedded as raw JSON when IsStructuralJSON accepts them.
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
			b.fieldKey(k)
			b.buf = append(b.buf, v...)
			continue
		}
		b.AddStringField(k, v)
	}
	b.EndObject()
}

// AddStringMapObjectField adds "name":{...} with m encoded as a JSON
// object. Semantics match AddStringMapObject.
func (b *Builder) AddStringMapObjectField(name string, m map[string]string, rawJSONKey string) {
	b.fieldKey(name)
	b.AddStringMapObject(m, rawJSONKey)
	b.first = false
}

// AddNestedStringMapField adds "name":{outer:{inner:"v"}}, keys sorted
// at both levels.
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

// FieldKey is a pre-computed field prefix in the form `,"name":`.
type FieldKey string

// NewFieldKey returns a FieldKey for the given safe-ASCII name.
func NewFieldKey(name string) FieldKey {
	return FieldKey(`,"` + name + `":`)
}

func (b *Builder) precomputedKey(k FieldKey) {
	if b.first {
		b.first = false
		b.buf = append(b.buf, k[1:]...)
		return
	}
	b.buf = append(b.buf, k...)
}

// AddStringFieldKey adds "name":"value" using a pre-computed key.
func (b *Builder) AddStringFieldKey(k FieldKey, value string) {
	b.precomputedKey(k)
	b.buf = append(b.buf, '"')
	b.escapeString(value)
	b.buf = append(b.buf, '"')
}

// AddStringArrayFieldKey adds "name":["v1",...] using a pre-computed key.
func (b *Builder) AddStringArrayFieldKey(k FieldKey, values []string) {
	b.precomputedKey(k)
	b.appendStringArray(values)
}

func (b *Builder) appendStringArray(values []string) {
	b.buf = append(b.buf, '[')
	for i, v := range values {
		if i > 0 {
			b.buf = append(b.buf, ',')
		}
		b.buf = append(b.buf, '"')
		b.escapeString(v)
		b.buf = append(b.buf, '"')
	}
	b.buf = append(b.buf, ']')
}

// AddIntFieldKey adds "name":<int> using a pre-computed key.
func (b *Builder) AddIntFieldKey(k FieldKey, v int) {
	b.precomputedKey(k)
	b.appendInt(v)
}

// AddInt64FieldKey adds "name":<int64> using a pre-computed key.
func (b *Builder) AddInt64FieldKey(k FieldKey, v int64) {
	b.precomputedKey(k)
	if v < 0 {
		b.buf = append(b.buf, '-')
		b.appendUint(absInt64AsUint64(v))
		return
	}
	b.appendUint(uint64(v))
}

// AddUint64FieldKey adds "name":<uint64> using a pre-computed key.
func (b *Builder) AddUint64FieldKey(k FieldKey, v uint64) {
	b.precomputedKey(k)
	b.appendUint(v)
}

// AddBoolFieldKey adds "name":true or "name":false using a pre-computed key.
func (b *Builder) AddBoolFieldKey(k FieldKey, v bool) {
	b.precomputedKey(k)
	if v {
		b.buf = append(b.buf, "true"...)
	} else {
		b.buf = append(b.buf, "false"...)
	}
}

// AddRawJSONFieldKey adds "name":<raw> using a pre-computed key.
func (b *Builder) AddRawJSONFieldKey(k FieldKey, rawJSON []byte) {
	b.precomputedKey(k)
	b.buf = append(b.buf, rawJSON...)
}

// AddTimeRFC3339FieldKey adds "name":"<RFC3339>" in UTC using a pre-computed key.
func (b *Builder) AddTimeRFC3339FieldKey(k FieldKey, t time.Time) {
	b.precomputedKey(k)
	b.appendTimeRFC3339(t)
}

// AddTimeRFC3339OffsetFieldKey adds "name":"<RFC3339>" preserving the
// timezone offset, using a pre-computed key.
func (b *Builder) AddTimeRFC3339OffsetFieldKey(k FieldKey, t time.Time) {
	b.precomputedKey(k)
	b.appendTimeRFC3339Offset(t)
}

// AddFloat64FieldKey adds "name":<float64> using a pre-computed key.
func (b *Builder) AddFloat64FieldKey(k FieldKey, v float64) {
	b.precomputedKey(k)
	b.appendFloat64(v)
}

// AddNullFieldKey adds "name":null using a pre-computed key.
func (b *Builder) AddNullFieldKey(k FieldKey) {
	b.precomputedKey(k)
	b.buf = append(b.buf, "null"...)
}

// ---------------------------------------------------------------------------
// String escaping
// ---------------------------------------------------------------------------

// safeASCII[b] is true for printable ASCII other than '"' and '\\'.
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

// escapeString writes s with JSON escaping and UTF-8 validation;
// invalid UTF-8 bytes are replaced with U+FFFD.
func (b *Builder) escapeString(s string) {
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
// requires escaping, or len(s) if all remaining bytes are safe.
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

func (b *Builder) escapeASCIIByte(c byte) {
	if e := shortEscape[c]; e != 0 {
		b.buf = append(b.buf, '\\', e)
		return
	}
	b.buf = append(b.buf, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0x0f])
}

// shortEscape maps a byte to its single-char JSON escape letter, or 0.
var shortEscape = [256]byte{
	'"':  '"',
	'\\': '\\',
	'\b': 'b',
	'\f': 'f',
	'\n': 'n',
	'\r': 'r',
	'\t': 't',
}

var hexDigits = [16]byte{
	'0', '1', '2', '3', '4', '5', '6', '7',
	'8', '9', 'a', 'b', 'c', 'd', 'e', 'f',
}

// escapeMultiByte validates a UTF-8 sequence and copies it verbatim on
// success, or replaces it with U+FFFD and returns the bytes consumed.
func (b *Builder) escapeMultiByte(s string, i int) int {
	size := utf8SeqLen(s[i])
	if size == 0 || i+size > len(s) || !validContinuation(s, i, size) {
		b.buf = append(b.buf, 0xEF, 0xBF, 0xBD)
		return 1
	}
	if !validCodepoint(s, i, size) {
		b.buf = append(b.buf, 0xEF, 0xBF, 0xBD)
		return size
	}
	b.buf = append(b.buf, s[i:i+size]...)
	return size
}

// validCodepoint rejects overlong encodings, surrogates, and codepoints
// above U+10FFFF. Called after validContinuation.
func validCodepoint(s string, i, size int) bool {
	_ = s[i+size-1]
	switch size {
	case 3:
		_ = s[i+2]
		cp := rune(s[i]&0x0F)<<12 | rune(s[i+1]&0x3F)<<6 | rune(s[i+2]&0x3F)
		return cp >= 0x0800 && (cp < 0xD800 || cp > 0xDFFF)
	case 4:
		_ = s[i+3]
		cp := rune(s[i]&0x07)<<18 | rune(s[i+1]&0x3F)<<12 |
			rune(s[i+2]&0x3F)<<6 | rune(s[i+3]&0x3F)
		return cp >= 0x10000 && cp <= 0x10FFFF
	}
	return true
}

// utf8SeqLen returns the byte length implied by a UTF-8 leading byte,
// or 0 if it is not a valid leader.
func utf8SeqLen(c byte) int {
	switch {
	case c >= 0xC2 && c <= 0xDF:
		return 2
	case c&0xF0 == 0xE0:
		return 3
	case c >= 0xF0 && c <= 0xF4:
		return 4
	default:
		return 0
	}
}

// validContinuation reports whether s[i+1..i+size-1] are continuation bytes.
func validContinuation(s string, i, size int) bool {
	_ = s[i+size-1]
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

// EscapeString returns s with JSON escaping. If s is already safe ASCII
// it is returned unchanged (zero allocation). Invalid UTF-8 bytes are
// replaced with U+FFFD.
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

// digitPairs holds the two-char representation of 00–99 (256-byte
// sized so &0x7F-masked indices are provably in-bounds).
var digitPairs [256]byte

func init() {
	for i := range 100 {
		digitPairs[i*2] = byte('0' + i/10)
		digitPairs[i*2+1] = byte('0' + i%10)
	}
}

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
		r := (x % 100) & 0x7F
		b.buf = append(b.buf, byte('0'+d), digitPairs[r*2], digitPairs[r*2+1])
		return
	}
	var tmp [20]byte
	i := len(tmp)
	for x >= 100 {
		j := (x % 100) & 0x7F
		x /= 100
		i -= 2
		tmp[i] = digitPairs[j*2]
		tmp[i+1] = digitPairs[j*2+1]
	}
	if x >= 10 {
		x &= 0x7F
		i -= 2
		tmp[i] = digitPairs[x*2]
		tmp[i+1] = digitPairs[x*2+1]
	} else {
		i--
		tmp[i] = byte('0' + x)
	}
	b.buf = append(b.buf, tmp[i:]...)
}

func (b *Builder) appendFloat64(v float64) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		b.buf = append(b.buf, "null"...)
		return
	}
	if v > -1e18 && v < 1e18 {
		if iv := int64(v); float64(iv) == v {
			if iv < 0 {
				b.buf = append(b.buf, '-')
				b.appendUint(absInt64AsUint64(iv))
				return
			}
			b.appendUint(uint64(iv))
			return
		}
	}
	b.buf = strconv.AppendFloat(b.buf, v, 'f', -1, 64)
}

// absInt64AsUint64 returns |v| as a uint64, handling math.MinInt64.
func absInt64AsUint64(v int64) uint64 {
	if v >= 0 {
		return uint64(v)
	}
	return uint64(-(v + 1)) + 1 //nolint:gosec // MinInt64 handling
}

// ---------------------------------------------------------------------------
// RFC 3339 time formatting
// ---------------------------------------------------------------------------

// civilDate converts Unix seconds to (year, month, day). Negative
// inputs are clamped to the epoch; years > 9999 are clamped to 9999.
func civilDate(sec int64) (year, month, day int) {
	sec = max(sec, 0)
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

func (b *Builder) appendTimeRFC3339(t time.Time) {
	b.buf = append(b.buf, '"')
	b.appendCivilDateTime(t.Unix())
	if ns := t.Nanosecond(); ns > 0 {
		b.buf = append(b.buf, '.')
		b.appendNano(ns)
	}
	b.buf = append(b.buf, 'Z', '"')
}

func (b *Builder) appendTimeRFC3339Offset(t time.Time) {
	_, offset := t.Zone()
	unix := t.Unix() + int64(offset)

	b.buf = append(b.buf, '"')
	b.appendCivilDateTime(unix)
	if ns := t.Nanosecond(); ns > 0 {
		b.buf = append(b.buf, '.')
		b.appendNano(ns)
	}
	b.appendZoneOffset(offset)
	b.buf = append(b.buf, '"')
}

func (b *Builder) appendCivilDateTime(unix int64) {
	sec := max(unix, 0)
	daySeconds := sec % 86400
	hour := int(daySeconds / 3600)
	minute := int(daySeconds % 3600 / 60)
	secs := int(daySeconds % 60)

	year, month, day := civilDate(unix)

	// &0x7F masks make *2+1 < 256 provable at BCE.
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
}

func (b *Builder) appendZoneOffset(offset int) {
	if offset == 0 {
		b.buf = append(b.buf, 'Z')
		return
	}
	sign := byte('+')
	if offset < 0 {
		sign = '-'
		offset = -offset
	}
	h := (offset / 3600) & 0x7F
	m := ((offset % 3600) / 60) & 0x7F
	b.buf = append(b.buf,
		sign,
		digitPairs[h*2], digitPairs[h*2+1], ':',
		digitPairs[m*2], digitPairs[m*2+1],
	)
}

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

const (
	poolBufferSize = 2048
	poolMaxRetain  = 1 << 18 // 256 KB
)

var pool = sync.Pool{
	New: func() any {
		return &Builder{
			buf:   make([]byte, 0, poolBufferSize),
			first: true,
		}
	},
}

// Acquire returns a Builder from the pool, ready for use.
func Acquire() *Builder {
	b, ok := pool.Get().(*Builder)
	if !ok {
		return New(poolBufferSize)
	}
	b.Reset()
	return b
}

// Release returns b to the pool. Buffers larger than 256 KB are discarded.
func Release(b *Builder) {
	if b == nil {
		return
	}
	if cap(b.buf) <= poolMaxRetain {
		pool.Put(b)
	}
}

// WarmPool pre-allocates n Builders and returns them to the pool.
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
