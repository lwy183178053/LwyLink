package dbutil

import "testing"

func TestLimit(t *testing.T) {
	tests := []struct {
		name     string
		value    int
		fallback int
		max      int
		want     int
	}{
		{name: "fallback", value: 0, fallback: 50, max: 100, want: 50},
		{name: "keeps value", value: 40, fallback: 50, max: 100, want: 40},
		{name: "caps max", value: 120, fallback: 50, max: 100, want: 100},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Limit(tt.value, tt.fallback, tt.max); got != tt.want {
				t.Fatalf("Limit() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestOffset(t *testing.T) {
	if got := Offset(-1); got != 0 {
		t.Fatalf("Offset(-1) = %d, want 0", got)
	}
	if got := Offset(25); got != 25 {
		t.Fatalf("Offset(25) = %d, want 25", got)
	}
}
