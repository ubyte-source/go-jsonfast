package jsonfast

// member locates one object member: its name token ends at nameEnd and its
// value spans valueStart to valueEnd.
type member struct {
	nameEnd, valueStart, valueEnd int
}

// IterateFields calls fn, which must not be nil, with the quoted key and raw value of
// each member of the object in data, in order, and returns fn's first error; input
// that is not one object is ErrMalformed, maybe after fn saw the earlier members.
func IterateFields[T Text](data T, fn func(key, value T) error) error {
	d := bytesOf(data)
	i, step := openContainer(d, 0, '{')
	for step == stepItem {
		m, ok := memberAt(d, i)
		if !ok {
			return ErrMalformed
		}
		if err := fn(view[T](d, i, m.nameEnd), view[T](d, m.valueStart, m.valueEnd)); err != nil {
			return err
		}
		i, step = nextItem(d, m.valueEnd, '}')
	}
	return step.closeErr(d, i)
}

// IterateMembers is IterateFields with each name decoded, or a view of data with no
// escape, and ErrDuplicateName at a repeat, after fn saw the earlier members. Names
// and values stay valid for as long as data does; fn must not write to them.
func IterateMembers[T Text](data T, fn func(name, value T) error) error {
	d := bytesOf(data)
	var (
		a     arena
		names nameSet
	)
	i, step := openContainer(d, 0, '{')
	for step == stepItem {
		name, m, ok := a.memberName(d, i, anyBody)
		if ok {
			m.valueEnd, ok = skipValueAt(d, m.valueStart)
		}
		switch {
		case !ok:
			return ErrMalformed
		case !names.add(textOf[string](name)):
			return ErrDuplicateName
		}
		if err := fn(textOf[T](name), view[T](d, m.valueStart, m.valueEnd)); err != nil {
			return err
		}
		i, step = nextItem(d, m.valueEnd, '}')
	}
	return step.closeErr(d, i)
}

// IterateObject is IterateMembers in one pass that accepts only an object
// ValidUTF8(data, maxDepth) accepts; ErrMalformed wins over ErrDuplicateName, and fn,
// which must not be nil, may have seen members before either fault.
func IterateObject[T Text](data T, maxDepth int, fn func(name, value T) error) error {
	return walkObject(data, maxDepth, nil, nil, fn)
}

// IterateDocument is IterateObject that also fails with ErrDuplicateName, in the same
// pass, when an object nested in a member's value repeats a name.
func IterateDocument[T Text](data T, maxDepth int, fn func(name, value T) error) error {
	var (
		sets  [inlineNameSets]nameSet
		marks [inlineNameSets]arenaMark
	)
	return walkObject(data, maxDepth, sets[:0], marks[:0], fn)
}

// objectWalk is the state of walkObject and WalkTokens: the member names walkObject
// hands out, decoded into arena, the names of the other objects, decoded into nested,
// and whether an object repeated a name.
type objectWalk struct {
	arena  arena
	nested arena
	dup    bool
}

// walkObject calls fn with each member of the object in data, checked in one pass by
// the ValidUTF8 rule; a repeated member name, or, given the stacks sets and marks, a
// name repeated in a member's value, is ErrDuplicateName unless ErrMalformed wins.
func walkObject[T Text](data T, maxDepth int, sets []nameSet, marks []arenaMark, fn func(name, value T) error) error {
	if maxDepth < 1 {
		return ErrMalformed
	}
	d := bytesOf(data)
	var (
		o     objectWalk
		names nameSet
	)
	open := openObjects{sets: sets, marks: marks}
	values := newWalker(d, maxDepth-1)
	i, step := openContainer(d, 0, '{')
	for step == stepItem {
		name, m, ok := o.arena.memberName(d, i, utf8Body)
		if ok {
			m.valueEnd, open, ok = o.valueEnd(values, m.valueStart, open)
		}
		switch {
		case !ok:
			return ErrMalformed
		case o.dup || !names.add(textOf[string](name)):
			return values.restMembers()
		}
		if err := fn(textOf[T](name), view[T](d, m.valueStart, m.valueEnd)); err != nil {
			return err
		}
		i, step = nextItem(d, m.valueEnd, '}')
	}
	return step.closeErr(d, i)
}

// valueEnd returns the index past the value at or after data[i], which w walks anew by
// the ValidUTF8 rule, and open as the value leaves it, or 0 and no stacks; with stacks,
// o.dup notes a name repeated in an object of the value, past which none are kept.
func (o *objectWalk) valueEnd(w *walker, i int, open openObjects) (end int, next openObjects, ok bool) {
	if open.sets == nil {
		end, ok = w.valueEnd(i)
		return end, open, ok
	}
	w.top(i)
	for !o.dup {
		var at int
		switch at, ok = w.token(); {
		case !ok:
			return 0, openObjects{}, false
		case at == endOfValue:
			return w.i, open, true
		}
		if open, _, ok = o.take(w, at, open); !ok {
			return 0, openObjects{}, false
		}
	}
	end, ok = w.finish()
	return end, openObjects{}, ok
}

