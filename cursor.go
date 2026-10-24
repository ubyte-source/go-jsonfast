package jsonfast

// Cursor is a position at a JSON value in a document: a method reads only as far as its
// result needs, never past the value of a checked document. The zero Cursor holds no
// value, so every method fails on it and a chain of lookups checks only its last step.
type Cursor[T Text] struct {
	data []byte
	at   int
}

// CursorOf returns a Cursor at the value in data, past leading whitespace. A Cursor
// checks only the bytes it reads, so check a boundary document once with ValidUTF8, and
// with IterateDocument or WalkTokens where a repeated name must be refused.
func CursorOf[T Text](data T) Cursor[T] {
	d := bytesOf(data)
	return Cursor[T]{data: d, at: skipWS(d, 0)}
}

// Kind classifies the value at c by its first byte, reading nothing else; a Cursor
// that holds no value gives KindInvalid.
func (c Cursor[T]) Kind() Kind {
	if c.at < len(c.data) {
		return kindOfByte(c.data[c.at])
	}
	return KindInvalid
}

// FindMember returns a Cursor at the value of the first member of the object at c whose
// decoded name equals name, reading the members before it but not the value it finds.
func (c Cursor[T]) FindMember(name string) (Cursor[T], bool) {
	at, ok := memberStart(c.data, c.at, bytesOf(name))
	if !ok {
		return Cursor[T]{}, false
	}
	return Cursor[T]{data: c.data, at: at}, true
}

// FindElement returns a Cursor at the element at index of the array at c, reading the
// elements before it but not the element it finds.
func (c Cursor[T]) FindElement(index int) (Cursor[T], bool) {
	at, ok := elementStart(c.data, c.at, index)
	if !ok {
		return Cursor[T]{}, false
	}
	return Cursor[T]{data: c.data, at: at}, true
}

// Raw returns the value at c as it lies, a view of the document, with strings, numbers
// and literals checked in full and an array or object by its opener and closer only.
func (c Cursor[T]) Raw() (T, bool) {
	end, ok := skipValueAt(c.data, c.at)
	if !ok {
		var zero T
		return zero, false
	}
	return view[T](c.data, c.at, end), true
}

// IterateArray calls fn, which must not be nil, with a Cursor at each element of the
// array at c, in order, skipping each after fn, and returns fn's first error; a fault
// is ErrMalformed, maybe after fn saw the elements up to it.
func (c Cursor[T]) IterateArray(fn func(elem Cursor[T]) error) error {
	i, step := openContainer(c.data, c.at, '[')
	for step == stepItem {
		if err := fn(Cursor[T]{data: c.data, at: i}); err != nil {
			return err
		}
		end, ok := skipValueAt(c.data, i)
		if !ok {
			return ErrMalformed
		}
		i, step = nextItem(c.data, end, ']')
	}
	return step.err()
}

// IterateMembers calls fn, which must not be nil, with each name decoded by the
// IterateMembers rule and a Cursor at its value, skipping each value after fn; a fault
// is ErrMalformed, a repeat ErrDuplicateName, after fn saw earlier ones.
func (c Cursor[T]) IterateMembers(fn func(name T, value Cursor[T]) error) error {
	var (
		a     arena
		names nameSet
	)
	i, step := openContainer(c.data, c.at, '{')
	for step == stepItem {
		name, m, ok := a.memberName(c.data, i, anyBody)
		switch {
		case !ok:
			return ErrMalformed
		case !names.add(textOf[string](name)):
			return ErrDuplicateName
		}
		if err := fn(textOf[T](name), Cursor[T]{data: c.data, at: m.valueStart}); err != nil {
			return err
		}
		if m.valueEnd, ok = skipValueAt(c.data, m.valueStart); !ok {
			return ErrMalformed
		}
		i, step = nextItem(c.data, m.valueEnd, '}')
	}
	return step.err()
}
