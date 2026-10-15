package server

import (
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Desktop runtime tools return one JSON text block, not structuredContent.
func desktopResult(t *testing.T, result *mcp.CallToolResult) any {
	t.Helper()
	for _, content := range result.Content {
		if text, ok := content.(*mcp.TextContent); ok {
			var out any
			if err := json.Unmarshal([]byte(text.Text), &out); err != nil {
				t.Fatal(err)
			}
			return out
		}
	}
	t.Fatal("missing desktop JSON result")
	return nil
}
