package mcptools

import (
	"slices"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/qinyongliang/gosshd-bastion/internal/desktop"
)

// Each tool advertises only arguments it actually accepts. Required fields are
// checked before reaching the service, so malformed calls cannot consume a snapshot.
func methodSchema(method string) *jsonschema.Schema {
	schema, err := jsonschema.For[ToolInput](nil)
	if err != nil {
		panic(err)
	}
	params := schema.Properties["params"]
	params.Properties["window"].Types = nil
	params.Properties["window"].Type = "object"
	params.Properties["element_index"].Types = nil
	params.Properties["element_index"].Type = "integer"
	params.Properties["mode"].Enum = []any{"", "default", "auto", "background"}
	params.Properties["mode"].Description = "auto: semantic-first with foreground fallback; background: strict; omitted/default: compatibility."
	fields := []string{"mode"}
	var required []string
	if method == "get_window_state" || desktop.NeedsSnapshot(method) {
		fields = append(fields, "window")
		required = append(required, "window")
	}
	if desktop.NeedsSnapshot(method) {
		fields = append(fields, "snapshot_id")
		required = append(required, "snapshot_id")
		params.Properties["snapshot_id"].Description = "Latest get_window_state snapshot; one action only."
	}
	switch method {
	case "get_window_state":
		fields = append(fields, "include_text", "include_screenshot", "max_depth", "max_elements", "include_element_details")
	case "screenshot":
		fields = append(fields, "window", "display_id")
	case "launch_app":
		fields = append(fields, "app")
		required = append(required, "app")
	case "click":
		fields = append(fields, "element_index", "x", "y", "button", "click_count")
	case "press_key":
		fields = append(fields, "key")
		required = append(required, "key")
	case "type_text":
		fields = append(fields, "text")
		required = append(required, "text")
	case "set_value":
		fields = append(fields, "element_index", "text")
		required = append(required, "element_index", "text")
	case "perform_action":
		fields = append(fields, "element_index", "action")
		required = append(required, "element_index", "action")
		params.Properties["action"].Enum = []any{"invoke", "toggle", "expand", "collapse", "select", "focus", "secondary"}
	case "scroll":
		fields = append(fields, "element_index", "x", "y", "delta_x", "delta_y")
	case "drag":
		fields = append(fields, "x", "y", "to_x", "to_y")
		required = append(required, "x", "y", "to_x", "to_y")
	}
	for name := range params.Properties {
		if !slices.Contains(fields, name) {
			delete(params.Properties, name)
		}
	}
	params.Required = required
	if len(required) > 0 {
		schema.Required = append(schema.Required, "params")
	}
	if method == "click" || method == "scroll" {
		params.OneOf = []*jsonschema.Schema{
			{Required: []string{"element_index"}, Not: &jsonschema.Schema{AnyOf: []*jsonschema.Schema{{Required: []string{"x"}}, {Required: []string{"y"}}}}},
			{Required: []string{"x", "y"}, Not: &jsonschema.Schema{Required: []string{"element_index"}}},
		}
	}
	if method == "scroll" {
		params.AnyOf = []*jsonschema.Schema{{Required: []string{"delta_x"}}, {Required: []string{"delta_y"}}}
	}
	return schema
}
