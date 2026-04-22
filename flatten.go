package jsonfast

import (
	"cmp"
	"slices"
)

// FlattenMap flattens a nested map into outer.inner → value form. If
// dst is nil a new map is allocated sized to the total entry count.
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

type flatEntry struct {
	outer, inner, val string
}

func compareFlatEntries(a, b flatEntry) int {
	if c := cmp.Compare(a.outer, b.outer); c != 0 {
		return c
	}
	return cmp.Compare(a.inner, b.inner)
}

// AddFlattenedMapField emits nested map data as sorted
// "outer.inner":"value" fields into the current object. Up to 16 total
// entries fit in a stack buffer; larger maps allocate once.
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
		b.escapeString(e.outer)
		b.buf = append(b.buf, '.')
		b.escapeString(e.inner)
		b.buf = append(b.buf, '"', ':', '"')
		b.escapeString(e.val)
		b.buf = append(b.buf, '"')
	}
}
