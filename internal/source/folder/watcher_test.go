package folder

import (
	"testing"

	"athlete-log/internal/domain"
)

func TestIsSupportedFile(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"activity.fit", true},
		{"activity.FIT", true},
		{"route.gpx", true},
		{"route.GPX", true},
		{"workout.tcx", true},
		{"workout.TCX", true},
		{"readme.txt", false},
		{"image.jpg", false},
		{"data.json", false},
		{"noextension", false},
		{".fit", true}, // edge: only extension
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isSupportedFile(tc.name); got != tc.want {
				t.Fatalf("isSupportedFile(%q) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

func TestWatcher_Path(t *testing.T) {
	t.Run("returns path from metadata", func(t *testing.T) {
		w := &Watcher{conn: domain.SourceConnection{
			Metadata: map[string]any{"path": "/data/garmin"},
		}}
		if got := w.Path(); got != "/data/garmin" {
			t.Fatalf("Path() = %q, want %q", got, "/data/garmin")
		}
	})

	t.Run("returns empty when metadata missing", func(t *testing.T) {
		w := &Watcher{conn: domain.SourceConnection{}}
		if got := w.Path(); got != "" {
			t.Fatalf("Path() = %q, want empty", got)
		}
	})

	t.Run("returns empty when path is wrong type", func(t *testing.T) {
		w := &Watcher{conn: domain.SourceConnection{
			Metadata: map[string]any{"path": 42},
		}}
		if got := w.Path(); got != "" {
			t.Fatalf("Path() = %q, want empty", got)
		}
	})
}

func TestWatcher_Stop_Idempotent(t *testing.T) {
	w := &Watcher{stopCh: make(chan struct{})}
	// Calling Stop twice must not panic (second close on already-closed channel).
	w.Stop()
	w.Stop()
}
