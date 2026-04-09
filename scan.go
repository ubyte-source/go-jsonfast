package jsonfast

import "unsafe"

// wsTable marks whitespace bytes (space, \n, \r, \t) as true.
// Using a lookup table eliminates multi-branch checks in SkipWS.
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

// SkipValueAt skips a complete JSON value starting at data[i].
// Returns the index past the value and true, or (i, false) on error.
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
	default:
		for j := i; j < len(data); j++ {
			switch data[j] {
			case ',', '}', ']', ' ', '\n', '\r', '\t':
				return j, j > i
			}
		}
		return len(data), len(data) > i
	}
}

// SkipStringAt finds the end of a JSON string starting at data[i].
// data[i] must be '"'. Returns the index past the closing quote.
// Rejects raw control chars (< 0x20) per RFC 8259.
// Uses swarSpecialSkip (see swar.go) for the detection formula.
//
//nolint:gosec,gocognit,cyclop // unsafe for SWAR throughput; complexity from unrolled SWAR loop
func SkipStringAt(data []byte, i int) (int, bool) {
	if i >= len(data) || data[i] != '"' {
		return i, false
	}
	j := i + 1
	n := len(data)

	if n-j >= 8 {
		p := unsafe.Pointer(unsafe.SliceData(data))
		for j+16 <= n {
			w1 := *(*uint64)(unsafe.Add(p, j))
			if swarSpecialSkip(w1) != 0 {
				break
			}
			w2 := *(*uint64)(unsafe.Add(p, j+8))
			if swarSpecialSkip(w2) != 0 {
				j += 8
				break
			}
			j += 16
		}
		for j+8 <= n {
			w := *(*uint64)(unsafe.Add(p, j))
			if swarSpecialSkip(w) != 0 {
				break
			}
			j += 8
		}
	}

	for j < n {
		c := data[j]
		if c == '"' {
			return j + 1, true
		}
		if c == '\\' {
			if j+1 >= n {
				return j, false
			}
			j += 2
			continue
		}
		if c < 0x20 {
			return j, false
		}
		j++
	}
	return j, false
}

// SkipBracedAt skips a balanced delimiter pair starting at data[i].
// Delegates string scanning to SkipStringAt for SWAR throughput.
// For non-string, non-bracket content, uses SWAR to skip 8 bytes at a time.
//
//nolint:gosec,gocognit,cyclop // unsafe for SWAR throughput; complexity from SWAR+parser logic
func SkipBracedAt(data []byte, i int, opener, closer byte) (int, bool) {
	if i >= len(data) || data[i] != opener {
		return i, false
	}
	depth := 1
	i++
	n := len(data)
	p := unsafe.Pointer(unsafe.SliceData(data))

	// Pre-compute broadcast masks outside the loop to avoid repeated multiplies.
	swarOpener := swarLo * uint64(opener)
	swarCloser := swarLo * uint64(closer)

	for i < n {
		// SWAR: skip 8 bytes at a time when no special chars present.
		if n-i >= 8 {
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
					break
				}
				i += 8
			}
		}

		if i >= n {
			break
		}

		switch data[i] {
		case '"':
			end, ok := SkipStringAt(data, i)
			if !ok {
				return i, false
			}
			i = end
		case opener:
			depth++
			i++
		case closer:
			depth--
			if depth == 0 {
				return i + 1, true
			}
			i++
		default:
			i++
		}
	}
	return i, false
}

