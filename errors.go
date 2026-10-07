package jsonfast

import (
	"errors"
	"fmt"
)

// errPrefix starts the text of the errors the package declares.
const errPrefix = "jsonfast: "

var (
	// ErrMalformed reports input that is not the JSON a walk expects.
	ErrMalformed = errors.New(errPrefix + "malformed JSON")
	// ErrDuplicateName reports a member whose decoded name an earlier member has.
	ErrDuplicateName = fmt.Errorf("%w: duplicate member name", ErrMalformed)
)
