package jsonfast

import "reflect"

// valueDecoder builds the Go values of a document in the one walk that checks
// it by the ValidUTF8 rule.
type valueDecoder[T Text] struct {
	number   func(raw T) any
	data     []byte
	arena    arena
	top      any  // the value of the document, once the walk passed it
	repeated bool // an object repeats a name
	views    bool // decoded strings may alias data, which is immutable
}

// DecodeValue decodes data as encoding/json decodes into an any, each number by
// number, which must not be nil and may see numbers before a fault: ErrMalformed
// unless ValidUTF8(data, maxDepth), else ErrDuplicateName when a name repeats.
func DecodeValue[T Text](data T, maxDepth int, number func(raw T) any) (any, error) {
	d := bytesOf(data)
	vd := valueDecoder[T]{number: number, data: d, views: reflect.TypeFor[T]().Kind() == reflect.String}
	return vd.decode(newWalker(d, maxDepth))
}

// openValue is a container being decoded: its object, or else its array, and
// the name of the member whose value comes next.
type openValue struct {
	object map[string]any
	array  []any
	name   string
}

// openValueOf returns the empty container that the opener c starts.
func openValueOf(c byte) openValue {
	if c == '{' {
		return openValue{object: map[string]any{}}
	}
	return openValue{array: []any{}}
}

func (o *openValue) value() any {
	if o.object != nil {
		return o.object
	}
	return o.array
}

// add puts v into the container, under the pending name in an object.
func (o *openValue) add(v any) {
	if o.object != nil {
		o.object[o.name] = v
		return
	}
	o.array = append(o.array, v)
}

// inlineOpenValues is how many nested containers DecodeValue and the value
// writers keep open on the goroutine stack before their stack of them moves to
// the heap.
const inlineOpenValues = 16

// decode builds the document token by token as w passes them, until a repeated
// name decides the result. The containers it is inside sit on a stack of its
// own, so no nesting grows the goroutine stack.
func (vd *valueDecoder[T]) decode(w *walker) (any, error) {
	var stack [inlineOpenValues]openValue
	open := stack[:0]
	for !vd.repeated {
		at, ok := w.token()
		switch {
		case !ok:
			return nil, ErrMalformed
		case at == endOfValue:
			return vd.end(w.i)
		case vd.data[at] == '{' || vd.data[at] == '[':
			open = append(open, openValueOf(vd.data[at]))
		default:
			if open, ok = vd.place(w, at, open); !ok {
				return nil, ErrMalformed
			}
		}
	}
	return nil, w.rest()
}

// end returns the document, whose value ends at data[i], or its fault.
func (vd *valueDecoder[T]) end(i int) (any, error) {
	if err := walkFault(vd.data, i); err != nil {
		return nil, err
	}
	return vd.top, nil
}

// place puts the value the token at data[at] holds or closes in the last of open,
// the container of each closer or name w passes, or keeps it as the document; a
// name waits for its value. It reports whether a string holds only code points.
func (vd *valueDecoder[T]) place(w *walker, at int, open []openValue) ([]openValue, bool) {
	var v any
	switch vd.data[at] {
	case '}', ']':
		v, open = open[len(open)-1].value(), open[:len(open)-1]
	case '"':
		s, ok := vd.text(w)
		switch {
		case !ok:
			return nil, false
		case w.state == walkColon:
			vd.name(&open[len(open)-1], s)
			return open, true
		}
		v = s
	default:
		v = vd.scalar(at, w.i)
	}
	if len(open) == 0 {
		vd.top = v
	} else {
		open[len(open)-1].add(v)
	}
	return open, true
}

// name makes s the name of the member of o whose value comes next, and notes
// a name that o has already.
func (vd *valueDecoder[T]) name(o *openValue, s string) {
	if _, ok := o.object[s]; ok {
		vd.repeated = true
	}
	o.name = s
}

// scalar returns the literal or the number data[i:end].
func (vd *valueDecoder[T]) scalar(i, end int) any {
	switch vd.data[i] {
	case 't':
		return true
	case 'f':
		return false
	case 'n':
		return nil
	}
	return vd.number(view[T](vd.data, i, end))
}

// text decodes the string whose quote precedes w.i, moves w past it, and reports
// whether the ValidUTF8 rule accepts it. Every string of a byte slice input comes
// from the arena, since the caller may write to the input once DecodeValue returns.
func (vd *valueDecoder[T]) text(w *walker) (string, bool) {
	i := w.i - 1
	v, end, st := tokenAt[utf8Body](&vd.arena, vd.data, i, arenaRoom)
	switch {
	case st == strMalformed:
		return "", false
	case st == strView && !vd.views:
		v = vd.arena.keep(vd.data[i+1:end-1], min(len(vd.data)-i, arenaRoom))
	}
	w.i = end
	return textOf[string](v), true
}
