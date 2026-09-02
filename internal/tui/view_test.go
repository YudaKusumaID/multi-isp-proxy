package tui

import "testing"

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		bytes uint64
		want  string
	}{
		{bytes: 0, want: "0 B"},
		{bytes: 1023, want: "1023 B"},
		{bytes: 1024, want: "1.0 KB"},
		{bytes: 1536, want: "1.5 KB"},
		{bytes: 1024 * 1024, want: "1.0 MB"},
	}

	for _, test := range tests {
		if got := formatBytes(test.bytes); got != test.want {
			t.Fatalf("formatBytes(%d) = %q, want %q", test.bytes, got, test.want)
		}
	}
}
