package jsonfast

import "unsafe"

// wsTable marks ASCII whitespace (space, \n, \r, \t) as true. Using a
// lookup table eliminates the multi-branch character classifier in SkipWS.
var wsTable = [256]bool{
	' ':  true,
	'\n': true,
	'\r': true,
	'\t': true,
}

// SkipWS returns the index of the first non-whitespace byte at or after i.
// Uses a 256-byte lookup table for branch-free classification.
func SkipWS(data []byte, i int) int {
	for i < len(data) && wsTable[data[i]] {
		i++
	}
	return i
}

// SkipValueAt skips a complete JSON value starting at data[i]. Returns
// the index past the value and true, or (i, false) on error.
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

// skipScalar scans a bare JSON scalar (number/bool/null). It stops at
// the first separator (',', '}', ']', whitespace) and returns the
// boundary position. Content is not validated beyond framing.
func skipScalar(data []byte, i int) (int, bool) {
	for j := i; j < len(data); j++ {
		switch data[j] {
		case ',', '}', ']', ' ', '\n', '\r', '\t':
			return j, j > i
		}
	}
	return len(data), len(data) > i
}

// SkipStringAt finds the end of a JSON string starting at data[i].
// data[i] must be '"'. Returns the index past the closing quote. Rejects
// raw control chars (< 0x20) per RFC 8259.
func SkipStringAt(data []byte, i int) (int, bool) {
	if i >= len(data) || data[i] != '"' {
		return i, false
	}
	n := len(data)
	j := swarSkipStringBulk(data, i+1, n)
	return scanStringTail(data, j, n)
}

// swarSkipStringBulk advances j past 8-byte words that contain no '"',
// '\\', or control byte, stopping at the first word that does.
//
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

// scanStringTail walks the remainder of a JSON string byte-by-byte,
// handling escape sequences and rejecting raw control chars.
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

