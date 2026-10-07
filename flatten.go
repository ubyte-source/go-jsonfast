package jsonfast

const maxFlattenDepth = 64

// inlineNameSets is how many name sets of open objects FlattenObject, IterateDocument
// and WalkTokens keep on the goroutine stack, with IterateDocument's arena marks,
// before their stacks move to the heap.
const inlineNameSets = 2

// flattener writes the leaves of an object into b in the one walk that checks the
// object by the ValidUTF8 rule, and keeps decoded names to refuse a repeat: those
// of the leaves, and those of each open object, pending until it needs a set.
type flattener struct {
	b        *Builder
	data     []byte
	leaves   nameSet             // the decoded names of the leaves
	pending  [inlineNames]string // leaf names of the innermost object until it has a set
	arena    arena
	nameMark arenaMark
	nested   arena  // names of the objects outside the leaves until their parent closes
	key      []byte // the name of the member whose value comes next, as written
	name     string // that name decoded
	waiting  int    // how many names pending holds
	leaf     int    // how many containers of the leaf being written are open
	repeated bool   // an object or the leaves repeat a name
	comma    bool   // the next item of the innermost container of the leaf takes a comma
	named    bool   // the innermost object outside the leaves has a name set
}

// FlattenObject writes each leaf of data, an object or a value with none, into b by its
// key, value compacted; b must not be nil. It fails, b as it was, with ErrMalformed
// unless ValidUTF8(data, 64), or ErrDuplicateName at a repeated member or leaf name.
func FlattenObject[T Text](b *Builder, data T) error {
	d := bytesOf(data)
	if i := skipWS(d, 0); i == len(d) || d[i] != '{' {
		if !ValidUTF8(d, maxFlattenDepth) {
			return ErrMalformed
		}
		return nil
	}
	mark, needSep := len(b.buf), b.needSep
	f := flattener{b: b, data: d}
	err := f.run(newWalker(d, maxFlattenDepth))
	if err != nil {
		b.buf, b.needSep = b.buf[:mark], needSep
	}
	return err
}

// run writes the leaves token by token as w passes them, until a repeated name decides
// the result, and returns the fault of the object: ErrMalformed wins over a repeat. Its
// arrays hold inlineNameSets sets and every mark, one at most per object w holds open.
func (f *flattener) run(w *walker) error {
	var (
		sets  [inlineNameSets]nameSet
		marks [maxFlattenDepth]arenaMark
	)
	open := openObjects{sets: sets[:0], marks: marks[:0]}
	for !f.repeated {
		at, ok := w.token()
		switch {
		case !ok:
			return ErrMalformed
		case at == endOfValue:
			return walkFault(f.data, w.i)
		}
		if open, ok = f.take(w, at, open); !ok {
			return ErrMalformed
		}
	}
	return w.rest()
}

// take writes what the token at data[at] adds to the leaves, files the names it
// adds in open, and returns open; ok reports whether its string holds a code point
// for every rune.
func (f *flattener) take(w *walker, at int, open openObjects) (next openObjects, ok bool) {
	c := f.data[at]
	switch {
	case c == '"' && w.state == walkColon:
		return open, f.member(w, open.sets)
	case c == '{' || c == '}':
		return f.object(w, open), true
	case f.leaf == 0 && !f.leafName():
		open = f.setName(open)
	}
	return open, f.leafToken(w, at)
}

// leafToken writes the token at data[at] of a leaf: a string as it is, an opener or
// a closer of an array, or a scalar; it reports whether the ValidUTF8 rule accepts a
// string.
func (f *flattener) leafToken(w *walker, at int) bool {
	switch c := f.data[at]; c {
	case '"':
		return f.text(w)
	case '[':
		f.open(c)
	case ']':
		f.close(c)
	default:
		f.value(at, w.i)
	}
	return true
}

// object takes the opener or closer before w.i of an object. Outside the leaves it
// enters or leaves the object, which writes nothing: its members are flattened.
// Inside a leaf it writes the byte, and open pushes or pops the object w opened.
func (f *flattener) object(w *walker, open openObjects) openObjects {
	c := f.data[w.i-1]
	switch {
	case f.leaf == 0 && c == '{':
		open = f.enter(w, open)
	case f.leaf == 0:
		open = f.leave(open)
	case c == '{':
		f.open(c)
		return open.push(&f.arena)
	default:
		f.close(c)
		return open.pop(&f.arena)
	}
	return open
}

