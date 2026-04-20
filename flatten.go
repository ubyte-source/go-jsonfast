package jsonfast

import (
	"cmp"
	"slices"
)

// FlattenMap flattens a nested map[string]map[string]string into a flat
// map[string]string using dot-notation keys: outer.inner → value.
//
// If dst is nil, a new map with an exact capacity hint is allocated.
// If dst is non-nil and has sufficient capacity for all flattened keys,
// the only allocation is one string per entry (for the dot-joined key),
// driven by the map insertion itself.
//
// Example:
//
//	nested := map[string]map[string]string{
//	    "sd@123": {"key1": "val1", "key2": "val2"},
//	}
//	flat := jsonfast.FlattenMap(nested, nil)
//	// flat == {"sd@123.key1": "val1", "sd@123.key2": "val2"}
func FlattenMap(m map[string]map[string]string, dst map[string]string) map[string]string {
	if len(m) == 0 {
		return dst
	}
	if dst == nil {
		n := 0
		for _, inner := range m {
			n += len(inner)
		}
		dst = make(map[string]string, n)
	}
	for outerKey, inner := range m {
		for innerKey, val := range inner {
			dst[outerKey+"."+innerKey] = val
		}
	}
	return dst
}

// flatEntry holds an outer.inner=val triple for sorted flattened map
// emission inside AddFlattenedMapField.
type flatEntry struct {
	outer, inner, val string
}

// compareFlatEntries compares two flatEntry values by outer key, then
// inner key. Defined as a package-level function to avoid closure
// allocation at the SortFunc call site.
func compareFlatEntries(a, b flatEntry) int {
	if c := cmp.Compare(a.outer, b.outer); c != 0 {
		return c
	}
	return cmp.Compare(a.inner, b.inner)
}

// AddFlattenedMapField writes nested map data as flattened
// "outer.inner":"value" fields directly into the JSON object being built,
// without materializing an intermediate flat map. Keys are sorted for
// deterministic output.
//
// Zero allocation when the total number of flattened entries is at most
// 16 (stack-allocated sort buffer). Larger maps fall back to one heap
// allocation for the sort buffer.
func (b *Builder) AddFlattenedMapField(m map[string]map[string]string) {
	if len(m) == 0 {
		return
	}

	n := 0
	for _, inner := range m {
		n += len(inner)
	}

	var stack [16]flatEntry
	var entries []flatEntry
	if n <= len(stack) {
		entries = stack[:0]
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
