package jsonfast

import (
	"strconv"
	"unsafe"
)

// wsTable[b] is true for ASCII whitespace (' ', '\n', '\r', '\t').
var wsTable = [256]bool{
	' ':  true,
	'\n': true,
	'\r': true,
	'\t': true,
}

// SkipWS returns the index of the first non-whitespace byte at or after i.
func SkipWS(data []byte, i int) int {
	for i < len(data) && wsTable[data[i]] {
		i++
	}
	return i
}

// SkipValueAt skips one JSON value starting at data[i] and returns the
// index past it.
func SkipValueAt(data []byte, i int) (int, bool) {
	i = SkipWS(data, i)
	if i >= len(data) {
		return i, false
	}
	switch data[i] {
	case '"':
		return SkipStringAt(data, i)
	case '{':
		return SkipBracedAt(data, i, '{', '}')
	case '[':
		return SkipBracedAt(data, i, '[', ']')
	}
	return skipScalar(data, i)
}

// skipScalar scans a JSON number or one of true/false/null.
func skipScalar(data []byte, i int) (int, bool) {
	if i >= len(data) {
		return i, false
	}
	switch data[i] {
	case 't':
		return matchLiteral(data, i, "true")
	case 'f':
		return matchLiteral(data, i, "false")
	case 'n':
		return matchLiteral(data, i, "null")
	}
	return skipNumber(data, i)
}

func matchLiteral(data []byte, i int, want string) (int, bool) {
	if i+len(want) > len(data) || string(data[i:i+len(want)]) != want {
		return i, false
	}
	return i + len(want), true
}

// skipNumber validates an RFC 8259 number:
//
//	[ "-" ] ( "0" | "1"-"9" *DIGIT ) [ "." 1*DIGIT ] [ ("e"|"E") ["+"|"-"] 1*DIGIT ]
func skipNumber(data []byte, i int) (int, bool) {
	start := i
	if i < len(data) && data[i] == '-' {
		i++
	}
	j, ok := skipNumberInt(data, i)
	if !ok {
		return start, false
	}
	if j, ok = skipNumberFrac(data, j); !ok {
		return start, false
	}
	if j, ok = skipNumberExp(data, j); !ok {
		return start, false
	}
	return j, true
}

func skipNumberInt(data []byte, i int) (int, bool) {
	if i >= len(data) {
		return i, false
	}
	if data[i] == '0' {
		return i + 1, true
	}
	if data[i] < '1' || data[i] > '9' {
		return i, false
	}
	return skipDigits(data, i+1), true
}

func skipNumberFrac(data []byte, i int) (int, bool) {
	if i >= len(data) || data[i] != '.' {
		return i, true
	}
	j := i + 1
	if j >= len(data) || data[j] < '0' || data[j] > '9' {
		return i, false
	}
	return skipDigits(data, j+1), true
}

func skipNumberExp(data []byte, i int) (int, bool) {
	if i >= len(data) || (data[i] != 'e' && data[i] != 'E') {
		return i, true
	}
	j := i + 1
	if j < len(data) && (data[j] == '+' || data[j] == '-') {
		j++
	}
	if j >= len(data) || data[j] < '0' || data[j] > '9' {
		return i, false
	}
	return skipDigits(data, j+1), true
}

func skipDigits(data []byte, i int) int {
	for i < len(data) && data[i] >= '0' && data[i] <= '9' {
		i++
	}
	return i
}

// SkipStringAt skips a JSON string starting at data[i] (which must be
// '"') and returns the index past the closing quote. Raw control bytes
// (< 0x20) are rejected.
func SkipStringAt(data []byte, i int) (int, bool) {
	if i >= len(data) || data[i] != '"' {
		return i, false
	}
	n := len(data)
	j := swarSkipStringBulk(data, i+1, n)
	return scanStringTail(data, j, n)
}

