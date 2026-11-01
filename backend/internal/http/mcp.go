package httpserver

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/mbougarne/modawana/backend/internal/auth"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"regexp"
	"strings"
	"time"
)

// This adapter deliberately delegates every operation to the publisher HTTP
// boundary, including its transactional scope checks and idempotency ledger.
const mcpPath = "/api/v1/publisher/mcp"
const mcpVersion = "2026-07-28"

var mcpKey = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,119}$`)
var mcpGate = make(chan struct{}, 4)

type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func strictJSON(b []byte, out any) bool {
	if len(bytes.TrimSpace(b)) == 0 || bytes.TrimSpace(b)[0] != '{' {
		return false
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	return d.Decode(out) == nil && d.Decode(new(any)) == io.EOF
}

func (d *Domain) mcpRoutes(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != mcpPath {
		return false
	}
	w.Header().Set("Cache-Control", "no-store")
	if origin := r.Header.Get("Origin"); origin != "" && origin != d.Auth.Config.BaseURL {
		writeError(w, Forbidden)
		return true
	}
	if _, err := d.Auth.PublisherRequest(r); err != nil {
		w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+d.Auth.Config.BaseURL+`/.well-known/oauth-protected-resource", scope="publisher offline_access"`)
		domainError(w, err)
		return true
	}
	if r.Method != "POST" {
		w.Header().Set("Allow", "POST")
		writeError(w, MethodNotAllowed)
		return true
	}
	select {
	case mcpGate <- struct{}{}:
		defer func() { <-mcpGate }()
	default:
		writeError(w, RateLimited)
		return true
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(60 * time.Second))
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(65 * time.Second))
	reply := func(status, code int, id json.RawMessage, message string, data any) {
		if len(id) == 0 {
			id = json.RawMessage("null")
		}
		writeJSON(w, status, map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message, "data": data}})
	}
	ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if ct != "application/json" {
		writeError(w, UnsupportedMediaType)
		return true
	}
	if !strings.Contains(r.Header.Get("Accept"), "application/json") || !strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		reply(406, -32600, nil, "Accept must include application/json and text/event-stream", nil)
		return true
	}
	r.Body = http.MaxBytesReader(w, r.Body, 12<<20)
	defer r.Body.Close()
	b, err := io.ReadAll(r.Body)
	var request mcpRequest
	if err != nil || !strictJSON(b, &request) || request.JSONRPC != "2.0" || request.Method == "" {
		reply(400, -32600, nil, "Invalid bounded JSON-RPC request", nil)
		return true
	}
	var params struct {
		Meta         map[string]json.RawMessage `json:"_meta"`
		Name         string                     `json:"name"`
		Arguments    json.RawMessage            `json:"arguments"`
		Version      string                     `json:"protocolVersion"`
		Capabilities json.RawMessage            `json:"capabilities"`
		ClientInfo   json.RawMessage            `json:"clientInfo"`
		Cursor       string                     `json:"cursor"`
	}
	if len(request.Params) != 0 && !strictJSON(request.Params, &params) {
		reply(400, -32602, request.ID, "Invalid params", nil)
		return true
	}
	var bodyVersion string
	json.Unmarshal(params.Meta["io.modelcontextprotocol/protocolVersion"], &bodyVersion)
	version := r.Header.Get("MCP-Protocol-Version")
	modern := version == mcpVersion || bodyVersion != ""
	if modern {
		var clientCapabilities map[string]json.RawMessage
		if bodyVersion == "" || json.Unmarshal(params.Meta["io.modelcontextprotocol/clientCapabilities"], &clientCapabilities) != nil || clientCapabilities == nil {
			reply(400, -32602, request.ID, "Required per-request metadata missing", nil)
			return true
		}
		if version != bodyVersion || r.Header.Get("Mcp-Method") != request.Method || request.Method == "tools/call" && r.Header.Get("Mcp-Name") != params.Name {
			reply(400, -32020, request.ID, "Header mismatch", nil)
			return true
		}
	} else if request.Method == "initialize" {
		version = params.Version
	} else if version == "" {
		version = "2025-03-26"
	}
	supported := []string{mcpVersion, "2025-11-25", "2025-06-18", "2025-03-26"}
	validVersion := false
	for _, v := range supported {
		if version == v {
			validVersion = true
		}
	}
	if !validVersion {
		reply(400, -32022, request.ID, "Unsupported protocol version", map[string]any{"supported": supported, "requested": version})
		return true
	}
	if len(request.ID) == 0 {
		if !modern && request.Method == "notifications/initialized" {
			w.WriteHeader(202)
		} else {
			reply(400, -32600, nil, "Unsupported notification", nil)
		}
		return true
	}
	var id any
	if json.Unmarshal(request.ID, &id) != nil || id == nil {
		reply(400, -32600, nil, "Invalid request ID", nil)
		return true
	}
	switch id.(type) {
	case string, float64:
	default:
		reply(400, -32600, nil, "Invalid request ID", nil)
		return true
	}
	info := map[string]string{"name": "personal-blog-publisher", "version": "1.0.0"}
	capabilities := map[string]any{"tools": map[string]bool{"listChanged": false}}
	var result map[string]any
	switch request.Method {
	case "initialize":
		if modern {
			reply(404, -32601, request.ID, "Use server/discover", nil)
			return true
		}
		result = map[string]any{"protocolVersion": version, "capabilities": capabilities, "serverInfo": info}
	case "server/discover":
		result = map[string]any{"supportedVersions": supported, "capabilities": capabilities, "instructions": "Save drafts first. Reuse exact idempotency keys on retry. Never automatically increase expected versions. Publication requires an authorized exact saved revision. Quiz and translation reviews remain owner-only."}
	case "ping":
		result = map[string]any{}
	case "tools/list":
		if params.Cursor != "" {
			reply(400, -32602, request.ID, "Invalid cursor", nil)
			return true
		}
		result = map[string]any{"tools": mcpTools()}
	case "tools/call":
		result, err = d.mcpCall(r, params.Name, params.Arguments)
		if err != nil {
			reply(400, -32602, request.ID, "Unknown tool or invalid arguments", nil)
			return true
		}
	default:
		reply(404, -32601, request.ID, "Method not found", nil)
		return true
	}
	if modern {
		result["resultType"] = "complete"
		result["_meta"] = map[string]any{"io.modelcontextprotocol/serverInfo": info}
	}
	writeJSON(w, 200, map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
	return true
}

