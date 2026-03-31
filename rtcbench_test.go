package rtcbench

import "testing"

func TestExpandRoomName(t *testing.T) {
	tests := []struct {
		base   string
		offset int
		want   string
	}{
		// offset 0 always returns base unchanged
		{"room-1234", 0, "room-1234"},
		{"nodigits", 0, "nodigits"},

		// trailing digits are incremented
		{"room-1234", 1, "room-1235"},
		{"room-1234", 2, "room-1236"},
		{"room-1234", 100, "room-1334"},

		// single digit
		{"test0", 1, "test1"},
		{"test9", 1, "test10"},

		// all digits
		{"42", 0, "42"},
		{"42", 1, "43"},
		{"42", 8, "50"},

		// no trailing digits falls back to _N suffix
		{"nodigits", 1, "nodigits_1"},
		{"nodigits", 5, "nodigits_5"},
		{"room-abc", 2, "room-abc_2"},

		// empty string
		{"", 0, ""},
		{"", 1, "_1"},
	}

	for _, tt := range tests {
		t.Run(tt.base+"_"+string(rune('0'+tt.offset)), func(t *testing.T) {
			got := expandRoomName(tt.base, tt.offset)
			if got != tt.want {
				t.Errorf("expandRoomName(%q, %d) = %q, want %q",
					tt.base, tt.offset, got, tt.want)
			}
		})
	}
}