//nolint:gosec // unsafe pointer math is load-bearing for SWAR throughput
func swarSkipStringBulk(data []byte, j, n int) int {
	if n-j < 8 {
		return j
	}
	p := unsafe.Pointer(unsafe.SliceData(data))
	for j+8 <= n {
		w := *(*uint64)(unsafe.Add(p, j))
		if swarSpecialSkip(w) != 0 {
			return j
		}
		j += 8
	}
	return j
}

func scanStringTail(data []byte, j, n int) (int, bool) {
	for j < n {
		c := data[j]
		switch {
		case c == '"':
			return j + 1, true
		case c == '\\':
			if j+1 >= n {
				return j, false
			}
			j += 2
		case c < 0x20:
			return j, false
		default:
			j++
		}
	}
	return j, false
}

// SkipBracedAt skips a balanced opener/closer pair starting at data[i].
func SkipBracedAt(data []byte, i int, opener, closer byte) (int, bool) {
	if i >= len(data) || data[i] != opener {
		return i, false
	}
	depth := 1
	i++
	n := len(data)
	swarOpener, swarCloser := swarBroadcastPair(opener, closer)

	for i < n {
		i = swarSkipBracedBulk(data, i, n, swarOpener, swarCloser)
		if i >= n {
			break
		}
		next, done, ok := stepBraced(data, i, opener, closer, &depth)
		if !ok {
			return i, false
		}
		if done {
			return next, true
		}
		i = next
	}
	return i, false
}

// swarBroadcastPair returns the SWAR broadcast words for opener and
// closer. The '{}/[]' pairs short-circuit to precomputed constants.
func swarBroadcastPair(opener, closer byte) (openMask, closeMask uint64) {
	switch opener {
	case '{':
		return swarBraceOpen, swarBraceClose
	case '[':
		return swarBrackOpen, swarBrackClose
	}
	return swarLo * uint64(opener), swarLo * uint64(closer)
}

func stepBraced(data []byte, i int, opener, closer byte, depth *int) (next int, done, ok bool) {
	switch data[i] {
	case '"':
		end, sok := SkipStringAt(data, i)
		if !sok {
			return i, false, false
		}
		return end, false, true
	case opener:
		*depth++
		return i + 1, false, true
	case closer:
		*depth--
		if *depth == 0 {
			return i + 1, true, true
		}
		return i + 1, false, true
	default:
		return i + 1, false, true
	}
}

//nolint:gosec // unsafe pointer math is load-bearing for SWAR throughput
func swarSkipBracedBulk(data []byte, i, n int, swarOpener, swarCloser uint64) int {
	if n-i < 8 {
		return i
	}
	p := unsafe.Pointer(unsafe.SliceData(data))
	for i+8 <= n {
		w := *(*uint64)(unsafe.Add(p, i))
		xq := w ^ swarQuote
		xb := w ^ swarBackslash
		xo := w ^ swarOpener
		xc := w ^ swarCloser
		hasSpecial := (xq-swarLo)&^xq&swarHi |
			(xb-swarLo)&^xb&swarHi |
			(xo-swarLo)&^xo&swarHi |
			(xc-swarLo)&^xc&swarHi
		if hasSpecial != 0 {
			return i
		}
		i += 8
	}
	return i
}

// iterateRawFields walks a JSON object, invoking fn for each field.
// Returns the index past '}' and whether the scan completed.
func iterateRawFields(data []byte, fn func(d []byte, ks, ke, vs, ve int) bool) (end int, ok bool) {
	i := SkipWS(data, 0)
	if i >= len(data) || data[i] != '{' {
		return i, false
	}
	i++

	for {
		i = SkipWS(data, i)
		if i >= len(data) {
			return i, false
		}
		if data[i] == '}' {
			return i + 1, true
		}
		fp, next, pok := parseField(data, i)
		if !pok {
			return next, false
		}
		i = next
		if !fn(data, fp.ks, fp.ke, fp.vs, fp.ve) {
			return i, false
		}
		next, done, sok := stepAfterField(data, i)
		if !sok {
			return next, false
		}
		if done {
			return next, true
		}
		i = next
	}
}

