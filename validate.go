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

// strictEnd returns the index past the string at data[at] read by the ValidUTF8 rule,
// utf8Body.
func strictEnd(data []byte, at int) (int, bool) {
	j := at + 1
	for {
		j = cleanRun(data, j)
		switch {
		case uint(j) >= uint(len(data)):
			return 0, false
		case data[j] == '"':
			return j + 1, true
		}
		if shortEscape(data, j) != 0 {
			j += shortEscapeLen
			continue
		}
		var ok bool
		if j, ok = runeEnd(data, j, utf8Body); !ok {
			return 0, false
		}
	}
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

// walkState is what the grammar allows at the walker's position. A value's state
// tells what follows the value: the end of the text, or the next item.
type walkState uint8

const (
	walkValue       walkState = iota // the top value
	walkArrayFirst                   // a value or ']', right after '['
	walkArrayValue                   // a value, after ',' in an array
	walkArrayAfter                   // ',' or ']', after a value in an array
	walkObjectFirst                  // a member name or '}', right after '{'
	walkName                         // a member name, after ',' in an object
	walkColon                        // ':', after a member name
	walkMemberValue                  // a member's value, after ':'
	walkObjectAfter                  // ',' or '}', after a member's value
	walkDone                         // nothing more, after the top value
)

// The slot of a token is its first byte folded into four bits, unique among the
// structural bytes; any other byte starts a scalar, whose slot no structural byte has.
const (
	slotMask       = 0xf
	slotScalar     = 0
	slotQuote      = ('"' ^ '"'>>5) & slotMask
	slotOpenObject = ('{' ^ '{'>>5) & slotMask
	slotColon      = (':' ^ ':'>>5) & slotMask
	slotComma      = (',' ^ ','>>5) & slotMask
	slotEnd        = 12 // the end of data, which every state refuses
	slotBits       = 4
)

// slotBytes holds each structural byte at its slot, and a quote, whose slot is
// slotQuote, at every other slot.
const slotBytes = `""""""""{[":",}]`

// A walkTable entry holds the state a token leads to and, in its high bits, the work
// the token takes first, where 0xf0 refuses the token; a closer's state is the one of
// the container around it.
const (
	stateMask  = 0x0f
	takeScalar = 0x10 // skip the scalar
	takeOpen   = 0x20 // enter a container
	takeClose  = 0x30 // leave a container
)

// walkTable holds the entry of each state and token slot at state<<slotBits|slot.
const walkTable = "" +
	"\x19\xf0\xf0\x09\xf0\xf0\xf0\xf0\x24\x21\xf0\xf0\xf0\xf0\xf0\xf0" + // walkValue
	"\x13\xf0\xf0\x03\xf0\xf0\xf0\xf0\x24\x21\xf0\xf0\xf0\xf0\xf0\x30" + // walkArrayFirst
	"\x13\xf0\xf0\x03\xf0\xf0\xf0\xf0\x24\x21\xf0\xf0\xf0\xf0\xf0\xf0" + // walkArrayValue
	"\xf0\xf0\xf0\xf0\xf0\xf0\xf0\xf0\xf0\xf0\xf0\xf0\xf0\x02\xf0\x30" + // walkArrayAfter
	"\xf0\xf0\xf0\x06\xf0\xf0\xf0\xf0\xf0\xf0\xf0\xf0\xf0\xf0\x30\xf0" + // walkObjectFirst
	"\xf0\xf0\xf0\x06\xf0\xf0\xf0\xf0\xf0\xf0\xf0\xf0\xf0\xf0\xf0\xf0" + // walkName
	"\xf0\xf0\xf0\xf0\xf0\xf0\xf0\xf0\xf0\xf0\xf0\x07\xf0\xf0\xf0\xf0" + // walkColon
	"\x18\xf0\xf0\x08\xf0\xf0\xf0\xf0\x24\x21\xf0\xf0\xf0\xf0\xf0\xf0" + // walkMemberValue
	"\xf0\xf0\xf0\xf0\xf0\xf0\xf0\xf0\xf0\xf0\xf0\xf0\xf0\x05\x30\xf0" // walkObjectAfter

// slotAt returns the slot of the token at data[i], or slotEnd at the end of data.
func slotAt(data []byte, i int) uint {
	if uint(i) >= uint(len(data)) {
		return slotEnd
	}
	c := data[i]
	if k := uint(c^c>>5) & slotMask; slotBytes[k] == c {
		return k
	}
	return slotScalar
}

// Slots at which the walker stops: each string, which its caller scans, or every
// token but a colon and a comma.
const (
	stopString uint16 = 1 << slotQuote
	stopToken         = ^uint16(1<<slotColon | 1<<slotComma)
)

// endOfValue is the index next returns once the top value ends.
const endOfValue = -1

// openObject is the kind of an open object in a kindStack, which holds the kind of a
// container as the low bit of its opener's slot.
const openObject uint64 = slotOpenObject & 1

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
	k, bit := uint(depth)/kindWordBits, uint(depth)%kindWordBits
	words := s.shallow[:]
	if k >= inlineKindWords {
		k -= inlineKindWords
		words = s.spill(k)
	}
	words[k] = words[k]&^(1<<bit) | kind<<bit
}

// spill returns the spill, grown to hold word k, which is at most one past its end.
func (s *kindStack) spill(k uint) []uint64 {
	if k == uint(len(s.deep)) {
		s.deep = append(s.deep, 0)
	}
	return s.deep
}

// get returns the kind set at depth, which is at least 0 and at most the deepest
// one set.
func (s *kindStack) get(depth int) uint64 {
	k, bit := uint(depth)/kindWordBits, uint(depth)%kindWordBits
	if k < inlineKindWords {
		return s.shallow[k] >> bit & 1
	}
	return s.deep[k-inlineKindWords] >> bit & 1
}

// closed returns the state past the container that closes at depth: the end of
// the text at the top, and else the next item of the container around it.
func (s *kindStack) closed(depth int) walkState {
	switch {
	case depth == 0:
		return walkDone
	case s.get(depth-1) == openObject:
		return walkObjectAfter
	}
	return walkArrayAfter
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
	return w.advance(stopString)
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
	if err := step.closeErr(w.data, i); err != nil {
		return err
	}
	return ErrDuplicateName
}

// token moves past the next token and returns the index of its first byte: a quote,
// whose string the caller scans and then resumes the walk past, an opener, a closer
// or a scalar ending at w.i; past the top value, endOfValue; ok is false on a fault.
func (w *walker) token() (at int, ok bool) {
	return w.advance(stopToken)
}

// advance moves past each token up to and including the next one whose slot is in
// stops, and returns the index of its first byte, as token does.
func (w *walker) advance(stops uint16) (at int, ok bool) {
	data, i, depth, state := w.data, w.i, w.depth, w.state
	for state != walkDone {
		at = skipWS(data, i)
		i = at + 1
		k := slotAt(data, at)
		entry := walkTable[uint(state)<<slotBits|k]
		state = walkState(entry & stateMask)
		if take := entry &^ stateMask; take != 0 {
			switch take {
			case takeScalar:
				if i, ok = skipScalar(data, at); !ok {
					return 0, false
				}
			case takeOpen:
				if depth >= w.maxDepth {
					return 0, false
				}
				w.kinds.set(depth, uint64(k&1))
				depth++
			case takeClose:
				depth--
				state = w.kinds.closed(depth)
			default:
				return 0, false
			}
		}
		if stops>>k&1 == 1 {
			w.i, w.depth, w.state = i, depth, state
			return at, true
		}
	}
	w.i, w.depth, w.state = i, depth, state
	return endOfValue, true
}
