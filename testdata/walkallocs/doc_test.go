// Package walkallocs calls jsonfast.WalkStrings from outside package jsonfast,
// as every caller does, so its test counts the allocations a caller sees.
package walkallocs

import (
	"runtime/debug"
	"slices"
	"testing"
)

// raceEnabled reports whether the test binary runs the race detector.
func raceEnabled() bool {
	info, _ := debug.ReadBuildInfo()
	return info != nil && slices.Contains(info.Settings, debug.BuildSetting{Key: "-race", Value: "true"})
}

// assertAllocs fails t unless f allocates want times per run. The race
// detector changes allocation counts, so under it f runs once, unchecked.
func assertAllocs(t *testing.T, want float64, f func()) {
	t.Helper()
	if raceEnabled() {
		f()
		return
	}
	if got := testing.AllocsPerRun(100, f); got != want {
		t.Errorf("allocs per run = %.0f, want %.0f", got, want)
	}
}
