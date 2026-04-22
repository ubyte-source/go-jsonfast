package jsonfast

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFlattenMap(t *testing.T) {
	tests := []struct {
		input map[string]map[string]string
		want  map[string]string
		name  string
	}{
		{
			name: "single outer key",
			input: map[string]map[string]string{
				"host": {"name": "fw01", "ip": "10.0.0.1"},
			},
			want: map[string]string{
				"host.name": "fw01",
				"host.ip":   "10.0.0.1",
			},
		},
		{
			name: "multiple outer keys",
			input: map[string]map[string]string{
				"src": {"ip": "1.2.3.4"},
				"dst": {"ip": "5.6.7.8"},
			},
			want: map[string]string{
				"src.ip": "1.2.3.4",
				"dst.ip": "5.6.7.8",
			},
		},
		{
			name:  "empty map",
			input: map[string]map[string]string{},
			want:  map[string]string{},
		},
		{
			name: "empty inner map",
			input: map[string]map[string]string{
				"outer": {},
			},
			want: map[string]string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FlattenMap(tt.input, nil)
			if len(got) != len(tt.want) {
				t.Fatalf("len(got)=%d, len(want)=%d\ngot: %v\nwant: %v",
					len(got), len(tt.want), got, tt.want)
			}
			for k, wantV := range tt.want {
				if gotV, ok := got[k]; !ok {
					t.Errorf("missing key %q", k)
				} else if gotV != wantV {
					t.Errorf("got[%q]=%q, want %q", k, gotV, wantV)
				}
			}
		})
	}
}

func TestFlattenMap_NilInput(t *testing.T) {
	got := FlattenMap(nil, nil)
	if got != nil {
		t.Errorf("Expected nil for nil input with nil dst, got %v", got)
	}
}

func TestAddFlattenedMapField(t *testing.T) {
	b := New(512)
	b.BeginObject()
	m := map[string]map[string]string{
		"host": {"name": "fw01"},
	}
	b.AddFlattenedMapField(m)
	b.EndObject()

	result := string(b.Bytes())
	assertContains(t, result, `"host.name":"fw01"`)
}

func TestAddFlattenedMapField_Empty(t *testing.T) {
	b := New(64)
	b.BeginObject()
	b.AddFlattenedMapField(map[string]map[string]string{})
	b.EndObject()
	expect(t, `{}`, string(b.Bytes()))
}

func TestFlattenMap_ManyEntries(t *testing.T) {
	m := map[string]map[string]string{
		"a": {}, "b": {}, "c": {}, "d": {},
	}
	for k := range m {
		for i := range 5 {
			m[k][string(rune('0'+i))] = "v"
		}
	}
	got := FlattenMap(m, nil)
	if len(got) != 20 {
		t.Errorf("expected 20 entries, got %d", len(got))
	}
}

func TestAddFlattenedMapField_ManyEntries(t *testing.T) {
	m := map[string]map[string]string{
		"a": {}, "b": {}, "c": {}, "d": {},
	}
	for k := range m {
		for i := range 5 {
			m[k][string(rune('0'+i))] = "v"
		}
	}
	b := New(1024)
	b.BeginObject()
	b.AddFlattenedMapField(m)
	b.EndObject()
	got := string(b.Bytes())
	count := strings.Count(got, `:"v"`)
	if count != 20 {
		t.Errorf("expected 20 fields, got %d in %s", count, got)
	}
}

func TestAddFlattenedMapField_SortedOutput(t *testing.T) {
	b := New(512)
	b.BeginObject()
	m := map[string]map[string]string{
		"z": {"k": "v1"},
		"a": {"k": "v2"},
		"m": {"k": "v3"},
	}
	b.AddFlattenedMapField(m)
	b.EndObject()

	result := string(b.Bytes())
	aIdx := strings.Index(result, "a.k")
	mIdx := strings.Index(result, "m.k")
	zIdx := strings.Index(result, "z.k")
	if mIdx <= aIdx || zIdx <= mIdx {
		t.Errorf("Expected sorted output: a.k < m.k < z.k, got %s", result)
	}
}

func TestAddFlattenedMapField_SpecialCharsInKey(t *testing.T) {
	b := New(256)
	b.BeginObject()
	m := map[string]map[string]string{
		"key\"with\\special\nchars": {"inner\tkey": "val"},
	}
	b.AddFlattenedMapField(m)
	b.EndObject()

	result := string(b.Bytes())
	var parsed map[string]string
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v\ngot: %s", err, result)
	}
	want := "key\"with\\special\nchars.inner\tkey"
	if parsed[want] != "val" {
		t.Errorf("expected decoded key %q → val, got %v", want, parsed)
	}
}

func TestAddFlattenedMapField_MultipleInnerKeys(t *testing.T) {
	b := New(512)
	b.BeginObject()
	m := map[string]map[string]string{
		"event": {
			"src":  "10.0.0.1",
			"dst":  "10.0.0.2",
			"type": "alert",
		},
	}
	b.AddFlattenedMapField(m)
	b.EndObject()

	result := string(b.Bytes())
	assertContains(t, result, `"event.dst":"10.0.0.2"`)
	assertContains(t, result, `"event.src":"10.0.0.1"`)
	assertContains(t, result, `"event.type":"alert"`)
}

func BenchmarkFlattenMap(b *testing.B) {
	m := map[string]map[string]string{
		"exampleSDID@32473": {
			"iut":         "3",
			"eventSource": "Application",
			"eventID":     "1011",
		},
		"examplePriority@32473": {
			"class": "high",
		},
	}

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		_ = FlattenMap(m, nil)
	}
}

func BenchmarkAddFlattenedMapField(b *testing.B) {
	builder := New(512)
	m := map[string]map[string]string{
		"exampleSDID@32473": {
			"iut":         "3",
			"eventSource": "Application",
			"eventID":     "1011",
		},
		"examplePriority@32473": {
			"class": "high",
		},
	}

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		builder.Reset()
		builder.BeginObject()
		builder.AddFlattenedMapField(m)
		builder.EndObject()
	}
}
