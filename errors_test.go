package jsonfast

import (
	"errors"
	"testing"
)

func TestErrDuplicateNameMatchesErrMalformed(t *testing.T) {
	if !errors.Is(ErrDuplicateName, ErrMalformed) || errors.Is(ErrMalformed, ErrDuplicateName) {
		t.Fatalf("errors.Is(ErrDuplicateName, ErrMalformed) = %v and the reverse %v, want true and false",
			errors.Is(ErrDuplicateName, ErrMalformed), errors.Is(ErrMalformed, ErrDuplicateName))
	}
	if got, want := ErrMalformed.Error(), "jsonfast: malformed JSON"; got != want {
		t.Errorf("ErrMalformed = %q, want %q", got, want)
	}
	if got, want := ErrDuplicateName.Error(), "jsonfast: malformed JSON: duplicate member name"; got != want {
		t.Errorf("ErrDuplicateName = %q, want %q", got, want)
	}
}