// fieldPos holds the start/end offsets of a parsed "key":value pair.
type fieldPos struct {
	ks, ke, vs, ve int
}

// parseField reads one "key":value pair. On failure, next is the
// position where parsing stopped.
func parseField(data []byte, i int) (fieldPos, int, bool) {
	if data[i] != '"' {
		return fieldPos{}, i, false
	}
	ks := i
	ke, sok := SkipStringAt(data, i)
	if !sok {
		return fieldPos{}, i, false
	}
	i = SkipWS(data, ke)
	if i >= len(data) || data[i] != ':' {
		return fieldPos{}, i, false
	}
	i++
	i = SkipWS(data, i)
	if i >= len(data) {
		return fieldPos{}, i, false
	}
	vs := i
	ve, vok := SkipValueAt(data, i)
	if !vok {
		return fieldPos{}, i, false
	}
	return fieldPos{ks, ke, vs, ve}, ve, true
}

// stepAfterField consumes whitespace and the ',' or '}' after a value.
func stepAfterField(data []byte, i int) (next int, done, ok bool) {
	i = SkipWS(data, i)
	if i >= len(data) {
		return i, false, false
	}
	switch data[i] {
	case ',':
		return i + 1, false, true
	case '}':
		return i + 1, true, true
	default:
		return i, false, false
	}
}

// IterateFields calls fn for each top-level field. key includes the
// surrounding quotes; value is the raw JSON bytes.
func IterateFields(data []byte, fn func(key, value []byte) bool) bool {
	_, ok := iterateRawFields(data, func(d []byte, ks, ke, vs, ve int) bool {
		return fn(d[ks:ke], d[vs:ve])
	})
	return ok
}

// IterateFieldsString is IterateFields with a string input. The slices
// passed to fn alias s and must not be mutated.
//
//nolint:gosec // unsafe: zero-alloc string→[]byte view
func IterateFieldsString(s string, fn func(key, value []byte) bool) bool {
	if s == "" {
		return false
	}
	data := unsafe.Slice(unsafe.StringData(s), len(s))
	return IterateFields(data, fn)
}

// FindField returns the raw value bytes for the first top-level field
// matching key, or (nil, false) if not found. Keys with JSON escape
// sequences are decoded on the fly.
func FindField(data []byte, key string) ([]byte, bool) {
	i := SkipWS(data, 0)
	if i >= len(data) || data[i] != '{' {
		return nil, false
	}
	i++
	for {
		i = SkipWS(data, i)
		if atObjectEnd(data, i) {
			return nil, false
		}
		fp, next, pok := parseField(data, i)
		if !pok {
			return nil, false
		}
		if matchesKey(data, fp, key) {
			return data[fp.vs:fp.ve], true
		}
		nx, done, sok := stepAfterField(data, next)
		if !sok || done {
			return nil, false
		}
		i = nx
	}
}

func atObjectEnd(data []byte, i int) bool {
	return i >= len(data) || data[i] == '}'
}

func matchesKey(data []byte, fp fieldPos, key string) bool {
	raw := data[fp.ks+1 : fp.ke-1]
	// Decoded length is at most raw length, so raw shorter than key
	// cannot match without running the decode-aware comparator.
	if len(raw) < len(key) {
		return false
	}
	if !bytesContainBackslash(raw) {
		return len(raw) == len(key) && string(raw) == key
	}
	return decodeKeyEqual(raw, key)
}

func bytesContainBackslash(raw []byte) bool {
	for _, c := range raw {
		if c == '\\' {
			return true
		}
	}
	return false
}

// decodeKeyEqual walks enc while decoding escapes, comparing each
// decoded byte against key in lockstep.
func decodeKeyEqual(enc []byte, key string) bool {
	i, j := 0, 0
	for i < len(enc) && j < len(key) {
		if enc[i] != '\\' {
			if enc[i] != key[j] {
				return false
			}
			i++
			j++
			continue
		}
		ni, nj, ok := matchEscape(enc, i, key, j)
		if !ok {
			return false
		}
		i, j = ni, nj
	}
	return i == len(enc) && j == len(key)
}

