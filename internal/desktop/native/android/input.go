package android

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/qinyongliang/gosshd-bastion/internal/desktop/contract"
)

func pointInside(x, y float64, b contract.Bounds) bool {
	return x >= 0 && y >= 0 && x < b.Width && y < b.Height
}
func elementBounds(o *observation, index int) (contract.Bounds, error) {
	ref, err := o.ref(index)
	if err != nil {
		return contract.Bounds{}, err
	}
	if !ref.Element.Enabled {
		return contract.Bounds{}, errors.New("element is disabled")
	}
	return ref.Element.Bounds, nil
}
func (h *Helper) click(ctx context.Context, o *observation, w windowInstance, p contract.Params) error {
	if p.Button != "" && p.Button != "left" {
		return errors.New("Android supports taps only; use secondary action for long press")
	}
	x, y := p.X, p.Y
	if p.ElementIndex != nil {
		b, err := elementBounds(o, *p.ElementIndex)
		if err != nil {
			return err
		}
		x, y = b.X+b.Width/2, b.Y+b.Height/2
	}
	if !pointInside(x, y, w.Bounds) {
		return errors.New("tap point is outside the observed window")
	}
	count := p.ClickCount
	if count == 0 {
		count = 1
	}
	for i := 0; i < count; i++ {
		if err := h.confirmTarget(ctx, w, o.displayBounds, [2]float64{x, y}); err != nil {
			return err
		}
		if _, err := h.runTool(ctx, "input", "tap", strconv.Itoa(int(w.Bounds.X+x)), strconv.Itoa(int(w.Bounds.Y+y))); err != nil {
			return err
		}
		if i+1 < count {
			timer := time.NewTimer(100 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	return nil
}
func (h *Helper) drag(ctx context.Context, o *observation, w windowInstance, p contract.Params) error {
	if !pointInside(p.X, p.Y, w.Bounds) || !pointInside(p.ToX, p.ToY, w.Bounds) {
		return errors.New("drag must stay inside the observed window")
	}
	return h.swipe(ctx, o, w, p.X, p.Y, p.ToX, p.ToY, 300)
}
func (h *Helper) scroll(ctx context.Context, o *observation, w windowInstance, p contract.Params) error {
	if p.DeltaX == 0 && p.DeltaY == 0 {
		return errors.New("scroll requires a non-zero delta")
	}
	b := contract.Bounds{Width: w.Bounds.Width, Height: w.Bounds.Height}
	x, y := p.X, p.Y
	if p.ElementIndex != nil {
		ref, err := o.ref(*p.ElementIndex)
		if err != nil {
			return err
		}
		if !containsString(ref.Element.Actions, "scroll") {
			return errors.New("element does not advertise scrolling")
		}
		b, err = elementBounds(o, *p.ElementIndex)
		if err != nil {
			return err
		}
		x, y = b.X+b.Width/2, b.Y+b.Height/2
	}
	if !pointInside(x, y, w.Bounds) {
		return errors.New("scroll point is outside the observed window")
	}
	// Clamp strictly inside the visible element and window, never at width/height.
	tx := min(max(x-scrollDistance(p.DeltaX, b.Width), b.X), b.X+b.Width-1)
	ty := min(max(y-scrollDistance(p.DeltaY, b.Height), b.Y), b.Y+b.Height-1)
	if int(tx) == int(x) && int(ty) == int(y) {
		return errors.New("scroll has no movement inside the target bounds")
	}
	return h.swipe(ctx, o, w, x, y, tx, ty, 240)
}
func (h *Helper) performAction(ctx context.Context, o *observation, w windowInstance, p contract.Params) error {
	ref, err := o.ref(*p.ElementIndex)
	if err != nil {
		return err
	}
	if !containsString(ref.Element.Actions, p.Action) {
		return errors.New("element does not advertise the requested gesture action")
	}
	b, err := elementBounds(o, *p.ElementIndex)
	if err != nil {
		return err
	}
	x, y := b.X+b.Width/2, b.Y+b.Height/2
	switch p.Action {
	case "invoke", "toggle":
		return h.click(ctx, o, w, contract.Params{ElementIndex: p.ElementIndex})
	case "secondary":
		return h.swipe(ctx, o, w, x, y, x, y, 700)
	default:
		return errors.New("Android shell backend has no semantic accessibility action for this operation")
	}
}
func (h *Helper) swipe(ctx context.Context, o *observation, w windowInstance, x, y, tx, ty float64, duration int) error {
	if !pointInside(x, y, w.Bounds) || !pointInside(tx, ty, w.Bounds) {
		return errors.New("gesture is outside observed bounds")
	}
	if err := h.confirmTarget(ctx, w, o.displayBounds, [2]float64{x, y}, [2]float64{tx, ty}); err != nil {
		return err
	}
	_, err := h.runTool(ctx, "input", "swipe", strconv.Itoa(int(w.Bounds.X+x)), strconv.Itoa(int(w.Bounds.Y+y)), strconv.Itoa(int(w.Bounds.X+tx)), strconv.Itoa(int(w.Bounds.Y+ty)), strconv.Itoa(duration))
	return err
}
func (h *Helper) pressKey(ctx context.Context, o *observation, w windowInstance, chord string) error {
	parts := strings.Split(chord, "+")
	if len(parts) > 5 {
		return errors.New("too many chord keys")
	}
	args := []string{"keyevent"}
	if len(parts) > 1 {
		// Old Android input has keyevent only; sequential events do not form chords.
		help, err := h.runTool(ctx, "input")
		if err != nil || !strings.Contains(string(help), "keycombination") {
			return errors.New("this Android input version does not support key combinations")
		}
		args[0] = "keycombination"
	}
	for i, part := range parts {
		code, ok := androidKeyCode(part)
		if !ok {
			return fmt.Errorf("unsupported key name %q", part)
		}
		if i < len(parts)-1 && code != 113 && code != 57 && code != 59 && code != 117 {
			return errors.New("chord prefixes must be modifiers")
		}
		args = append(args, strconv.Itoa(code))
	}
	if err := h.confirmTarget(ctx, w, o.displayBounds); err != nil {
		return err
	}
	_, err := h.runTool(ctx, "input", args...)
	return err
}
func (h *Helper) typeText(ctx context.Context, o *observation, w windowInstance, text string) error {
	// Validate every character before the first injection. No partial ASCII prefix
	// may be sent for an unsupported Unicode/newline/tab request.
	for _, r := range text {
		if r < 32 || r > 126 {
			return errors.New("Android shell type_text supports printable ASCII only; Unicode requires a separate on-device IME")
		}
	}
	if text == "" {
		return nil
	}
	send := func(value string) error {
		if err := h.confirmTarget(ctx, w, o.displayBounds); err != nil {
			return err
		}
		_, err := h.runTool(ctx, "input", "text", value)
		return err
	}
	// InputShellCommand rewrites %s into a space. Isolate each literal percent
	// as its own argv invocation to preserve arbitrary literal text.
	parts := strings.Split(text, "%")
	for i, value := range parts {
		if value != "" {
			if err := send(value); err != nil {
				return err
			}
		}
		if i < len(parts)-1 {
			if err := send("%"); err != nil {
				return err
			}
		}
	}
	return nil
}
func scrollDistance(delta, extent float64) float64 {
	if delta == 0 {
		return 0
	}
	distance := min(extent/3, max(1, extent-2))
	if delta < 0 {
		return -distance
	}
	return distance
}
func containsString(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
