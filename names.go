package jsonfast

// A nameSet holds inlineNames names in a table of twice as many slots, so the
// table is at most half full and a probe stays short.
const (
	inlineNames = 32
	nameSlots   = 2 * inlineNames
)

// keptSpillNames is the most names a map may have held for reset to keep it:
// clearing a map takes time in proportion to its capacity.
const keptSpillNames = 1 << 10

// The offset basis and the prime of FNV-1a, the hash that picks the slot where
// the probe for a name starts.
const (
	fnvOffset = 14695981039346656037
	fnvPrime  = 1099511628211
)

// nameSet is a set of decoded member names: an object's, or FlattenObject's leaves.
// The first names live inline behind a seedless hash, since a table this small
// bounds every probe; more spill to a map, whose own seed covers long objects.
type nameSet struct {
	slots  [nameSlots]uint8 // 1 + the index in inline of the name hashed there, or 0
	inline [inlineNames]string
	n      uint8
	spill  map[string]struct{}
}

// add records name and reports whether no earlier name equals it.
func (s *nameSet) add(name string) bool {
	if len(s.spill) != 0 {
		if _, dup := s.spill[name]; dup {
			return false
		}
		s.spill[name] = struct{}{}
		return true
	}
	for k := slotOf(name); ; k = (k + 1) % nameSlots {
		switch at := s.slots[k]; {
		case at == 0:
			s.insert(name, k)
			return true
		case s.inline[at-1] == name:
			return false
		}
	}
}

// reset empties s and keeps its map, cleared, for the names it spills next,
// unless the map held more than keptSpillNames.
func (s *nameSet) reset() {
	spill := s.spill
	if len(spill) > keptSpillNames {
		spill = nil
	}
	clear(spill)
	*s = nameSet{spill: spill}
}

// insert records name, which no inline name equals, at the empty slot k, or
// spills every name to a map when the inline ones are full.
func (s *nameSet) insert(name string, k uint64) {
	if int(s.n) == len(s.inline) {
		if s.spill == nil {
			s.spill = make(map[string]struct{}, nameSlots)
		}
		for _, seen := range &s.inline {
			s.spill[seen] = struct{}{}
		}
		s.spill[name] = struct{}{}
		return
	}
	s.inline[s.n] = name
	s.n++
	s.slots[k] = s.n
}

// slotOf returns the slot where the probe for name starts.
func slotOf(name string) uint64 {
	h := uint64(fnvOffset)
	for i := range len(name) {
		h = (h ^ uint64(name[i])) * fnvPrime
	}
	return h % nameSlots
}

// openObjects holds the name sets of the objects open in a walk, the innermost last,
// and, unless marks is nil, for each object push opened, a mark of the arena whose
// room past it pop gives back.
type openObjects struct {
	sets  []nameSet
	marks []arenaMark
}

// push opens an object: a set, reset from one closed at its level, and a mark of a
// unless marks is nil.
func (o openObjects) push(a *arena) openObjects {
	o.sets = pushSet(o.sets)
	if o.marks != nil {
		o.marks = append(o.marks, a.mark())
	}
	return o
}

// pop closes the innermost object, which push opened, and gives back the room its
// names took in a, unless marks is nil.
func (o openObjects) pop(a *arena) openObjects {
	o.sets = o.sets[:len(o.sets)-1]
	if o.marks != nil {
		last := len(o.marks) - 1
		a.rewind(o.marks[last])
		o.marks = o.marks[:last]
	}
	return o
}

// pushSet returns sets with an empty set on top, reset from a set closed at that
// level if there is one, so sibling objects share the map they spill to as long as
// it holds at most keptSpillNames names.
func pushSet(sets []nameSet) []nameSet {
	if len(sets) == cap(sets) {
		return append(sets, nameSet{})
	}
	sets = sets[:len(sets)+1]
	sets[len(sets)-1].reset()
	return sets
}
