package android

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/draw"
	"image/png"

	"github.com/google/uuid"
	"github.com/qinyongliang/gosshd-bastion/internal/desktop/contract"
)

const maxScreenshotSide = 1600

func (h *Helper) observe(ctx context.Context, p contract.Params) (any, error) {
	windows, size, dump, err := h.windowSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	if err = h.checkInteractive(ctx, dump); err != nil {
		return nil, err
	}
	target, err := matchWindow(windows, p.Window.ID, p.Window.App)
	if err != nil {
		return nil, err
	}
	if !target.Focused && (p.IncludeText || p.IncludeScreenshot == nil || *p.IncludeScreenshot) {
		return nil, errors.New("inactive Android window supports metadata only; observe with include_text false and include_screenshot false before activate_window")
	}
	depth, limit := p.MaxDepth, p.MaxElements
	if depth == 0 {
		depth = 8
	}
	if limit == 0 {
		limit = 512
	}
	state := contract.State{SnapshotID: uuid.NewString(), Window: windowObject(*target), Bounds: target.Bounds}
	var refs []elementRef
	if p.IncludeText {
		state.Elements, refs, state.Truncated, err = h.dumpTree(ctx, target.Bounds, target.Package, depth, limit)
		if err != nil {
			return nil, err
		}
	}
	if p.IncludeScreenshot == nil || *p.IncludeScreenshot {
		state.Image, err = h.capture(ctx, target.Bounds, size)
		if err != nil {
			return nil, err
		}
	}
	// Capture/dump can be slow. Reject a focus/geometry change during observation.
	if err = h.confirmTarget(ctx, *target, size); err != nil {
		return nil, err
	}
	h.observed = &observation{snapshot: state.SnapshotID, window: state.Window, frame: target.Bounds, displayBounds: size, at: h.now(), refs: refs}
	return state, nil
}
func (h *Helper) screenshot(ctx context.Context, p contract.Params) (any, error) {
	if p.DisplayID != "" && p.DisplayID != "0" {
		return nil, errPrimaryDisplay
	}
	windows, size, dump, err := h.windowSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	if err = h.checkInteractive(ctx, dump); err != nil {
		return nil, err
	}
	state := contract.State{SnapshotID: uuid.NewString(), Bounds: size}
	if p.Window != nil {
		target, e := matchWindow(windows, p.Window.ID, p.Window.App)
		if e != nil {
			return nil, e
		}
		if !target.Focused {
			return nil, errors.New("window is not focused; no unobscured Android window capture is available")
		}
		state.Window = windowObject(*target)
		state.Bounds = target.Bounds
	}
	state.Image, err = h.capture(ctx, state.Bounds, size)
	if err != nil {
		return nil, err
	}
	// This read deliberately never creates an actionable observation.
	return state, nil
}
func (h *Helper) confirmTarget(ctx context.Context, target windowInstance, size contract.Bounds, points ...[2]float64) error {
	windows, currentSize, dump, err := h.windowSnapshot(ctx)
	if err != nil {
		return err
	}
	if err = h.checkInteractive(ctx, dump); err != nil {
		return err
	}
	current, err := matchWindow(windows, target.Token, target.Package)
	if err != nil {
		return err
	}
	if current.Focused != target.Focused || current.Bounds != target.Bounds || currentSize != size {
		return errors.New("window focus or geometry changed; observe again")
	}
	if len(points) > 0 {
		left, top, right, bottom := points[0][0], points[0][1], points[0][0], points[0][1]
		for _, point := range points {
			left = min(left, point[0])
			top = min(top, point[1])
			right = max(right, point[0])
			bottom = max(bottom, point[1])
		}
		region := contract.Bounds{X: target.Bounds.X + left, Y: target.Bounds.Y + top, Width: right - left + 1, Height: bottom - top + 1}
		for _, w := range windows {
			if w.Token == target.Token {
				break
			}
			// A visible higher window can receive touch even while our target
			// retains keyboard focus (IME/overlays). Reject the gesture region.
			if clipped := intersection(region, w.Bounds); clipped.Width > 0 && clipped.Height > 0 {
				return errors.New("gesture is occluded by another visible window; observe again")
			}
		}
	}
	return nil
}
func decodePNG(data []byte) (image.Image, error) {
	if len(data) == 0 || len(data) > maxOutputBytes {
		return nil, errors.New("invalid screenshot size")
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 16384 || cfg.Height > 16384 || int64(cfg.Width)*int64(cfg.Height) > 64<<20 {
		return nil, errors.New("invalid screenshot dimensions or format")
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("invalid PNG screenshot")
	}
	return img, nil
}
func (h *Helper) capture(ctx context.Context, frame, size contract.Bounds) (*contract.Image, error) {
	data, err := h.runTool(ctx, "screencap", "-p")
	if err != nil {
		return nil, err
	}
	source, err := decodePNG(data)
	if err != nil {
		return nil, err
	}
	if source.Bounds().Dx() != int(size.Width) || source.Bounds().Dy() != int(size.Height) {
		return nil, errors.New("screenshot and logical display geometry differ; observe again")
	}
	if !insideDisplay(frame, size) {
		return nil, errors.New("capture bounds are outside display")
	}
	region := image.Rect(int(frame.X), int(frame.Y), int(frame.X+frame.Width), int(frame.Y+frame.Height))
	width, height := region.Dx(), region.Dy()
	if longest := max(width, height); longest > maxScreenshotSide {
		scale := float64(maxScreenshotSide) / float64(longest)
		width = max(1, int(float64(width)*scale))
		height = max(1, int(float64(height)*scale))
	}
	target := image.NewRGBA(image.Rect(0, 0, width, height))
	if width == region.Dx() && height == region.Dy() {
		draw.Draw(target, target.Bounds(), source, region.Min, draw.Src)
	} else {
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				target.Set(x, y, source.At(region.Min.X+x*region.Dx()/width, region.Min.Y+y*region.Dy()/height))
			}
		}
	}
	var out bytes.Buffer
	if err = png.Encode(&out, target); err != nil {
		return nil, err
	}
	if out.Len() > contract.MaxImage {
		return nil, errors.New("screenshot exceeds image budget")
	}
	return &contract.Image{MIMEType: "image/png", Data: out.Bytes(), Width: width, Height: height}, nil
}
