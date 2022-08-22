package handler

import (
	"testing"
)

var samples = []struct { //nolint:gochecknoglobals
	in  string
	out []string
}{
	{"key", []string{"key"}},
	{"key[subkey]", []string{"key", "subkey"}},
	{"key[subkey]value", []string{"key", "subkey", "value"}},
	{"key[subkey][value]", []string{"key", "subkey", "value"}},
	{"key[subkey][value][]", []string{"key", "subkey", "value", ""}},
	{"key[subkey] [value][]", []string{"key", "subkey", "value", ""}},
	{"key [ subkey ] [ value ] [ ]", []string{"key", "subkey", "value", ""}},
	{"ключь [ subkey ] [ value ] [ ]", []string{"ключь", "subkey", "value", ""}}, // test non 1-byte symbols
}

func Test_FetchIndexes(t *testing.T) {
	for i := 0; i < len(samples); i++ {
		if !same(fetchIndexes(samples[i].in), samples[i].out) {
			t.Errorf("got %q, want %q", fetchIndexes(samples[i].in), samples[i].out)
		}
	}
}

func BenchmarkConfig_FetchIndexes(b *testing.B) {
	b.ReportAllocs()
	for _, tt := range samples {
		for n := 0; n < b.N; n++ {
			if !same(fetchIndexes(tt.in), tt.out) {
				b.Fail()
			}
		}
	}
}

func same(in, out []string) bool {
	if len(in) != len(out) {
		return false
	}

	for i := 0; i < len(in); i++ {
		if in[i] != out[i] {
			return false
		}
	}

	return true
}