// shortEscapeByte maps a JSON escape letter to its decoded byte (0 = unmapped).
var shortEscapeByte = [256]byte{
	'"':  '"',
	'\\': '\\',
	'/':  '/',
	'b':  '\b',
	'f':  '\f',
	'n':  '\n',
	'r':  '\r',
	't':  '\t',
}

func matchEscape(enc []byte, i int, key string, j int) (nextI, nextJ int, ok bool) {
	if i+1 >= len(enc) {
		return 0, 0, false
	}
	esc := enc[i+1]
	if esc == 'u' {
		r, consumed, ok := decodeUnicodeEscape(enc, i)
		if !ok {
			return 0, 0, false
		}
		n, ok := compareCodepoint(r, key, j)
		if !ok {
			return 0, 0, false
		}
		return i + consumed, j + n, true
	}
	decoded := shortEscapeByte[esc]
	if decoded == 0 {
		return 0, 0, false
	}
	if key[j] != decoded {
		return 0, 0, false
	}
	return i + 2, j + 1, true
}

// decodeUnicodeEscape parses \uXXXX (and a following low surrogate
// when the first hit the high-surrogate range).
func decodeUnicodeEscape(enc []byte, i int) (r rune, consumed int, ok bool) {
	if i+6 > len(enc) {
		return 0, 0, false
	}
	r, ok = parseHex4(enc[i+2 : i+6])
	if !ok {
		return 0, 0, false
	}
	if r >= 0xD800 && r <= 0xDBFF {
		return decodeSurrogatePair(enc, i, r)
	}
	if r >= 0xDC00 && r <= 0xDFFF {
		return 0, 0, false
	}
	return r, 6, true
}

func decodeSurrogatePair(enc []byte, i int, high rune) (r rune, consumed int, ok bool) {
	if i+12 > len(enc) || enc[i+6] != '\\' || enc[i+7] != 'u' {
		return 0, 0, false
	}
	low, ok := parseHex4(enc[i+8 : i+12])
	if !ok || low < 0xDC00 || low > 0xDFFF {
		return 0, 0, false
	}
	return 0x10000 + (high-0xD800)<<10 + (low - 0xDC00), 12, true
}

func parseHex4(b []byte) (rune, bool) {
	var r rune
	for _, c := range b {
		r <<= 4
		switch {
		case c >= '0' && c <= '9':
			r |= rune(c - '0')
		case c >= 'a' && c <= 'f':
			r |= rune(c-'a') + 10
		case c >= 'A' && c <= 'F':
			r |= rune(c-'A') + 10
		default:
			return 0, false
		}
	}
	return r, true
}

// compareCodepoint checks whether key[j:] begins with the UTF-8
// encoding of r and returns the number of key bytes consumed.
func compareCodepoint(r rune, key string, j int) (int, bool) {
	switch {
	case r < 0x80:
		return compareCodepoint1(r, key, j)
	case r < 0x800:
		return compareCodepoint2(r, key, j)
	case r < 0x10000:
		return compareCodepoint3(r, key, j)
	default:
		return compareCodepoint4(r, key, j)
	}
}

// utf8Byte returns byte(r & 0xFF); the mask makes the conversion
// provably safe for gosec.
func utf8Byte(r rune) byte { return byte(r & 0xFF) }

func compareCodepoint1(r rune, key string, j int) (int, bool) {
	if j >= len(key) || key[j] != utf8Byte(r) {
		return 0, false
	}
	return 1, true
}

func compareCodepoint2(r rune, key string, j int) (int, bool) {
	if j+2 > len(key) ||
		key[j] != utf8Byte(0xC0|r>>6) ||
		key[j+1] != utf8Byte(0x80|r&0x3F) {
		return 0, false
	}
	return 2, true
}

func compareCodepoint3(r rune, key string, j int) (int, bool) {
	if j+3 > len(key) ||
		key[j] != utf8Byte(0xE0|r>>12) ||
		key[j+1] != utf8Byte(0x80|(r>>6)&0x3F) ||
		key[j+2] != utf8Byte(0x80|r&0x3F) {
		return 0, false
	}
	return 3, true
}