// iterateRawFields is the core field iterator. It parses a JSON object and
// calls fn(data, keyStart, keyEnd, valueStart, valueEnd) for each field.
// fn returns true to continue, false to stop. If fn returns false,
// iterateRawFields returns the index past the value and false.
// This single implementation eliminates the duplicated parsing logic
// that was previously in IterateFields, FindField, and flattenObject.
//
//nolint:gocognit,gocyclo,cyclop,funlen // single-pass JSON object scanner
func iterateRawFields(data []byte, fn func(data []byte, ks, ke, vs, ve int) bool) bool {
	i := SkipWS(data, 0)
	if i >= len(data) || data[i] != '{' {
		return false
	}
	i++

	for {
		i = SkipWS(data, i)
		if i >= len(data) {
			return false
		}
		if data[i] == '}' {
			return true
		}
		if data[i] != '"' {
			return false
		}

		keyStart := i
		keyEnd, ok := SkipStringAt(data, i)
		if !ok {
			return false
		}
		i = keyEnd

		i = SkipWS(data, i)
		if i >= len(data) || data[i] != ':' {
			return false
		}
		i++
		i = SkipWS(data, i)
		if i >= len(data) {
			return false
		}

		valueStart := i
		valueEnd, ok := SkipValueAt(data, i)
		if !ok {
			return false
		}
		i = valueEnd

		if !fn(data, keyStart, keyEnd, valueStart, valueEnd) {
			return false
		}

		i = SkipWS(data, i)
		if i >= len(data) {
			return false
		}
		switch data[i] {
		case ',':
			i++
		case '}':
			return true
		default:
			return false
		}
	}
}

// IterateFields calls fn for each top-level key-value pair in a JSON object.
// key includes quotes, value is the raw JSON bytes.
// Returns false if the JSON is malformed or fn returns false.
func IterateFields(data []byte, fn func(key, value []byte) bool) bool {
	return iterateRawFields(data, func(d []byte, ks, ke, vs, ve int) bool {
		return fn(d[ks:ke], d[vs:ve])
	})
}

// IterateFieldsString is like IterateFields but accepts a string,
// avoiding the []byte(string) allocation. The callback slices share
// the string's backing memory and must not be mutated.
//
//nolint:gosec // unsafe usage intentional: zero-alloc string→[]byte view
func IterateFieldsString(s string, fn func(key, value []byte) bool) bool {
	if s == "" {
		return false
	}
	data := unsafe.Slice(unsafe.StringData(s), len(s))
	return IterateFields(data, fn)
}

// FindField finds a top-level field by key name in a JSON object.
// Returns the raw value bytes and whether the field was found.
func FindField(data []byte, key string) ([]byte, bool) {
	keyWithQuotes := len(key) + 2
	var result []byte
	var found bool
	iterateRawFields(data, func(d []byte, ks, ke, vs, ve int) bool {
		if ke-ks == keyWithQuotes && string(d[ks+1:ke-1]) == key {
			result = d[vs:ve]
			found = true
			return false // stop iteration
		}
		return true
	})
	return result, found
}

// AddRawBytesField adds a "name":value field where name is raw bytes
// (without quotes) and value is raw JSON bytes. Zero allocation.
func (b *Builder) AddRawBytesField(name, value []byte) {
	b.sep()
	b.buf = append(b.buf, '"')
	b.buf = append(b.buf, name...)
	b.buf = append(b.buf, '"', ':')
	b.buf = append(b.buf, value...)
}

// maxFlattenDepth limits recursion in FlattenObject to prevent
// stack overflow from maliciously nested input.
const maxFlattenDepth = 64

// FlattenObject recursively flattens a nested JSON object into the Builder's
// current object. Nested objects are recursed up to 64 levels deep;
// leaf values are emitted as top-level fields. Non-object input is silently skipped.
// Returns false if the JSON is malformed or nesting exceeds the depth limit.
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
	parseOK := iterateRawFields(data, func(d []byte, ks, ke, vs, ve int) bool {
		valueRaw := d[vs:ve]
		if len(valueRaw) > 0 && valueRaw[0] == '{' {
			if !flattenObject(b, valueRaw, depth+1) {
				callbackOK = false
				return false
			}
		} else {
			b.AddRawBytesField(d[ks+1:ke-1], valueRaw)
		}
		return true
	})
	return parseOK && callbackOK
}
