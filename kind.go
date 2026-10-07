package jsonfast

// Kind classifies a JSON value.
type Kind uint8

// The kinds of JSON value that KindOf tells apart.
const (
	KindInvalid Kind = iota
	KindNull
	KindBool
	KindNumber
	KindString
	KindArray
	KindObject
)

// KindOf classifies the JSON value in raw by its first byte after whitespace,
// without validating the rest; blank input and any other byte give KindInvalid.
// On scanner values and validated documents, KindNull is exactly the null test.
func KindOf[T Text](raw T) Kind {
	d := bytesOf(raw)
	if i := skipWS(d, 0); i < len(d) {
		return kindOfByte(d[i])
	}
	return KindInvalid
}

// kindOfByte returns the kind of the value that c starts.
func kindOfByte(c byte) Kind {
	switch c {
	case 'n':
		return KindNull
	case 't', 'f':
		return KindBool
	case '"':
		return KindString
	case '[':
		return KindArray
	case '{':
		return KindObject
	}
	if c == '-' || isDigit(c) {
		return KindNumber
	}
	return KindInvalid
}
