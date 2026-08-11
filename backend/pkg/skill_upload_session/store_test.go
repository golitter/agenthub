package skill_upload_session

import (
	"testing"
	"time"
)

func TestRedisTTLSecondsRoundsUpAndKeepsMinimum(t *testing.T) {
	tests := []struct {
		name string
		in   time.Duration
		want int64
	}{
		{name: "below minimum", in: 100 * time.Millisecond, want: 1},
		{name: "exact second", in: 2 * time.Second, want: 2},
		{name: "fractional second", in: 1500 * time.Millisecond, want: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := redisTTLSeconds(tt.in); got != tt.want {
				t.Fatalf("redisTTLSeconds(%s) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestStoreLeaseDurationNilSafe(t *testing.T) {
	var store *Store
	if got := store.LeaseDuration(); got != 0 {
		t.Fatalf("nil Store LeaseDuration() = %s, want 0", got)
	}
}
