package desktop

import "testing"

func TestDesktopWindowsInteractiveSessionSelection(t *testing.T) {
	for _, tt := range []struct {
		name      string
		console   uint32
		ids       []uint32
		want      uint32
		wantError bool
	}{
		{name: "prefer active console", console: 1, ids: []uint32{2, 1, 3}, want: 1},
		{name: "single remote session", console: ^uint32(0), ids: []uint32{4}, want: 4},
		{name: "console not logged in", console: 1, ids: []uint32{2}, want: 2},
		{name: "never Session 0", console: 0, ids: []uint32{0}, wantError: true},
		{name: "no logged user", console: ^uint32(0), wantError: true},
		{name: "ambiguous remote sessions", console: ^uint32(0), ids: []uint32{2, 3}, wantError: true},
		{name: "duplicate enumeration", console: ^uint32(0), ids: []uint32{2, 2}, want: 2},
		{name: "invalid IDs filtered", console: ^uint32(0), ids: []uint32{0, ^uint32(0), 3}, want: 3},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := selectWindowsInteractiveSession(tt.console, tt.ids)
			if (err != nil) != tt.wantError || got != tt.want {
				t.Fatalf("session=%d error=%v; want %d error=%v", got, err, tt.want, tt.wantError)
			}
		})
	}
}