func compareCodepoint4(r rune, key string, j int) (int, bool) {
	if j+4 > len(key) ||
		key[j] != utf8Byte(0xF0|r>>18) ||
		key[j+1] != utf8Byte(0x80|(r>>12)&0x3F) ||
		key[j+2] != utf8Byte(0x80|(r>>6)&0x3F) ||
		key[j+3] != utf8Byte(0x80|r&0x3F) {
		return 0, false
	}
	return 4, true
}

// FindFieldString is FindField with a string input. The returned slice
// aliases s and must not be mutated.
//
//nolint:gosec // unsafe: zero-alloc string→[]byte view
func FindFieldString(s, key string) ([]byte, bool) {
	if s == "" {
		return nil, false
	}
	data := unsafe.Slice(unsafe.StringData(s), len(s))
	return FindField(data, key)
}

// maxFlattenDepth bounds FlattenObject recursion.
const maxFlattenDepth = 64

// FlattenObject recursively flattens a JSON object's leaves into b (up
// to 64 levels deep). Non-object input is skipped silently.
func FlattenObject(b *Builder, data []byte) bool {
	return flattenObject(b, data, 0)
}

func flattenObject(b *Builder, data []byte, depth int) bool {
	if depth > maxFlattenDepth {
		return false
	}
	i := SkipWS(data, 0)
	if i >= len(data) || data[i] != '{' {
		return true
	}
	callbackOK := true
	_, parseOK := iterateRawFields(data, func(d []byte, ks, ke, vs, ve int) bool {
		valueRaw := d[vs:ve]
		if len(valueRaw) > 0 && valueRaw[0] == '{' {
			if !flattenObject(b, valueRaw, depth+1) {
				callbackOK = false
				return false
			}
			return true
		}
		b.AddRawBytesField(d[ks+1:ke-1], valueRaw)
		return true
	})
	return parseOK && callbackOK
}

// iterateRawArray walks a JSON array, invoking fn for each element.
func iterateRawArray(data []byte, fn func(element []byte) bool) (end int, ok bool) {
	i, empty, oopen := openArray(data)
	if !oopen {
		return i, false
	}
	if empty {
		return i, true
	}
	for {
		vs := i
		ve, vok := SkipValueAt(data, i)
		if !vok {
			return i, false
		}
		i = ve
		if !fn(data[vs:ve]) {
			return i, false
		}
		next, done, sok := stepAfterArrayElement(data, i)
		if !sok {
			return next, false
		}
		if done {
			return next, true
		}
		i = next
	}
}

// openArray consumes '[' and whitespace. empty=true if the array is []
// (next points past ']'); otherwise next is the first element's byte.
func openArray(data []byte) (next int, empty, ok bool) {
	i := SkipWS(data, 0)
	if i >= len(data) || data[i] != '[' {
		return i, false, false
	}
	i = SkipWS(data, i+1)
	if i >= len(data) {
		return i, false, false
	}
	if data[i] == ']' {
		return i + 1, true, true
	}
	return i, false, true
}

func stepAfterArrayElement(data []byte, i int) (next int, done, ok bool) {
	i = SkipWS(data, i)
	if i >= len(data) {
		return i, false, false
	}
	switch data[i] {
	case ',':
		return SkipWS(data, i+1), false, true
	case ']':
		return i + 1, true, true
	default:
		return i, false, false
	}
}

// IterateArray calls fn for each element. element is the raw JSON
// bytes of the value.
func IterateArray(data []byte, fn func(element []byte) bool) bool {
	_, ok := iterateRawArray(data, fn)
	return ok
}

