package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/mbougarne/modawana/backend/internal/extension"
)

const toolArgumentsLimit = 16 << 10
const toolStringLimit = 4096

// ManifestTool describes a read-only tool exposed to explicitly scoped MCP clients.
type ManifestTool struct {
	Name        string                  `json:"name"`
	Description string                  `json:"description"`
	Properties  map[string]ToolProperty `json:"properties"`
	Required    []string                `json:"required,omitempty"`
}

type ToolProperty struct {
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

func (m Manifest) validateTools() error {
	if m.Capability("mcp_tools") != (len(m.MCPTools) > 0) || len(m.MCPTools) > 8 ||
		len(m.MCPTools) > 0 && m.HostAPIVersion < 3 {
		return fmt.Errorf("%w: MCP tools require API 3 and the mcp_tools capability", ErrManifest)
	}
	names := map[string]bool{}
	for _, tool := range m.MCPTools {
		if !capabilityName.MatchString(tool.Name) || names[tool.Name] || strings.TrimSpace(tool.Description) == "" ||
			!boundedText(tool.Description, 500) || len(tool.Properties) > 16 {
			return fmt.Errorf("%w: invalid MCP tool declaration", ErrManifest)
		}
		names[tool.Name] = true
		for name, property := range tool.Properties {
			if !capabilityName.MatchString(name) || !boundedText(property.Description, 160) {
				return fmt.Errorf("%w: invalid MCP tool property", ErrManifest)
			}
			switch property.Type {
			case "string", "integer", "number", "boolean":
			default:
				return fmt.Errorf("%w: invalid MCP tool property type", ErrManifest)
			}
		}
		required := map[string]bool{}
		for _, name := range tool.Required {
			if _, ok := tool.Properties[name]; !ok || required[name] {
				return fmt.Errorf("%w: invalid required MCP tool property", ErrManifest)
			}
			required[name] = true
		}
	}
	return nil
}

func (t ManifestTool) arguments(raw json.RawMessage) (map[string]json.RawMessage, error) {
	if len(raw) > toolArgumentsLimit {
		return nil, fmt.Errorf("MCP arguments exceed %d bytes", toolArgumentsLimit)
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return nil, fmt.Errorf("invalid MCP argument object")
	}
	for name, value := range fields {
		property, ok := t.Properties[name]
		if !ok || bytes.Equal(value, []byte("null")) || !property.accepts(value) {
			return nil, fmt.Errorf("invalid MCP argument")
		}
	}
	for _, name := range t.Required {
		if _, ok := fields[name]; !ok {
			return nil, fmt.Errorf("missing MCP argument")
		}
	}
	return fields, nil
}

func (p ToolProperty) accepts(raw json.RawMessage) bool {
	switch p.Type {
	case "string":
		var value string
		return json.Unmarshal(raw, &value) == nil && utf8.RuneCountInString(value) <= toolStringLimit
	case "integer":
		var value int32
		return json.Unmarshal(raw, &value) == nil
	case "number":
		var value float64
		return json.Unmarshal(raw, &value) == nil
	case "boolean":
		var value bool
		return json.Unmarshal(raw, &value) == nil
	}
	return false
}

func (x *wasmExtension) mcpTool(tool ManifestTool) extension.MCPTool {
	properties := make(map[string]any, len(tool.Properties))
	for name, property := range tool.Properties {
		schema := map[string]any{"type": property.Type}
		if property.Description != "" {
			schema["description"] = property.Description
		}
		switch property.Type {
		case "string":
			schema["maxLength"] = toolStringLimit
		case "integer":
			schema["minimum"] = -2147483648
			schema["maximum"] = 2147483647
		}
		properties[name] = schema
	}
	return extension.MCPTool{
		Name: x.Slug() + "__" + tool.Name, Description: tool.Description,
		Properties: properties, Required: tool.Required, ReadOnly: true, Idempotent: true, RequiresPluginScope: true,
		Call: func(ctx context.Context, raw json.RawMessage) ([]byte, error) {
			arguments, err := tool.arguments(raw)
			if err != nil {
				return nil, err
			}
			input, err := json.Marshal(struct {
				API       int                        `json:"api"`
				Kind      string                     `json:"kind"`
				Tool      string                     `json:"tool"`
				Arguments map[string]json.RawMessage `json:"arguments"`
			}{x.plugin.manifest.HostAPIVersion, "mcp", tool.Name, arguments})
			if err != nil {
				return nil, err
			}
			out, err := x.service.Runtime.invoke(ctx, x.plugin, input, hookResultLimit, pluginReadOnly, nil)
			if err != nil || !utf8.Valid(out) {
				x.service.logger().Warn("plugin MCP invocation failed", "plugin", x.Slug(), "tool", tool.Name)
				return json.Marshal(map[string]any{
					"isError": true, "content": []map[string]string{{"type": "text", "text": "Plugin tool failed."}},
				})
			}
			return json.Marshal(map[string]any{"content": []map[string]string{{"type": "text", "text": string(out)}}})
		},
	}
}
