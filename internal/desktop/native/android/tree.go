package android

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/qinyongliang/gosshd-bastion/internal/desktop/contract"
)

type elementRef struct{ Element contract.Element }

func (h *Helper) dumpTree(ctx context.Context, origin contract.Bounds, pkg string, depth, limit int) ([]contract.Element, []elementRef, bool, error) {
	base := h.TempDir
	if base == "" {
		base = "/data/local/tmp"
	}
	if !filepath.IsAbs(base) {
		return nil, nil, false, errors.New("Android dump directory must be absolute")
	}
	dir, err := os.MkdirTemp(base, "gosshd-desktop-")
	if err != nil {
		return nil, nil, false, errors.New("cannot create private UI dump directory")
	}
	defer os.RemoveAll(dir)
	if err = os.Chmod(dir, 0700); err != nil {
		return nil, nil, false, err
	}
	name := filepath.Join(dir, "hierarchy.xml")
	if _, err = h.runTool(ctx, "uiautomator", "dump", name); err != nil {
		return nil, nil, false, err
	}
	data, err := h.runTool(ctx, "cat", name)
	if err != nil {
		return nil, nil, false, err
	}
	return parseTree(data, origin, pkg, depth, limit)
}
func parseTree(data []byte, origin contract.Bounds, pkg string, maxDepth, maxElements int) ([]contract.Element, []elementRef, bool, error) {
	if len(data) > maxOutputBytes {
		return nil, nil, false, errors.New("accessibility dump exceeds limit")
	}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	type level struct {
		index int
		skip  bool
	}
	stack := []level{}
	elements := []contract.Element{}
	refs := []elementRef{}
	truncated := false
	rootSeen := false
	rootClosed := false
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, false, errors.New("invalid accessibility XML")
		}
		switch t := token.(type) {
		case xml.StartElement:
			if rootClosed {
				return nil, nil, false, errors.New("multiple accessibility roots")
			}
			if !rootSeen {
				if t.Name.Local != "hierarchy" {
					return nil, nil, false, errors.New("unexpected accessibility root")
				}
				rootSeen = true
				continue
			}
			if t.Name.Local != "node" {
				return nil, nil, false, errors.New("unexpected accessibility element")
			}
			if len(stack) > 64 {
				return nil, nil, false, errors.New("accessibility nesting exceeds limit")
			}
			attrs := map[string]string{}
			for _, a := range t.Attr {
				attrs[a.Name.Local] = a.Value
			}
			parent := -1
			skip := false
			if len(stack) > 0 {
				parent = stack[len(stack)-1].index
				skip = stack[len(stack)-1].skip
			}
			if attrs["password"] == "true" || attrs["package"] != pkg {
				skip = true
			}
			if len(stack) > maxDepth {
				skip = true
				truncated = true
			}
			index := parent
			if !skip {
				b, ok := parseFrame(attrs["bounds"])
				if ok {
					visible := intersection(b, origin)
					if visible.Width > 0 && visible.Height > 0 {
						if len(elements) >= maxElements {
							return elements, refs, true, nil
						}
						actions := []string{}
						if attrs["enabled"] != "false" {
							if attrs["clickable"] == "true" {
								actions = append(actions, "invoke")
							}
							if attrs["long-clickable"] == "true" {
								actions = append(actions, "secondary")
							}
							if attrs["checkable"] == "true" && attrs["clickable"] == "true" {
								actions = append(actions, "toggle")
							}
							if attrs["scrollable"] == "true" {
								actions = append(actions, "scroll")
							}
						}
						name := attrs["content-desc"]
						if name == "" {
							name = attrs["text"]
						}
						index = len(elements)
						e := contract.Element{Index: index, Parent: parent, Role: androidRole(attrs["class"]), Name: limitText(name, 4096), Value: limitText(attrs["text"], 4096), AutomationID: limitText(attrs["resource-id"], 512), Enabled: attrs["enabled"] != "false", Focused: attrs["focused"] == "true", Bounds: contract.Bounds{X: visible.X - origin.X, Y: visible.Y - origin.Y, Width: visible.Width, Height: visible.Height}, Actions: actions}
						if v := attrs["selected"]; v == "true" || v == "false" {
							b := v == "true"
							e.Selected = &b
						}
						if attrs["checkable"] == "true" {
							e.ToggleState = "off"
							if attrs["checked"] == "true" {
								e.ToggleState = "on"
							}
						}
						elements = append(elements, e)
						refs = append(refs, elementRef{Element: e})
					}
				}
			}
			stack = append(stack, level{index: index, skip: skip})
		case xml.EndElement:
			if t.Name.Local == "hierarchy" {
				rootClosed = true
			}
			if t.Name.Local == "node" {
				if len(stack) == 0 {
					return nil, nil, false, errors.New("invalid accessibility hierarchy")
				}
				stack = stack[:len(stack)-1]
			}
		}
	}
	if !rootSeen || !rootClosed || len(stack) != 0 {
		return nil, nil, false, errors.New("incomplete accessibility hierarchy")
	}
	return elements, refs, truncated, nil
}
func intersection(a, b contract.Bounds) contract.Bounds {
	x, y := max(a.X, b.X), max(a.Y, b.Y)
	r, bot := min(a.X+a.Width, b.X+b.Width), min(a.Y+a.Height, b.Y+b.Height)
	if r <= x || bot <= y {
		return contract.Bounds{}
	}
	return contract.Bounds{X: x, Y: y, Width: r - x, Height: bot - y}
}
func androidRole(class string) string {
	name := class
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		name = name[i+1:]
	}
	name = strings.ToLower(name)
	switch {
	case strings.Contains(name, "edittext"):
		return "edit"
	case strings.Contains(name, "checkbox"), strings.Contains(name, "switch"):
		return "checkbox"
	case strings.Contains(name, "button"):
		return "button"
	case strings.Contains(name, "textview"):
		return "text"
	case strings.Contains(name, "scrollview"), strings.Contains(name, "recyclerview"), strings.Contains(name, "listview"):
		return "list"
	case strings.Contains(name, "webview"):
		return "document"
	case strings.Contains(name, "imageview"):
		return "image"
	default:
		return "group"
	}
}
func limitText(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}
