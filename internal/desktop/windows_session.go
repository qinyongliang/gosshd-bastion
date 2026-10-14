package desktop

import "errors"

// The native broker supplies only active sessions with a real logged-in token.
func selectWindowsInteractiveSession(console uint32, candidates []uint32) (uint32, error) {
	sessions := make(map[uint32]bool)
	for _, id := range candidates {
		if id != 0 && id != ^uint32(0) {
			sessions[id] = true
		}
	}
	if sessions[console] {
		return console, nil
	}
	if len(sessions) == 1 {
		for id := range sessions {
			return id, nil
		}
	}
	if len(sessions) == 0 {
		return 0, errors.New("no logged-in interactive Windows session is available")
	}
	return 0, errors.New("multiple active Windows sessions without an active console; refusing to choose another user's desktop")
}