// IterateStringArray calls fn for each string element. val aliases the
// input and is only valid for the duration of the callback; use
// strings.Clone to retain. Non-string elements abort the iteration.
//
//nolint:gosec // unsafe.String: zero-alloc borrow into data
func IterateStringArray(data []byte, fn func(val string) bool) bool {
	return IterateArray(data, func(elem []byte) bool {
		if len(elem) < 2 || elem[0] != '"' || elem[len(elem)-1] != '"' {
			return false
		}
		if len(elem) == 2 {
			return fn("")
		}
		return fn(unsafe.String(&elem[1], len(elem)-2))
	})
}

// IterateArrayString is IterateArray with a string input.
//
//nolint:gosec // unsafe: zero-alloc string→[]byte view
func IterateArrayString(s string, fn func(element []byte) bool) bool {
	if s == "" {
		return false
	}
	data := unsafe.Slice(unsafe.StringData(s), len(s))
	return IterateArray(data, fn)
}

// IterateStringArrayString is IterateStringArray with a string input.
//
//nolint:gosec // unsafe: zero-alloc string→[]byte view
func IterateStringArrayString(s string, fn func(val string) bool) bool {
	if s == "" {
		return false
	}
	data := unsafe.Slice(unsafe.StringData(s), len(s))
	return IterateStringArray(data, fn)
}

// IsStructuralJSON reports whether s is a grammar-valid JSON object or
// array (every nested value checked) with no trailing content. Duplicate
// keys are not rejected; invalid UTF-8 bytes are passed through.
//
//nolint:gosec // unsafe: zero-alloc string→[]byte view
func IsStructuralJSON(s string) bool {
	if len(s) < 2 || (s[0] != '{' && s[0] != '[') {
		return false
	}
	data := unsafe.Slice(unsafe.StringData(s), len(s))
	end, ok := validateValue(data, 0)
	return ok && SkipWS(data, end) == len(data)
}

// validateValue validates one JSON value (recursing into nested
// containers) and returns the index past it.
func validateValue(data []byte, i int) (int, bool) {
	i = SkipWS(data, i)
	if i >= len(data) {
		return i, false
	}
	switch data[i] {
	case '"':
		return SkipStringAt(data, i)
	case '{':
		return validateObject(data, i)
	case '[':
		return validateArray(data, i)
	}
	return skipScalar(data, i)
}

func validateObject(data []byte, i int) (int, bool) {
	i++
	first := true
	for {
		i = SkipWS(data, i)
		if i >= len(data) {
			return i, false
		}
		if data[i] == '}' {
			return i + 1, true
		}
		if !first {
			next, ok := expectComma(data, i)
			if !ok {
				return i, false
			}
			i = next
		}
		first = false
		end, ok := validateObjectEntry(data, i)
		if !ok {
			return i, false
		}
		i = end
	}
}

func expectComma(data []byte, i int) (int, bool) {
	if data[i] != ',' {
		return i, false
	}
	j := SkipWS(data, i+1)
	if j >= len(data) {
		return i, false
	}
	return j, true
}

func validateObjectEntry(data []byte, i int) (int, bool) {
	if data[i] != '"' {
		return i, false
	}
	end, ok := SkipStringAt(data, i)
	if !ok {
		return i, false
	}
	j := SkipWS(data, end)
	if j >= len(data) || data[j] != ':' {
		return i, false
	}
	return validateValue(data, j+1)
}

func validateArray(data []byte, i int) (int, bool) {
	i++
	first := true
	for {
		i = SkipWS(data, i)
		if i >= len(data) {
			return i, false
		}
		if data[i] == ']' {
			return i + 1, true
		}
		if !first {
			if data[i] != ',' {
				return i, false
			}
			i++
		}
		first = false
		end, ok := validateValue(data, i)
		if !ok {
			return i, false
		}
		i = end
	}
}

// ---------------------------------------------------------------------------
// Decoders
// ---------------------------------------------------------------------------

// DecodeString decodes a JSON string (including surrounding quotes)
// into its Go form. The returned string is a fresh allocation.
func DecodeString(raw []byte) (string, bool) {
	if len(raw) < 2 || raw[0] != '"' || raw[len(raw)-1] != '"' {
		return "", false
	}
	body := raw[1 : len(raw)-1]
	if !bytesContainBackslash(body) {
		return string(body), true
	}
	out := make([]byte, 0, len(body))
	out, ok := appendDecoded(out, body)
	if !ok {
		return "", false
	}
	return string(out), true
}