// SkipBracedAt skips a balanced delimiter pair starting at data[i].
// Delegates string scanning to SkipStringAt for SWAR throughput. For
// non-string, non-bracket content, uses SWAR to skip 8 bytes at a time.
func SkipBracedAt(data []byte, i int, opener, closer byte) (int, bool) {
	if i >= len(data) || data[i] != opener {
		return i, false
	}
	depth := 1
	i++
	n := len(data)
	swarOpener := swarLo * uint64(opener)
	swarCloser := swarLo * uint64(closer)

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

// stepBraced advances one byte within SkipBracedAt. done indicates the
// matching closer was reached; ok is false on a parse error.
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

// swarSkipBracedBulk advances i past 8-byte words that contain none of
// '"', '\\', opener, closer, stopping at the first word that does.
//
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

// iterateRawFields scans a JSON object and invokes fn for each field.
// Returns the index past the closing '}' and whether the scan completed.
// fn returning false aborts the scan; the aborted position is reported
// with ok=false.
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
// Positions are relative to the original data slice; ke sits just past
// the closing quote of the key and ve sits just past the final byte of
// the value (which is also the cursor position for the next field).
type fieldPos struct {
	ks, ke, vs, ve int
}

// parseField reads one "key":value pair starting at i. On success,
// returns the pair positions and ok=true. On failure, returns
// (_, i-position-of-error, false).
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

// stepAfterField consumes whitespace and the separator following a
// field value. done is true when the enclosing object's '}' is reached;
// ok is false on malformed input.
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

// IterateFields calls fn for each top-level key-value pair in a JSON
// object. key includes the surrounding quotes; value is the raw JSON
// bytes. Returns false if the JSON is malformed or fn returns false.
func IterateFields(data []byte, fn func(key, value []byte) bool) bool {
	_, ok := iterateRawFields(data, func(d []byte, ks, ke, vs, ve int) bool {
		return fn(d[ks:ke], d[vs:ve])
	})
	return ok
}

// IterateFieldsString is IterateFields over a string, avoiding the
// []byte conversion. The slices passed to fn alias the string's backing
// memory and must not be mutated.
//
//nolint:gosec // unsafe usage intentional: zero-alloc string→[]byte view
func IterateFieldsString(s string, fn func(key, value []byte) bool) bool {
	if s == "" {
		return false
	}
	data := unsafe.Slice(unsafe.StringData(s), len(s))
	return IterateFields(data, fn)
}

// FindField looks up a top-level field by key in a JSON object. Returns
// the raw value bytes and true on match, or (nil, false) if not found
// or the JSON is malformed. Implemented as a direct scanner without a
// callback so the match-comparison and early-return are inlined.
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

// atObjectEnd reports whether the cursor is past the end of data or
// sitting on a closing '}'.
func atObjectEnd(data []byte, i int) bool {
	return i >= len(data) || data[i] == '}'
}

// matchesKey reports whether the quoted key at fp.ks..fp.ke equals key.
// The unquoted key is data[fp.ks+1 : fp.ke-1].
func matchesKey(data []byte, fp fieldPos, key string) bool {
	return fp.ke-2-fp.ks == len(key) && string(data[fp.ks+1:fp.ke-1]) == key
}

// FindFieldString is FindField over a string input, avoiding the
// []byte conversion allocation. The returned slice aliases the string's
// backing memory and must not be mutated.
//
//nolint:gosec // unsafe usage intentional: zero-alloc string→[]byte view
func FindFieldString(s, key string) ([]byte, bool) {
	if s == "" {
		return nil, false
	}
	data := unsafe.Slice(unsafe.StringData(s), len(s))
	return FindField(data, key)
}

// maxFlattenDepth bounds recursion in FlattenObject to defend against
// pathological input.
const maxFlattenDepth = 64

// FlattenObject recursively flattens a nested JSON object into the
// Builder's current object. Nested objects are recursed up to 64 levels
// deep; leaf values are emitted as top-level fields. Non-object input
// is silently skipped. Returns false if the JSON is malformed or the
// nesting exceeds the depth limit.
func FlattenObject(b *Builder, data []byte) bool {
	return flattenObject(b, data, 0)
}

func flattenObject(b *Builder, data []byte, depth int) bool {
	if depth > maxFlattenDepth {
		return false
	}
	i := SkipWS(data, 0)
	if i >= len(data) || data[i] != '{' {
		return true // non-object: skip silently
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

// iterateRawArray scans a JSON array and invokes fn for each element.
// Returns the index past the closing ']' and whether the scan completed.
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

// openArray consumes the opening '[' and any leading whitespace. It
// reports empty=true if the array closes immediately (next is the index
// past ']'); otherwise next points at the first element byte.
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

// stepAfterArrayElement consumes whitespace and the separator following
// an array element. done is true when the enclosing ']' is reached; ok
// is false on malformed input.
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

// IterateArray calls fn for each element in a JSON array. element is
// the raw JSON bytes of each value (string with quotes, number, bool,
// null, nested object, nested array). Returns false if the JSON is
// malformed or fn returns false.
func IterateArray(data []byte, fn func(element []byte) bool) bool {
	_, ok := iterateRawArray(data, fn)
	return ok
}

// IterateStringArray calls fn for each string element in a JSON array.
// val is a zero-allocation view aliasing the input; it is only valid
// for the duration of the callback. Use strings.Clone(val) in fn to
// retain the value beyond the call.
//
// Non-string elements cause the iteration to abort and return false.
//
//nolint:gosec // unsafe.String intentional: zero-alloc borrow into data
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

// IterateArrayString is IterateArray over a string, avoiding the
// []byte conversion. The slice passed to fn aliases the string's
// backing memory and must not be mutated.
//
//nolint:gosec // unsafe usage intentional: zero-alloc string→[]byte view
func IterateArrayString(s string, fn func(element []byte) bool) bool {
	if s == "" {
		return false
	}
	data := unsafe.Slice(unsafe.StringData(s), len(s))
	return IterateArray(data, fn)
}

// IterateStringArrayString is IterateStringArray over a string input.
// See IterateStringArray for val lifetime rules.
//
//nolint:gosec // unsafe usage intentional: zero-alloc string→[]byte view
func IterateStringArrayString(s string, fn func(val string) bool) bool {
	if s == "" {
		return false
	}
	data := unsafe.Slice(unsafe.StringData(s), len(s))
	return IterateStringArray(data, fn)
}

// IsStructuralJSON reports whether s is a structurally valid JSON object
// or array with no trailing content. Grammar is validated in a single
// pass: objects require well-formed key:value pairs with commas; arrays
// require well-formed values with commas. Zero allocation.
//
// Does NOT validate semantic correctness (e.g., duplicate keys, number
// formats, escape sequences beyond framing).
//
//nolint:gosec // unsafe.Slice: zero-alloc string→[]byte view; s is read-only
func IsStructuralJSON(s string) bool {
	if len(s) < 2 || (s[0] != '{' && s[0] != '[') {
		return false
	}
	data := unsafe.Slice(unsafe.StringData(s), len(s))
	var end int
	var ok bool
	if data[0] == '{' {
		end, ok = iterateRawFields(data, nopFieldCallback)
	} else {
		end, ok = iterateRawArray(data, nopArrayCallback)
	}
	return ok && end == len(data)
}

// nopFieldCallback / nopArrayCallback are package-level no-op callbacks
// used by IsStructuralJSON. Declaring them at package scope prevents
// per-call closure construction and keeps IsStructuralJSON allocation-free
// even when its inputs are heap-backed.
var (
	nopFieldCallback = func(_ []byte, _, _, _, _ int) bool { return true }
	nopArrayCallback = func(_ []byte) bool { return true }
)
