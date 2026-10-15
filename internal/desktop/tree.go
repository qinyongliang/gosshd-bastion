package desktop

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ValidateElements requires an acyclic pre-order hierarchy. Native handles stay
// private to the helper; only bounded, observation-scoped indexes cross IPC.
func ValidateElements(elements []Element) error {
	if len(elements) > 1024 {
		return errors.New("desktop accessibility response exceeds element limit")
	}
	seen := map[int]bool{}
	for _, e := range elements {
		if e.Index < 0 || e.Index >= 1024 || seen[e.Index] || e.Parent == e.Index || (e.Parent != -1 && !seen[e.Parent]) {
			return errors.New("invalid desktop accessibility hierarchy")
		}
		if len(e.Name) > 16384 || len(e.Value) > 16384 || len(e.Help) > 4096 || len(e.Actions) > 16 {
			return errors.New("desktop accessibility text exceeds limit")
		}
		seen[e.Index] = true
	}
	return nil
}

// RenderTree makes the same hierarchy legible to text-only MCP hosts, without
// changing element indexes or deriving new action coordinates.
func RenderTree(elements []Element, truncated bool) string {
	if len(elements) == 0 {
		return ""
	}
	var out strings.Builder
	depths := map[int]int{}
	for _, e := range elements {
		depth := 0
		if e.Parent != -1 {
			depth = depths[e.Parent] + 1
		}
		depths[e.Index] = depth
		fmt.Fprintf(&out, "%s[%d] %s %s", strings.Repeat("  ", min(depth, 16)), e.Index, strings.TrimPrefix(e.Role, "ControlType."), strconv.Quote(e.Name))
		if e.Value != "" {
			fmt.Fprintf(&out, " value=%s", strconv.Quote(e.Value))
		}
		if e.Focused {
			out.WriteString(" focused")
		}
		if !e.Enabled {
			out.WriteString(" disabled")
		}
		if e.Selected != nil {
			fmt.Fprintf(&out, " selected=%t", *e.Selected)
		}
		if e.Expanded != nil {
			fmt.Fprintf(&out, " expanded=%t", *e.Expanded)
		}
		if e.ToggleState != "" {
			fmt.Fprintf(&out, " toggle=%s", e.ToggleState)
		}
		if e.Minimum != nil {
			fmt.Fprintf(&out, " min=%g", *e.Minimum)
		}
		if e.Maximum != nil {
			fmt.Fprintf(&out, " max=%g", *e.Maximum)
		}
		if e.VerticalScrollable != nil {
			fmt.Fprintf(&out, " vertical_scrollable=%t", *e.VerticalScrollable)
		}
		if e.HorizontalScrollable != nil {
			fmt.Fprintf(&out, " horizontal_scrollable=%t", *e.HorizontalScrollable)
		}
		if e.VerticalScrollPercent != nil {
			fmt.Fprintf(&out, " vertical_scroll_percent=%g", *e.VerticalScrollPercent)
		}
		if e.HorizontalScrollPercent != nil {
			fmt.Fprintf(&out, " horizontal_scroll_percent=%g", *e.HorizontalScrollPercent)
		}
		if len(e.Actions) > 0 {
			fmt.Fprintf(&out, " actions=%s", strings.Join(e.Actions, ","))
		}
		out.WriteByte('\n')
		if out.Len() > 64*1024 {
			truncated = true
			break
		}
	}
	if truncated {
		out.WriteString("[truncated; increase max_depth/max_elements within limits or inspect a screenshot]\n")
	}
	return out.String()
}
