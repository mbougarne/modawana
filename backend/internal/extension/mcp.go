package extension

import (
	"context"
	"encoding/json"
)

// ToolFunc handles tools/call for one tool. The arguments and the result are
// the bounded JSON documents of the MCP contract; a Wasm-backed tool will
// exchange the same bytes through the plugin's routes.
type ToolFunc func(ctx context.Context, arguments json.RawMessage) ([]byte, error)

// MCPTool declares one MCP tool. The host advertises Definition and enforces
// the declared argument names and required fields before calling Call.
type MCPTool struct {
	Name        string
	Description string
	// Properties and Required form the tool's object input schema; the host
	// always advertises and enforces additionalProperties false.
	Properties map[string]any
	Required   []string
	ReadOnly   bool
	Idempotent bool
	Call       ToolFunc

	slug string
}

// Definition is the tools/list entry for the tool.
func (t MCPTool) Definition() map[string]any {
	required := t.Required
	if required == nil {
		required = []string{}
	}
	return map[string]any{
		"name":        t.Name,
		"description": t.Description,
		"inputSchema": map[string]any{"type": "object", "properties": t.Properties, "required": required, "additionalProperties": false},
		"annotations": map[string]bool{"readOnlyHint": t.ReadOnly, "destructiveHint": !t.ReadOnly, "idempotentHint": t.Idempotent, "openWorldHint": false},
	}
}

// MCPTools returns every registered tool in installation order.
func (h *Host) MCPTools() []MCPTool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return append([]MCPTool(nil), h.tools...)
}

// MCPTool returns one registered tool by name.
func (h *Host) MCPTool(name string) (MCPTool, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	index, exists := h.indexed[name]
	if !exists {
		return MCPTool{}, false
	}
	return h.tools[index], true
}
