package clientrelease

import "testing"

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
		want int
	}{
		{name: "patch number", a: "0.1.10", b: "0.1.9", want: 1},
		{name: "equal with missing segment", a: "1.2.0", b: "1.2", want: 0},
		{name: "stable beats prerelease", a: "1.0.0", b: "1.0.0-beta.1", want: 1},
		{name: "v prefix", a: "v2.0.0", b: "1.9.9", want: 1},
		{name: "older minor", a: "1.9.0", b: "1.10.0", want: -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CompareVersions(tt.a, tt.b)
			if tt.want == 0 && got != 0 {
				t.Fatalf("CompareVersions(%q, %q) = %d, want 0", tt.a, tt.b, got)
			}
			if tt.want > 0 && got <= 0 {
				t.Fatalf("CompareVersions(%q, %q) = %d, want > 0", tt.a, tt.b, got)
			}
			if tt.want < 0 && got >= 0 {
				t.Fatalf("CompareVersions(%q, %q) = %d, want < 0", tt.a, tt.b, got)
			}
		})
	}
}
