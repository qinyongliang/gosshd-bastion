package android

import "strings"

// androidKeyCode maps the documented chord vocabulary onto Android key codes,
// which the input command accepts as decimal values.
func androidKeyCode(name string) (int, bool) {
	key := strings.ToLower(strings.TrimSpace(name))
	if key == "" {
		return 0, false
	}
	if code, ok := namedKeys[key]; ok {
		return code, true
	}
	if len(key) == 1 {
		character := key[0]
		if character >= 'a' && character <= 'z' {
			return 29 + int(character-'a'), true
		}
		if character >= '0' && character <= '9' {
			return 7 + int(character-'0'), true
		}
	}
	return 0, false
}

// Codes follow android.view.KeyEvent. Keys the shell surface cannot address
// (printable punctuation beyond comma/period/minus) are intentionally absent:
// use type_text for those characters.
var namedKeys = map[string]int{
	"ctrl":        113,
	"control":     113,
	"control_l":   113,
	"alt":         57,
	"alt_l":       57,
	"shift":       59,
	"shift_l":     59,
	"meta":        117,
	"command":     117,
	"super_l":     117,
	"return":      66,
	"enter":       66,
	"tab":         61,
	"escape":      111,
	"esc":         111,
	"space":       62,
	"backspace":   67,
	"delete":      112,
	"del":         112,
	"left":        21,
	"up":          19,
	"right":       22,
	"down":        20,
	"home":        3,
	"move_home":   122,
	"end":         123,
	"pageup":      92,
	"page_up":     92,
	"pagedown":    93,
	"page_down":   93,
	"comma":       55,
	"period":      56,
	"minus":       69,
	",":           55,
	".":           56,
	"-":           69,
	"volume_up":   24,
	"volume_down": 25,
	"power":       26,
	"camera":      27,
	"menu":        82,
	"search":      84,
	"back":        4,
}