type mcpCapture struct {
	header http.Header
	status int
	bytes.Buffer
}

func (w *mcpCapture) Header() http.Header    { return w.header }
func (w *mcpCapture) WriteHeader(status int) { w.status = status }

func (d *Domain) mcpCall(r *http.Request, name string, raw json.RawMessage) (map[string]any, error) {
	// Enforce each advertised tool's argument names and required fields before
	// decoding the common envelope. Never silently ignore fields from another tool.
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return nil, io.ErrUnexpectedEOF
	}
	known := false
	for _, definition := range mcpTools() {
		tool := definition.(map[string]any)
		if tool["name"] != name {
			continue
		}
		known = true
		schema := tool["inputSchema"].(map[string]any)
		properties := schema["properties"].(map[string]any)
		for key := range fields {
			if _, ok := properties[key]; !ok {
				return nil, io.ErrUnexpectedEOF
			}
		}
		for _, key := range schema["required"].([]string) {
			if value, ok := fields[key]; !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return nil, io.ErrUnexpectedEOF
			}
		}
	}
	if !known {
		return nil, io.ErrUnexpectedEOF
	}
	var in struct {
		Key         string          `json:"key"`
		Locale      string          `json:"locale"`
		Idempotency string          `json:"idempotency_key"`
		Draft       json.RawMessage `json:"draft"`
		Publication json.RawMessage `json:"publication"`
		Filename    string          `json:"filename"`
		MIME        string          `json:"mime_type"`
		Data        string          `json:"data_base64"`
	}
	if !strictJSON(raw, &in) {
		return nil, io.ErrUnexpectedEOF
	}
	method, path, ct := "GET", "/api/v1/publisher/content/"+in.Key+"/"+in.Locale, "application/json"
	var body []byte
	if name != "upload_media" && (!mcpKey.MatchString(in.Key) || in.Locale != "en" && in.Locale != "ar" && in.Locale != "fr") {
		return nil, io.ErrUnexpectedEOF
	}
	if name != "get_draft" && (len(in.Idempotency) == 0 || len(in.Idempotency) > 160 || strings.ContainsAny(in.Idempotency, "\r\n")) {
		return nil, io.ErrUnexpectedEOF
	}
	switch name {
	case "get_draft":
	case "save_draft":
		method, body = "PUT", in.Draft
	case "publish_revision":
		method, body, path = "POST", in.Publication, path+"/publish"
	case "upload_media":
		if len(in.Filename) == 0 || len(in.Filename) > 200 || strings.ContainsAny(in.Filename, "/\\\r\n\"") || len(in.MIME) > 100 || strings.ContainsAny(in.MIME, "\r\n") {
			return nil, io.ErrUnexpectedEOF
		}
		data, e := base64.StdEncoding.Strict().DecodeString(in.Data)
		if e != nil || len(data) == 0 || len(data) > 8<<20 {
			return nil, io.ErrUnexpectedEOF
		}
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", `form-data; name="file"; filename="`+in.Filename+`"`)
		header.Set("Content-Type", in.MIME)
		part, e := mw.CreatePart(header)
		if e != nil {
			return nil, e
		}
		part.Write(data)
		mw.Close()
		method, path, ct, body = "POST", "/api/v1/publisher/media", mw.FormDataContentType(), buf.Bytes()
	default:
		return nil, io.ErrUnexpectedEOF
	}
	next := auth.MCPInternalRequest(r.Clone(r.Context()))
	u := *r.URL
	u.Path, u.RawPath, u.RawQuery = path, "", ""
	next.URL = &u
	next.Method, next.Body, next.ContentLength = method, io.NopCloser(bytes.NewReader(body)), int64(len(body))
	next.Header.Set("Content-Type", ct)
	next.Header.Set("Idempotency-Key", in.Idempotency)
	w := &mcpCapture{header: make(http.Header), status: 200}
	d.publisherRoutes(w, next)
	var payload any
	if json.Unmarshal(w.Bytes(), &payload) != nil {
		payload = map[string]any{"error": map[string]string{"code": "request_failed"}}
	}
	encoded, _ := json.Marshal(map[string]any{"status": w.status, "data": payload})
	return map[string]any{"content": []any{map[string]string{"type": "text", "text": string(encoded)}}, "isError": w.status >= 400}, nil
}