// take files the token at data[at] that w passed, where w opened every object it
// closes, and returns open and a member name decoded: an object's opener pushes its
// set and its closer pops it; ok reports whether the ValidUTF8 rule accepts a string.
func (o *objectWalk) take(w *walker, at int, open openObjects) (next openObjects, name []byte, ok bool) {
	switch w.data[at] {
	case '{':
		return open.push(&o.nested), nil, true
	case '}':
		return open.pop(&o.nested), nil, true
	case '"':
		if name, ok = o.text(w, open.sets); !ok {
			return openObjects{}, nil, false
		}
	}
	return open, name, true
}

// text moves w past the string whose quote precedes w.i, reports whether the
// ValidUTF8 rule accepts it, and returns a member name decoded: it joins the last of
// sets, its object's, which w opened, and o.dup notes whether the object had it.
func (o *objectWalk) text(w *walker, sets []nameSet) (name []byte, ok bool) {
	at := w.i - 1
	if w.state != walkColon {
		w.i, ok = strictEnd(w.data, at)
		return nil, ok
	}
	name, w.i, ok = o.nested.strictToken(w.data, at)
	o.dup = ok && !sets[len(sets)-1].add(textOf[string](name))
	return name, ok
}

// WalkTokens calls fn, which must not be nil, with each scalar, string, opener and
// closer of the value in data as it lies, and the decoded name of the member it starts:
// ErrMalformed unless ValidUTF8(data, maxDepth), else ErrDuplicateName at a repeat.
func WalkTokens[T Text](data T, maxDepth int, fn func(name, token T) error) error {
	d := bytesOf(data)
	var (
		o             objectWalk
		sets          [inlineNameSets]nameSet
		name, decoded []byte
	)
	open := openObjects{sets: sets[:0]}
	w := newWalker(d, maxDepth)
	for !o.dup {
		at, ok := w.token()
		switch {
		case !ok:
			return ErrMalformed
		case at == endOfValue:
			return walkFault(d, w.i)
		}
		if open, decoded, ok = o.take(w, at, open); !ok {
			return ErrMalformed
		}
		if w.state == walkColon {
			name = decoded
			continue
		}
		if err := fn(textOf[T](name), view[T](d, at, w.i)); err != nil {
			return err
		}
		name = nil
	}
	return w.rest()
}

// IterateArray calls fn, which must not be nil, with each raw element of the array in
// data and returns fn's first error; malformed input is ErrMalformed, maybe after fn
// saw the elements before the fault.
func IterateArray[T Text](data T, fn func(elem T) error) error {
	d := bytesOf(data)
	i, step := openContainer(d, 0, '[')
	for step == stepItem {
		end, ok := skipValueAt(d, i)
		if !ok {
			return ErrMalformed
		}
		if err := fn(view[T](d, i, end)); err != nil {
			return err
		}
		i, step = nextItem(d, end, ']')
	}
	return step.closeErr(d, i)
}

// IterateStringArray calls fn, which must not be nil, with each string of the array in
// data, decoded, or a view of data with no escape, and returns fn's first error; a
// non-string element or other fault is ErrMalformed, maybe after fn saw earlier ones.
func IterateStringArray[T Text](data T, fn func(s T) error) error {
	d := bytesOf(data)
	var a arena
	i, step := openContainer(d, 0, '[')
	for step == stepItem {
		v, end, st := tokenAt(&a, d, i, arenaRoom, anyBody)
		if st == strMalformed {
			return ErrMalformed
		}
		if err := fn(textOf[T](v)); err != nil {
			return err
		}
		i, step = nextItem(d, end, ']')
	}
	return step.closeErr(d, i)
}

// FindMember returns the raw value of the first member of the object in data whose
// decoded name equals name. It stops at the match, so check a boundary object once
// with IterateObject, which refuses malformed input and repeated names.
func FindMember[T Text](data T, name string) (T, bool) {
	d := bytesOf(data)
	at, ok := memberStart(d, 0, bytesOf(name))
	end := 0
	if ok {
		end, ok = skipValueAt(d, at)
	}
	if !ok {
		var zero T
		return zero, false
	}
	return view[T](d, at, end), true
}

// memberStart returns the index of the value of the first member of the object at or
// after data[i] whose decoded name equals name, reading only the members before it;
// a value that starts past the end of data is not found.
func memberStart(data []byte, i int, name []byte) (int, bool) {
	i, step := openContainer(data, i, '{')
	for step == stepItem {
		end, equal, ok := matchName(data, i, name)
		var m member
		if ok {
			m, ok = colonAfter(data, end)
		}
		switch {
		case !ok:
			return 0, false
		case equal:
			return m.valueStart, m.valueStart < len(data)
		}
		if m.valueEnd, ok = skipValueAt(data, m.valueStart); !ok {
			return 0, false
		}
		i, step = nextItem(data, m.valueEnd, '}')
	}
	return 0, false
}