// enter opens an object outside the leaves, which w has entered. Its member name
// joins the set of the object around it, which needs one from then on, unless it
// is the top one; the new object has none.
func (f *flattener) enter(w *walker, open openObjects) openObjects {
	if w.depth > 1 {
		open = f.ensure(open)
		f.add(&open.sets[len(open.sets)-1], f.objectName(w))
	}
	f.waiting, f.named = 0, false
	return open
}

// objectName returns the name of the object w entered, which member decoded into
// arena past nameMark unless it is a view of data; a decoded one moves to nested,
// where it stays until the object around it closes.
func (f *flattener) objectName(w *walker) string {
	if f.arena.mark() == f.nameMark {
		return f.name
	}
	name := f.nested.keep(bytesOf(f.name), min(len(f.data)-w.i, arenaRoom))
	f.arena.rewind(f.nameMark)
	return textOf[string](name)
}

// leave closes the innermost object outside the leaves and drops its set, if it
// has one, and the room its names took in nested; the object around it has one,
// since it held this one.
func (f *flattener) leave(open openObjects) openObjects {
	if f.named {
		open = open.pop(&f.nested)
	}
	f.named = true
	return open
}

// leafName refuses the name of the leaf that starts when an earlier leaf has it,
// and reports whether it waits pending with the names of its object, which has no
// set yet and room there; otherwise setName files it.
func (f *flattener) leafName() bool {
	f.add(&f.leaves, f.name)
	if !f.named && f.waiting < len(f.pending) {
		f.pending[f.waiting] = f.name
		f.waiting++
		return true
	}
	return false
}

// setName files the name of the leaf that starts in the set of its object, which
// takes one unless it has one.
func (f *flattener) setName(open openObjects) openObjects {
	open = f.ensure(open)
	f.add(&open.sets[len(open.sets)-1], f.name)
	return open
}

// ensure gives the innermost object outside the leaves a name set holding its
// pending leaf names, which differ, as the leaves would repeat, and a mark of
// nested, which holds the names of its objects from then on, unless it has a set.
func (f *flattener) ensure(open openObjects) openObjects {
	if f.named {
		return open
	}
	open = open.push(&f.nested)
	for _, name := range f.pending[:f.waiting] {
		open.sets[len(open.sets)-1].add(name)
	}
	f.named = true
	return open
}

// add puts name into the set names and notes a repeat.
func (f *flattener) add(names *nameSet, name string) {
	if !names.add(name) {
		f.repeated = true
	}
}

// text writes the string value whose quote precedes w.i as it is, moves w past it,
// and reports whether the ValidUTF8 rule accepts it.
func (f *flattener) text(w *walker) bool {
	at := w.i - 1
	end, ok := strictEnd(f.data, at)
	if !ok {
		return false
	}
	w.i = end
	f.value(at, end)
	return true
}

// member decodes the name whose quote precedes w.i, moves w past it, and reports
// whether the ValidUTF8 rule accepts it. Outside the leaves it keys the next value;
// inside one it is written and joins the last of sets, its object's, which w opened.
func (f *flattener) member(w *walker, sets []nameSet) bool {
	at := w.i - 1
	if f.leaf == 0 {
		f.nameMark = f.arena.mark()
	}
	name, end, ok := f.arena.strictToken(f.data, at)
	if !ok {
		return false
	}
	w.i = end
	if f.leaf == 0 {
		f.key, f.name = f.data[at+1:end-1], textOf[string](name)
		return true
	}
	f.add(&sets[len(sets)-1], textOf[string](name))
	f.item()
	f.b.buf = append(append(f.b.buf, f.data[at:end]...), ':')
	f.comma = false
	return true
}

// item starts what comes next: the key of a leaf outside the leaves, or a
// comma inside a container that holds an item already.
func (f *flattener) item() {
	switch {
	case f.leaf == 0:
		f.b.AddRawBytesField(f.key, nil)
	case f.comma:
		f.b.buf = append(f.b.buf, ',')
	}
}

// value writes the scalar or string data[at:end], a leaf or an item of one.
func (f *flattener) value(at, end int) {
	f.item()
	f.b.buf = append(f.b.buf, f.data[at:end]...)
	f.comma = true
}

// open writes the opener c of a leaf or of a container inside one.
func (f *flattener) open(c byte) {
	f.item()
	f.b.buf = append(f.b.buf, c)
	f.leaf++
	f.comma = false
}

// close writes the closer c of a leaf or of a container inside one.
func (f *flattener) close(c byte) {
	f.b.buf = append(f.b.buf, c)
	f.leaf--
	f.comma = true
}