func mcpTools() []any {
	str := func(description string) map[string]any {
		return map[string]any{"type": "string", "description": description}
	}
	identity := func() map[string]any {
		return map[string]any{"key": map[string]any{"type": "string", "pattern": mcpKey.String()}, "locale": map[string]any{"type": "string", "enum": []string{"en", "ar", "fr"}}}
	}
	tool := func(name, description string, properties map[string]any, required []string, read bool) any {
		return map[string]any{"name": name, "description": description, "inputSchema": map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}, "annotations": map[string]bool{"readOnlyHint": read, "destructiveHint": !read, "idempotentHint": true, "openWorldHint": false}}
	}
	get := tool("get_draft", "Read a scoped private draft. Quiz-bearing drafts require quiz scope too.", identity(), []string{"key", "locale"}, true)
	save := identity()
	save["idempotency_key"] = str("Stable retry key; reuse with identical payload only")
	save["draft"] = mcpDraftSchema()
	pub := identity()
	pub["idempotency_key"] = str("Stable exact-publication retry key")
	pub["publication"] = map[string]any{"type": "object", "properties": map[string]any{"expected_version": map[string]string{"type": "integer"}, "revision_id": str("Exact saved revision ID")}, "required": []string{"expected_version", "revision_id"}, "additionalProperties": false}
	return []any{get, tool("save_draft", "Create/update a draft through existing scoped services, including optional immutable quiz. Stop on conflict; never bump versions automatically.", save, []string{"key", "locale", "idempotency_key", "draft"}, false), tool("publish_revision", "Publish an explicitly authorized saved revision with publish scope. Translation/assessment review remains required. No plan/gate changes.", pub, []string{"key", "locale", "idempotency_key", "publication"}, false), tool("upload_media", "Upload a base64 content attachment, maximum 8 MiB decoded. No remote URL fetching. Requires media scope; draft media stays private.", map[string]any{"filename": str("Plain filename"), "mime_type": str("Declared MIME type; bytes are validated"), "data_base64": str("Standard base64 encoded file"), "idempotency_key": str("Stable upload retry key")}, []string{"filename", "mime_type", "data_base64", "idempotency_key"}, false)}
}

func mcpDraftSchema() map[string]any {
	text := map[string]string{"type": "string"}
	integer := map[string]any{"type": "integer", "minimum": 0}
	stringsList := map[string]any{"type": "array", "items": text}
	option := map[string]any{"type": "object", "properties": map[string]any{"id": text, "text": text}, "required": []string{"id", "text"}, "additionalProperties": false}
	question := map[string]any{"type": "object", "properties": map[string]any{"id": text, "type": map[string]any{"type": "string", "enum": []string{"single", "multi", "bool"}}, "text": text, "options": map[string]any{"type": "array", "items": option, "minItems": 2, "maxItems": 8}, "correct": stringsList, "explanation": text}, "required": []string{"id", "type", "text", "options", "correct"}, "additionalProperties": false}
	quiz := map[string]any{"type": []string{"object", "null"}, "properties": map[string]any{"assessment_id": text, "pass_mark": map[string]any{"type": "integer", "minimum": 1}, "show_explanations": map[string]string{"type": "boolean"}, "questions": map[string]any{"type": "array", "items": question, "minItems": 1, "maxItems": 20}}, "required": []string{"pass_mark", "questions"}, "additionalProperties": false}
	presentation := map[string]any{"type": "object", "properties": map[string]any{"media_id": text, "poster_media_id": text, "caption_track_media_id": text, "caption": text, "transcript": text}, "required": []string{"media_id"}, "additionalProperties": false}
	return map[string]any{"type": "object", "description": "Existing publisher DraftInput. expected_version=0 creates content. Lessons/docs require an allowed collection and position. Quiz edits require quiz scope. Reviews and gate changes remain owner-only.", "properties": map[string]any{
		"expected_version": integer, "expected_collection_version": integer,
		"kind":          map[string]any{"type": "string", "enum": []string{"article", "lesson", "doc"}},
		"source_locale": map[string]any{"type": "string", "enum": []string{"en", "ar", "fr"}},
		"slug":          text, "title": map[string]any{"type": "string", "minLength": 1, "maxLength": 200}, "summary": map[string]any{"type": "string", "maxLength": 500}, "markdown": text,
		"source_revision_id": text, "category_key": text, "tag_keys": stringsList, "cover_media_id": text,
		"media_presentations": map[string]any{"type": "array", "items": presentation}, "collection_key": text, "position": integer, "quiz": quiz,
	}, "required": []string{"expected_version", "kind", "source_locale", "slug", "title", "markdown"}, "additionalProperties": false}
}