// DecodeBool decodes "true" or "false".
func DecodeBool(raw []byte) (value, ok bool) {
	switch len(raw) {
	case 4:
		if string(raw) == "true" {
			return true, true
		}
	case 5:
		if string(raw) == "false" {
			return false, true
		}
	}
	return false, false
}

// DecodeInt64 decodes a JSON integer. Rejects fractional forms,
// exponents, leading '+' and leading zeros (except "0" and "-0").
func DecodeInt64(raw []byte) (int64, bool) {
	if !isJSONInteger(raw) {
		return 0, false
	}
	v, err := strconv.ParseInt(bytesToString(raw), 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// DecodeUint64 decodes a non-negative JSON integer. Same rejection
// rules as DecodeInt64.
func DecodeUint64(raw []byte) (uint64, bool) {
	if !isJSONInteger(raw) || raw[0] == '-' {
		return 0, false
	}
	v, err := strconv.ParseUint(bytesToString(raw), 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// DecodeFloat64 decodes any RFC 8259 number into a float64. NaN and
// Inf are rejected.
func DecodeFloat64(raw []byte) (float64, bool) {
	end, ok := skipNumber(raw, 0)
	if !ok || end != len(raw) {
		return 0, false
	}
	v, err := strconv.ParseFloat(bytesToString(raw), 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

func isJSONInteger(raw []byte) bool {
	if len(raw) == 0 {
		return false
	}
	i := 0
	if raw[0] == '-' {
		i = 1
		if i == len(raw) {
			return false
		}
	}
	if raw[i] == '0' {
		return i+1 == len(raw)
	}
	if raw[i] < '1' || raw[i] > '9' {
		return false
	}
	for ; i < len(raw); i++ {
		if raw[i] < '0' || raw[i] > '9' {
			return false
		}
	}
	return true
}

// bytesToString returns a read-only string view over raw.
//
//nolint:gosec // unsafe.String: zero-alloc borrow for strconv
func bytesToString(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	return unsafe.String(&raw[0], len(raw))
}

// appendDecoded decodes a JSON string body (no quotes) into dst.
func appendDecoded(dst, src []byte) ([]byte, bool) {
	i := 0
	for i < len(src) {
		if src[i] != '\\' {
			dst = append(dst, src[i])
			i++
			continue
		}
		next, out, ok := decodeEscape(src, i, dst)
		if !ok {
			return nil, false
		}
		dst = out
		i = next
	}
	return dst, true
}

func decodeEscape(src []byte, i int, dst []byte) (next int, out []byte, ok bool) {
	if i+1 >= len(src) {
		return 0, dst, false
	}
	esc := src[i+1]
	if esc == 'u' {
		r, consumed, ok := decodeUnicodeEscape(src, i)
		if !ok {
			return 0, dst, false
		}
		return i + consumed, appendRuneUTF8(dst, r), true
	}
	decoded := shortEscapeByte[esc]
	if decoded == 0 {
		return 0, dst, false
	}
	return i + 2, append(dst, decoded), true
}

func appendRuneUTF8(dst []byte, r rune) []byte {
	switch {
	case r < 0x80:
		return append(dst, utf8Byte(r))
	case r < 0x800:
		return append(dst,
			utf8Byte(0xC0|r>>6),
			utf8Byte(0x80|r&0x3F),
		)
	case r < 0x10000:
		return append(dst,
			utf8Byte(0xE0|r>>12),
			utf8Byte(0x80|(r>>6)&0x3F),
			utf8Byte(0x80|r&0x3F),
		)
	default:
		return append(dst,
			utf8Byte(0xF0|r>>18),
			utf8Byte(0x80|(r>>12)&0x3F),
			utf8Byte(0x80|(r>>6)&0x3F),
			utf8Byte(0x80|r&0x3F),
		)
	}
}
