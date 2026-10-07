package jsonfast

import (
	"hash/fnv"
	"strconv"
	"testing"
)

// addAll adds every name to s and reports how many adds said the name was new.
func addAll(s *nameSet, names []string) int {
	added := 0
	for _, name := range names {
		if s.add(name) {
			added++
		}
	}
	return added
}

func TestNameSetFindsEveryNameThroughItsProbes(t *testing.T) {
	for round := range repeats {
		names := make([]string, inlineNames+1)
		for i := range names {
			names[i] = strconv.Itoa(round) + "-name-" + strconv.Itoa(i)
		}
		var s nameSet
		if got := addAll(&s, names[:inlineNames]); got != inlineNames {
			t.Fatalf("round %d: %d of %d new names added, want all", round, got, inlineNames)
		}
		if got := addAll(&s, names[:inlineNames]); got != 0 {
			t.Fatalf("round %d: %d names added again, want none found missing in the table", round, got)
		}
		if !s.add(names[inlineNames]) || s.spill == nil || s.add(names[0]) || s.add(names[inlineNames]) {
			t.Fatalf("round %d: the spill lost a name, want every name found once", round)
		}
	}
}

// candidateNames bounds the search of collidingNames: far more names than it
// needs for a hash that spreads them over the slots.
const candidateNames = 1 << 16

// collidingNames returns n distinct names whose probes all start at the last
// slot, so they wrap around the table, or fewer when the search runs out.
func collidingNames(n int) []string {
	var out []string
	for i := 0; len(out) < n && i < candidateNames; i++ {
		if name := "n" + strconv.Itoa(i); slotOf(name) == nameSlots-1 {
			out = append(out, name)
		}
	}
	return out
}

// nameTableSlots is how many slots the table of a nameSet has: twice the 32
// names it holds inline.
const nameTableSlots = 64

func TestSlotOfIsFNV1aModuloTheTable(t *testing.T) {
	for i := range candidateNames {
		name := "n" + strconv.Itoa(i)
		h := fnv.New64a()
		if _, err := h.Write([]byte(name)); err != nil {
			t.Fatalf("FNV-1a Write(%q) = %v, want nil", name, err)
		}
		if got, want := slotOf(name), h.Sum64()%nameTableSlots; got != want {
			t.Fatalf("slotOf(%q) = %d, want FNV-1a modulo %d = %d", name, got, nameTableSlots, want)
		}
	}
}

// emptyInline reports whether s holds no name, no slot and no string inline.
func emptyInline(s *nameSet) bool {
	return s.n == 0 && s.slots == [nameSlots]uint8{} && s.inline == [inlineNames]string{}
}

func TestNameSetResetEmptiesTheSetAndKeepsItsMap(t *testing.T) {
	names := make([]string, inlineNames+1)
	for i := range names {
		names[i] = "reset-name-" + strconv.Itoa(i)
	}
	var s nameSet
	addAll(&s, names)
	s.reset()
	if !emptyInline(&s) || len(s.spill) != 0 || s.spill == nil {
		t.Fatalf("reset left %d inline names, a map of %d, map kept %v, want an empty set that keeps its map",
			s.n, len(s.spill), s.spill != nil)
	}
	if got := addAll(&s, names[:inlineNames]); got != inlineNames || len(s.spill) != 0 {
		t.Fatalf("after reset %d of %d names added, %d in the map, want all of them inline",
			got, inlineNames, len(s.spill))
	}
	assertAllocs(t, 0, func() {
		s.reset()
		if got := addAll(&s, names); got != len(names) || len(s.spill) != len(names) {
			t.Fatalf("after reset %d of %d names added, %d in the map, want all in the map",
				got, len(names), len(s.spill))
		}
	})
	if got := addAll(&s, names); got != 0 {
		t.Fatalf("the refilled map missed %d names, want none", got)
	}
}

// keptMapNames is the most names a map may have held for reset to keep it.
const keptMapNames = 1 << 10

func TestNameSetResetDropsAMapPastTheNamesItKeeps(t *testing.T) {
	names := make([]string, keptMapNames+1)
	for i := range names {
		names[i] = "kept-name-" + strconv.Itoa(i)
	}
	for _, n := range []int{keptMapNames, keptMapNames + 1} {
		var s nameSet
		addAll(&s, names[:n])
		s.reset()
		if kept, want := s.spill != nil, n == keptMapNames; kept != want || len(s.spill) != 0 || !emptyInline(&s) {
			t.Errorf("reset after %d names kept the map: %v, with %d names, want %v and an empty set",
				n, kept, len(s.spill), want)
		}
	}
}

func TestNameSetProbesPastCollisions(t *testing.T) {
	names := collidingNames(inlineNames)
	if len(names) != inlineNames {
		t.Fatalf("found %d names that hash to the last slot, want %d", len(names), inlineNames)
	}
	var s nameSet
	if got := addAll(&s, names); got != inlineNames || s.spill != nil {
		t.Fatalf("%d colliding names: %d added, spill %v, want all added and no spill",
			inlineNames, got, s.spill != nil)
	}
	if got := addAll(&s, names); got != 0 {
		t.Fatalf("the probe missed %d names past their colliding ones, want none", inlineNames-got)
	}
	// The probe runs forward and wraps: the second name takes the first slot.
	if s.slots[nameSlots-1] != 1 || s.slots[0] != 2 {
		t.Fatalf("the probe filled the last and the first slot with names %d and %d, want 1 and 2",
			s.slots[nameSlots-1], s.slots[0])
	}
}

func TestOpenObjectsPopGivesBackTheRoomOfTheNamesPushMarked(t *testing.T) {
	const before, inside, unmarked = "before", "inside", "unmarked"
	var a arena
	kept := a.keep([]byte(before), len(before+inside+unmarked))
	marked := openObjects{marks: []arenaMark{}}.push(&a)
	a.keep([]byte(inside), 0)
	if marked = marked.pop(&a); len(marked.sets) != 0 || len(marked.marks) != 0 || string(a.buf) != before {
		t.Fatalf("pop left %d sets, %d marks and %q in the arena, want none, none and %q",
			len(marked.sets), len(marked.marks), a.buf, before)
	}
	open := openObjects{}.push(&a)
	name := a.keep([]byte(unmarked), 0)
	if open = open.pop(&a); len(open.sets) != 0 || open.marks != nil || string(name) != unmarked ||
		string(kept) != before {
		t.Fatalf("pop without marks left %d sets, marks %v and %q, %q, want none, nil and %q, %q",
			len(open.sets), open.marks, kept, name, before, unmarked)
	}
}

func TestPushSetTakesOverTheMapOfAClosedSet(t *testing.T) {
	names := make([]string, inlineNames+1)
	for i := range names {
		names[i] = "name" + strconv.Itoa(i)
	}
	var inline [documentedNameSets]nameSet
	sets := pushSet(inline[:0])
	for _, name := range names {
		sets[0].add(name)
	}
	assertAllocs(t, 0, func() {
		sets = pushSet(sets[:0])
		for _, name := range names {
			if !sets[0].add(name) {
				t.Fatalf("the set pushed over a closed one holds %q already, want it empty", name)
			}
		}
	})
	full := pushSet(pushSet(inline[:0]))
	if grown := pushSet(full); len(grown) != documentedNameSets+1 || grown[len(grown)-1].spill != nil {
		t.Fatalf("pushSet on %d full sets gave %d sets, the last with map %v, want %d and a new set",
			len(full), len(grown), grown[len(grown)-1].spill != nil, documentedNameSets+1)
	}
}
