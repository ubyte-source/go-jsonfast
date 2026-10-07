package jsonfast

// MaxDepth is the nesting limit of encoding/json, for callers that validate
// with the bound of json.Valid.
const MaxDepth = 10000

// ValidUTF8 reports whether data is exactly one JSON value, whitespace around it
// allowed, nested at most maxDepth arrays and objects deep, with every string valid
// UTF-8 and no lone surrogate escape, so no string decodes to a U+FFFD substitute.
func ValidUTF8[T Text](data T, maxDepth int) bool {
	d := bytesOf(data)
	end, ok := newWalker(d, maxDepth).finish()
	return ok && skipWS(d, end) == len(d)
}

// walkFault returns the fault of a walk whose top value ends at data[i]:
// ErrMalformed unless only whitespace follows, and else nil.
func walkFault(data []byte, i int) error {
	if skipWS(data, i) != len(data) {
		return ErrMalformed
	}
	return nil
}

// strictEnd returns the index past the string at data[at] read by the ValidUTF8 rule.
func strictEnd(data []byte, at int) (int, bool) {
	return bodyEnd[utf8Body](data, at+1)
}

// WalkStrings calls fn, which must not be nil, with every string of the value in data,
// names included, in order, decoded, or a view of data with no escape; it returns fn's
// error, or ErrMalformed unless ValidUTF8(data, maxDepth), maybe after earlier calls.
func WalkStrings[T Text](data T, maxDepth int, fn func(s T) error) error {
	return walkBytes(bytesOf(data), maxDepth, func(s []byte) error { return fn(textOf[T](s)) })
}

// walkBytes is WalkStrings on bytes. It is not generic, so its arena stays on
// its stack wherever WalkStrings is instantiated.
func walkBytes(d []byte, maxDepth int, fn func(s []byte) error) error {
	w := newWalker(d, maxDepth)
	var a arena
	for {
		i, ok := w.next()
		switch {
		case !ok:
			return ErrMalformed
		case i == endOfValue && skipWS(d, w.i) != len(d):
			return ErrMalformed
		case i == endOfValue:
			return nil
		}
		v, end, ok := a.strictToken(d, i)
		if !ok {
			return ErrMalformed
		}
		w.i = end
		if err := fn(v); err != nil {
			return err
		}
	}
}

// walkState is what the grammar allows at the walker's position.
type walkState uint8

const (
	walkValue      walkState = iota // a value
	walkValueOrEnd                  // a value or ']', right after '['
	walkName                        // a member name, after ',' in an object
	walkNameOrEnd                   // a member name or '}', right after '{'
	walkColon                       // ':', after a member name
	walkAfter                       // ',' or the closer, after a nested value
	walkDone                        // nothing more, after the top value
)

// endOfValue is the index next returns once the top value ends.
const endOfValue = -1

// Kinds of an open container, one bit each in a kindStack.
const (
	openArray  uint64 = 0
	openObject uint64 = 1
)

// Layout of a kindStack.
const (
	kindWordBits    = 64
	inlineKindWords = 4
)

// kindStack holds the kind of every open container, one bit per level. The
// first 256 levels live inline, and a spill that grows with the depth reached
// holds the deeper ones.
type kindStack struct {
	shallow [inlineKindWords]uint64
	deep    []uint64
}

// set records kind at depth, which is at least 0 and at most one level past the
// deepest one set.
func (s *kindStack) set(depth int, kind uint64) {
	word, bit := s.word(depth)
	*word = *word&^(1<<bit) | kind<<bit
}

// get returns the kind set at depth, which is at least 0 and at most the deepest
// one set.
func (s *kindStack) get(depth int) uint64 {
	word, bit := s.word(depth)
	return *word >> bit & 1
}

// word returns the word that holds the bit of depth and the bit's position; depth
// is at least 0 and at most one level past the deepest one set.
func (s *kindStack) word(depth int) (w *uint64, bit int) {
	k, bit := depth/kindWordBits, depth%kindWordBits
	if k < len(s.shallow) {
		return &s.shallow[k], bit
	}
	if k -= len(s.shallow); k == len(s.deep) {
		s.deep = append(s.deep, 0)
	}
	return &s.deep[k], bit
}

// walker checks the grammar of a JSON value iteratively, one value per walk, and
// stops at each string, which its caller scans before it resumes the walk at w.i.
type walker struct {
	data     []byte
	maxDepth int
	kinds    kindStack
	i        int
	depth    int
	state    walkState
}

// newWalker returns a walker of data that opens at most maxDepth nested arrays and
// objects; the pointer spares copying it.
func newWalker(data []byte, maxDepth int) *walker {
	w := &walker{data: data, maxDepth: maxDepth}
	w.top(0)
	return w
}

// top puts w before a top value at or after data[i], or at the end of data under a
// negative bound, which refuses every value; its kind stack keeps the spill it has.
func (w *walker) top(i int) {
	w.i, w.depth, w.state = i, 0, walkValue
	if w.maxDepth < 0 {
		w.i = len(w.data)
	}
}

// valueEnd returns the index past the value at or after data[i], whitespace
// before it allowed, walked anew from the top by the ValidUTF8 rule.
func (w *walker) valueEnd(i int) (int, bool) {
	w.top(i)
	return w.finish()
}

