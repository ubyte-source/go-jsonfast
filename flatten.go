package jsonfast

import (
	"cmp"
	"slices"
)

// flatEntry holds an outer.inner=val triple for sorted flattened map emission.
type flatEntry struct {
	outer, inner, val string
}

// compareFlatEntries compares two flatEntry values by outer key, then inner key.
// Defined as a package-level function to avoid closure allocation in SortFunc.
func compareFlatEntries(a, b flatEntry) int {
	if c := cmp.Compare(a.outer, b.outer); c != 0 {
		return c
	}
	return cmp.Compare(a.inner, b.inner)
}

// FlattenMap flattens a nested map[string]map[string]string into a flat
// map[string]string using dot-notation keys: outer.inner → value.
// Zero allocation when the result map is provided and has sufficient capacity.
//
// Example:
//
//	nested := map[string]map[string]string{
//	    "sd@123": {"key1": "val1", "key2": "val2"},
//	}
//	flat := jsonfast.FlattenMap(nested, nil)
//	// flat = {"sd@123.key1": "val1", "sd@123.key2": "val2"}
func FlattenMap(m map[string]map[string]string, dst map[string]string) map[string]string {
	if len(m) == 0 {
		return dst
	}
	n := 0
	totalKeyLen := 0
	for outerKey, inner := range m {
		for innerKey := range inner {
			n++
			totalKeyLen += len(outerKey) + 1 + len(innerKey)
		}
	}
	if dst == nil {
		dst = make(map[string]string, n)
	}
	// Build all keys into one buffer, convert once to string.
	// Substrings of the resulting string share backing memory (fewer allocs).
	type span struct {
		val        string
		start, end int
	}
	var stackBuf [16]span
	spans := stackBuf[:0]
	if n > len(stackBuf) {
		spans = make([]span, 0, n)
	}
	keyBuf := make([]byte, 0, totalKeyLen)
	for outerKey, inner := range m {
		for innerKey, val := range inner {
			s := len(keyBuf)
			keyBuf = append(keyBuf, outerKey...)
			keyBuf = append(keyBuf, '.')
			keyBuf = append(keyBuf, innerKey...)
			spans = append(spans, span{val, s, len(keyBuf)})
		}
	}
	allKeys := string(keyBuf)
	for _, sp := range spans {
		dst[allKeys[sp.start:sp.end]] = sp.val
	}
	return dst
}

// AddFlattenedMapField writes nested map data as flattened "outer.inner":"value"
// fields directly into the JSON object being built, without materializing an
// intermediate flat map. Keys are sorted for deterministic output.
//
// Zero allocation for maps with ≤16 total entries (stack-allocated sort buffer).
func (b *Builder) AddFlattenedMapField(m map[string]map[string]string) {
	if len(m) == 0 {
		return
	}

	n := 0
	for _, inner := range m {
		n += len(inner)
	}

	var entries []flatEntry
	var buf [16]flatEntry
	if n <= len(buf) {
		entries = buf[:0]
	} else {
		entries = make([]flatEntry, 0, n)
	}

	for outerKey, inner := range m {
		for innerKey, val := range inner {
			entries = append(entries, flatEntry{outerKey, innerKey, val})
		}
	}

	slices.SortFunc(entries, compareFlatEntries)

	for _, e := range entries {
		b.sep()
		b.buf = append(b.buf, '"')
		b.buf = append(b.buf, e.outer...)
		b.buf = append(b.buf, '.')
		b.buf = append(b.buf, e.inner...)
		b.buf = append(b.buf, '"', ':', '"')
		b.escapeString(e.val)
		b.buf = append(b.buf, '"')
	}
}
