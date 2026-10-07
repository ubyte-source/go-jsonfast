package walkallocs

import (
	"testing"

	"github.com/ubyte-source/go-jsonfast"
)

// TestWalkStringsAllocs pins that WalkStrings allocates nothing for a document
// whose strings need no decoding.
func TestWalkStringsAllocs(t *testing.T) {
	doc := []byte(`{"a":["b",{"c":"d"}],"e":1,"f":"g"}`)
	fn := func([]byte) error { return nil }
	assertAllocs(t, 0, func() {
		if err := jsonfast.WalkStrings(doc, jsonfast.MaxDepth, fn); err != nil {
			t.Fatalf("WalkStrings = %v, want nil", err)
		}
	})
}