// next moves to the next string and returns the index of its opening quote, or
// endOfValue once the top value ends at w.i; ok is false on malformed or too deep
// input.
func (w *walker) next() (at int, ok bool) {
	for {
		if at, ok = w.token(); !ok || at == endOfValue || w.data[at] == '"' {
			return at, ok
		}
	}
}

// finish walks the rest of the value by the ValidUTF8 rule and returns the index
// past it.
func (w *walker) finish() (int, bool) {
	for {
		at, ok := w.next()
		switch {
		case !ok:
			return 0, false
		case at == endOfValue:
			return w.i, true
		}
		if w.i, ok = strictEnd(w.data, at); !ok {
			return 0, false
		}
	}
}

// rest walks the rest of the document by the ValidUTF8 rule, once a repeated name
// decided the result, and returns ErrMalformed unless it holds, and else
// ErrDuplicateName.
func (w *walker) rest() error {
	if end, ok := w.finish(); ok && walkFault(w.data, end) == nil {
		return ErrDuplicateName
	}
	return ErrMalformed
}

// restMembers walks by the ValidUTF8 rule the members after the value w ends at, once
// a repeated name decided the result, and returns ErrMalformed unless the object
// holds, and else ErrDuplicateName.
func (w *walker) restMembers() error {
	i, step := nextItem(w.data, w.i, '}')
	for step == stepItem {
		m, ok := skipName(w.data, i)
		if ok {
			m.valueEnd, ok = w.valueEnd(m.valueStart)
		}
		if !ok {
			return ErrMalformed
		}
		i, step = nextItem(w.data, m.valueEnd, '}')
	}
	if err := step.err(); err != nil {
		return err
	}
	return ErrDuplicateName
}

// token moves past the next token and returns the index of its first byte: a quote,
// whose string the caller scans and then resumes the walk past, an opener, a closer
// or a scalar ending at w.i; past the top value, endOfValue; ok is false on a fault.
func (w *walker) token() (at int, ok bool) {
	for w.state != walkDone {
		i := skipWS(w.data, w.i)
		if i == len(w.data) {
			return 0, false
		}
		w.i = i + 1
		switch c := w.data[i]; {
		case c == '"':
			ok = w.quote()
		case c == ':' && w.state == walkColon:
			w.state = walkValue
			continue
		case c == ',' && w.state == walkAfter:
			w.state = w.afterComma()
			continue
		default:
			ok = w.step()
		}
		if !ok {
			return 0, false
		}
		return i, true
	}
	return endOfValue, true
}

// afterComma returns what the grammar allows after a comma: a member name in an
// object, and a value in an array.
func (w *walker) afterComma() walkState {
	if w.inObject() {
		return walkName
	}
	return walkValue
}

// quote takes a quote, which opens a value or a member name.
func (w *walker) quote() bool {
	switch w.state {
	case walkValue, walkValueOrEnd:
		w.valueEnded()
	case walkName, walkNameOrEnd:
		w.state = walkColon
	default:
		return false
	}
	return true
}

// step takes the byte before w.i, which is not a quote, and reports whether the
// grammar allows it there.
func (w *walker) step() bool {
	switch w.state {
	case walkAfter:
		return w.after()
	case walkValue, walkValueOrEnd:
		return w.value()
	case walkNameOrEnd:
		return w.closeEmpty()
	default:
		return false
	}
}

// value takes the byte before w.i, where a value may start, other than a quote.
func (w *walker) value() bool {
	i := w.i - 1
	switch w.data[i] {
	case '{', '[':
		return w.open()
	case ']':
		return w.closeEmpty()
	}
	end, ok := skipScalar(w.data, i)
	w.i = end
	w.valueEnded()
	return ok
}

// after takes the byte before w.i that follows a nested value, other than a
// comma, which only the closer of the innermost container may be.
func (w *walker) after() bool {
	closer := byte(']')
	if w.inObject() {
		closer = '}'
	}
	if w.data[w.i-1] != closer {
		return false
	}
	w.close()
	return true
}

// open enters the container whose opener is the byte before w.i, unless that
// passes maxDepth.
func (w *walker) open() bool {
	if w.depth >= w.maxDepth {
		return false
	}
	kind, next := openArray, walkValueOrEnd
	if w.data[w.i-1] == '{' {
		kind, next = openObject, walkNameOrEnd
	}
	w.kinds.set(w.depth, kind)
	w.depth++
	w.state = next
	return true
}

// closeEmpty closes the container just opened, if the byte before w.i is its
// closer: the state is still the one the opener of that closer set.
func (w *walker) closeEmpty() bool {
	opened := walkValueOrEnd
	if w.data[w.i-1] == '}' {
		opened = walkNameOrEnd
	}
	if w.state != opened {
		return false
	}
	w.close()
	return true
}

// close leaves the innermost container, which ends a value.
func (w *walker) close() {
	w.depth--
	w.valueEnded()
}

// inObject reports whether the innermost open container, which must exist, is an
// object.
func (w *walker) inObject() bool {
	return w.kinds.get(w.depth-1) == openObject
}

// valueEnded moves past a value: to a separator or a closer inside a
// container, and to the end of the text at the top.
func (w *walker) valueEnded() {
	if w.depth == 0 {
		w.state = walkDone
		return
	}
	w.state = walkAfter
}