// matchName scans the string token at data[i], i at most len(data), once, and
// returns the index past it, whether it decodes to s, and whether it is well
// formed; past the first byte that s does not match, it only skips.
func matchName(data []byte, i int, s []byte) (end int, equal, ok bool) {
	if i == len(data) || data[i] != '"' {
		return 0, false, false
	}
	k, equal := matchedPrefix(data, i+1, s)
	if k == len(data) || data[k] != '"' {
		end, ok = stringEnd(data, k)
		return end, false, ok
	}
	return k + 1, equal, true
}

// FindElement returns the raw element at index of the array in data. It checks the
// elements only up to that one, so check a boundary array once with ValidUTF8.
func FindElement[T Text](data T, index int) (T, bool) {
	d := bytesOf(data)
	at, ok := elementStart(d, 0, index)
	end := 0
	if ok {
		end, ok = skipValueAt(d, at)
	}
	if !ok {
		var zero T
		return zero, false
	}
	return view[T](d, at, end), true
}

// elementStart returns the index of the element at index of the array at or after
// data[i], reading only the elements before it; one past the end of data is not found.
func elementStart(data []byte, i, index int) (int, bool) {
	if index < 0 {
		return 0, false
	}
	i, step := openContainer(data, i, '[')
	for n := 0; step == stepItem; n++ {
		if n == index {
			return i, i < len(data)
		}
		end, ok := skipValueAt(data, i)
		if !ok {
			return 0, false
		}
		i, step = nextItem(data, end, ']')
	}
	return 0, false
}

// scanStep is what a container walk meets at its position.
type scanStep uint8

const (
	stepItem      scanStep = iota // an item starts there
	stepEnd                       // the container closes there
	stepMalformed                 // anything else
)

// err maps the step that ends the walk of a value to its result.
func (s scanStep) err() error {
	if s == stepEnd {
		return nil
	}
	return ErrMalformed
}

// closeErr maps the step that ends the walk of a document, at its closer data[i] at
// stepEnd, to its result: only whitespace may follow the closer.
func (s scanStep) closeErr(data []byte, i int) error {
	if s == stepEnd && skipWS(data, i+1) == len(data) {
		return nil
	}
	return ErrMalformed
}

// openContainer finds the first item of the container that opener starts at or
// after data[i], past whitespace, or else returns the step that ends the walk and,
// at stepEnd, the index of the closer.
func openContainer(data []byte, i int, opener byte) (int, scanStep) {
	i = skipWS(data, i)
	if i == len(data) || data[i] != opener {
		return 0, stepMalformed
	}
	i = skipWS(data, i+1)
	if i < len(data) && data[i] == closerOf(opener) {
		return i, stepEnd
	}
	return i, stepItem
}

// nextItem moves past the separator that follows an item ending at data[i] and
// finds the next item, or else returns the step that ends the walk and, at stepEnd,
// the index of the closer.
func nextItem(data []byte, i int, closer byte) (int, scanStep) {
	i = skipWS(data, i)
	switch {
	case i == len(data):
		return 0, stepMalformed
	case data[i] == closer:
		return i, stepEnd
	case data[i] != ',':
		return 0, stepMalformed
	}
	return skipWS(data, i+1), stepItem
}

// memberAt reads the member whose name token starts at data[i].
func memberAt(data []byte, i int) (member, bool) {
	end, ok := skipStringAt(data, i)
	if !ok {
		return member{}, false
	}
	return memberValue(data, end)
}

// memberValue reads the colon and the value of the member whose name token ends
// at data[nameEnd].
func memberValue(data []byte, nameEnd int) (member, bool) {
	m, ok := colonAfter(data, nameEnd)
	if ok {
		m.valueEnd, ok = skipValueAt(data, m.valueStart)
	}
	if !ok {
		return member{}, false
	}
	return m, true
}

// colonAfter finds the colon after the member name token that ends at
// data[nameEnd]; m holds that end and the start of the value.
func colonAfter(data []byte, nameEnd int) (m member, ok bool) {
	j := skipWS(data, nameEnd)
	if j == len(data) || data[j] != ':' {
		return member{}, false
	}
	return member{nameEnd: nameEnd, valueStart: skipWS(data, j+1)}, true
}

// memberName decodes by the rule lowest the member name whose token starts at data[i]
// and finds its colon; the member holds the end of the token and the value's start.
func (a *arena) memberName(data []byte, i int, lowest rune) ([]byte, member, bool) {
	name, end, st := tokenAt(a, data, i, arenaRoom, lowest)
	if st != strMalformed {
		if m, ok := colonAfter(data, end); ok {
			return name, m, true
		}
	}
	return nil, member{}, false
}

// skipName checks by the ValidUTF8 rule, decoding nothing, the member name whose
// token starts at data[i] and finds its colon; the member holds the end of the
// token and the start of the value.
func skipName(data []byte, i int) (member, bool) {
	if i == len(data) || data[i] != '"' {
		return member{}, false
	}
	if end, ok := strictEnd(data, i); ok {
		return colonAfter(data, end)
	}
	return member{}, false
}
