// Package ahe connects Pouch to public AHE MCP tools without importing AHE code.
package ahe

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"slices"
	"sync"

	"github.com/Yui-Qi-Tang/pouch/internal/validation"
)

const protocolVersion = "2025-06-18"
const maxMessageBytes = 16 << 20

var ErrCapabilityUnavailable = errors.New("CAPABILITY_UNAVAILABLE")
var ErrDeliveryUnknown = errors.New("DELIVERY_UNKNOWN")

// Command is supplied by the local operator. Path must be absolute; no shell is used.
// Env, when nil, inherits the process environment. Credentials belong in the launcher.
type Command struct {
	Path string   `json:"path"`
	Args []string `json:"args"`
	Env  []string `json:"env,omitempty"`
}

type Tool struct {
	Name        string          `json:"name"`
	InputSchema json.RawMessage `json:"inputSchema"`
	Annotations struct {
		ReadOnly bool `json:"readOnlyHint"`
	} `json:"annotations"`
}

// SchemaSHA256 identifies the exact inputSchema bytes received during discovery.
func (t Tool) SchemaSHA256() string { return validation.Digest(t.InputSchema) }

type incoming struct {
	raw []byte
	err error
}

// Client owns one serialized stdio connection. A cancelled request closes it so
// a delayed response cannot be mistaken for the next request. It never retries.
type Client struct {
	cmd       *exec.Cmd
	in        io.WriteCloser
	messages  chan incoming
	done      chan struct{}
	wait      chan error
	gate      chan struct{}
	closeOnce sync.Once
	tools     []Tool
	nextID    int
}

// Start initializes and discovers tools, but grants no permission to call them.
// ctx also controls the lifetime of the child process.
func Start(ctx context.Context, command Command) (*Client, error) {
	if ctx == nil || !filepath.IsAbs(command.Path) {
		return nil, errors.New("context and absolute MCP executable required")
	}
	cmd := exec.CommandContext(ctx, command.Path, command.Args...)
	if command.Env != nil {
		cmd.Env = slices.Clone(command.Env)
	}
	cmd.Stderr = io.Discard // Do not expose launcher credentials through diagnostics.
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		_ = in.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		_ = in.Close()
		return nil, fmt.Errorf("starting MCP: %w", err)
	}
	c := &Client{cmd: cmd, in: in, messages: make(chan incoming, 1), done: make(chan struct{}), wait: make(chan error, 1), gate: make(chan struct{}, 1)}
	go func() {
		scanner := bufio.NewScanner(out)
		scanner.Buffer(make([]byte, 64<<10), maxMessageBytes)
		for scanner.Scan() {
			if !c.deliver(incoming{raw: bytes.Clone(scanner.Bytes())}) {
				return
			}
		}
		err := scanner.Err()
		if err == nil {
			err = io.EOF
		}
		c.deliver(incoming{err: err})
	}()
	go func() { c.wait <- cmd.Wait() }()
	raw, err := c.request(ctx, "initialize", map[string]any{"protocolVersion": protocolVersion, "capabilities": map[string]any{}, "clientInfo": map[string]string{"name": "pouch", "version": "v1"}}, false)
	if err != nil {
		c.Close()
		return nil, err
	}
	var init struct {
		ProtocolVersion string `json:"protocolVersion"`
		Capabilities    struct {
			Tools json.RawMessage `json:"tools"`
		} `json:"capabilities"`
	}
	if json.Unmarshal(raw, &init) != nil || init.ProtocolVersion != protocolVersion || len(init.Capabilities.Tools) == 0 {
		c.Close()
		return nil, fmt.Errorf("%w: incompatible MCP initialization", ErrCapabilityUnavailable)
	}
	if _, err := io.WriteString(in, "{\"jsonrpc\":\"2.0\",\"method\":\"notifications/initialized\"}\n"); err != nil {
		c.Close()
		return nil, err
	}
	cursor := ""
	seen := map[string]bool{}
	names := map[string]bool{}
	for page := 0; page < 32; page++ {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		raw, err = c.request(ctx, "tools/list", params, false)
		if err != nil {
			c.Close()
			return nil, err
		}
		var list struct {
			Tools      []Tool `json:"tools"`
			NextCursor string `json:"nextCursor"`
		}
		if json.Unmarshal(raw, &list) != nil {
			c.Close()
			return nil, errors.New("invalid MCP discovery")
		}
		for _, tool := range list.Tools {
			if tool.Name == "" || names[tool.Name] || len(tool.InputSchema) == 0 {
				c.Close()
				return nil, errors.New("invalid or duplicate MCP tool")
			}
			names[tool.Name] = true
			c.tools = append(c.tools, tool)
		}
		if list.NextCursor == "" {
			return c, nil
		}
		if seen[list.NextCursor] {
			break
		}
		seen[list.NextCursor] = true
		cursor = list.NextCursor
	}
	c.Close()
	return nil, errors.New("MCP discovery pagination exceeded or repeated")
}
func (c *Client) deliver(m incoming) bool {
	select {
	case c.messages <- m:
		return true
	case <-c.done:
		return false
	}
}
func (c *Client) Close() {
	if c == nil {
		return
	}
	c.closeOnce.Do(func() { close(c.done); _ = c.in.Close(); _ = c.cmd.Process.Kill(); <-c.wait })
}
func (c *Client) Tools() []Tool {
	out := slices.Clone(c.tools)
	for i := range out {
		out[i].InputSchema = bytes.Clone(out[i].InputSchema)
	}
	return out
}

// ToolError preserves a server-reported error payload for reconciliation.
type ToolError struct{ Payload json.RawMessage }

func (e *ToolError) Error() string { return "MCP_TOOL_REJECTED" }

type ProfileKind string

const (
	ProfileQuery               ProfileKind = "query"
	ProfileIntake              ProfileKind = "intake"
	ProfileEndpointReviewer    ProfileKind = "endpoint-reviewer"
	ProfileSourceClaimReviewer ProfileKind = "source-claim-reviewer"
)

var allowed = map[ProfileKind]map[string]bool{
	ProfileQuery:               {"open_canonical_read_view": false, "get_evidence_record": false},
	ProfileIntake:              {"submit_external_source": true, "submit_extractor_output": true},
	ProfileEndpointReviewer:    {"get_endpoint_review": false, "admit_reviewed_endpoint": true},
	ProfileSourceClaimReviewer: {"get_source_claim_review": false, "admit_reviewed_source_claim": true},
}

// Profile admits only tools whose schemas were pinned outside the result package.
type Profile struct {
	client *Client
	kind   ProfileKind
	tools  map[string]bool
}

func (c *Client) Bind(kind ProfileKind, schemaPins map[string]string) (*Profile, error) {
	permit, ok := allowed[kind]
	if !ok || len(schemaPins) == 0 {
		return nil, fmt.Errorf("%w: empty or unknown profile", ErrCapabilityUnavailable)
	}
	p := &Profile{client: c, kind: kind, tools: map[string]bool{}}
	for name, pin := range schemaPins {
		write, ok := permit[name]
		if !ok {
			return nil, fmt.Errorf("%w: tool outside profile: %s", ErrCapabilityUnavailable, name)
		}
		found := false
		for _, tool := range c.tools {
			if tool.Name == name {
				found = true
				if tool.SchemaSHA256() != pin || (!write && !tool.Annotations.ReadOnly) {
					return nil, fmt.Errorf("%w: tool schema or read annotation mismatch: %s", ErrCapabilityUnavailable, name)
				}
			}
		}
		if !found {
			return nil, fmt.Errorf("%w: missing tool: %s", ErrCapabilityUnavailable, name)
		}
		p.tools[name] = write
	}
	return p, nil
}
func (p *Profile) call(ctx context.Context, name string, args any) (json.RawMessage, error) {
	if p == nil {
		return nil, ErrCapabilityUnavailable
	}
	write, ok := p.tools[name]
	if !ok {
		return nil, fmt.Errorf("%w: unpinned tool: %s", ErrCapabilityUnavailable, name)
	}
	raw, err := p.client.request(ctx, "tools/call", map[string]any{"name": name, "arguments": args}, write)
	if err != nil {
		return nil, err
	}
	var response struct {
		IsError    bool            `json:"isError"`
		Structured json.RawMessage `json:"structuredContent"`
	}
	if json.Unmarshal(raw, &response) != nil || len(response.Structured) == 0 || bytes.Equal(response.Structured, []byte("null")) {
		if write {
			return nil, ErrDeliveryUnknown
		}
		return nil, errors.New("MCP structured result missing")
	}
	if response.IsError {
		return nil, &ToolError{Payload: bytes.Clone(response.Structured)}
	}
	return bytes.Clone(response.Structured), nil
}
func (c *Client) request(ctx context.Context, method string, params any, write bool) (json.RawMessage, error) {
	if ctx == nil {
		return nil, errors.New("context required")
	}
	select {
	case c.gate <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.done:
		return nil, errors.New("MCP connection closed")
	}
	defer func() { <-c.gate }()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.nextID++
	id := c.nextID
	payload, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if err != nil {
		return nil, err
	}
	failure := func(err error) (json.RawMessage, error) {
		c.Close()
		if write {
			return nil, fmt.Errorf("%w: reconcile exact request before retry: %v", ErrDeliveryUnknown, err)
		}
		return nil, err
	}
	if len(payload) > maxMessageBytes {
		return nil, errors.New("MCP request too large")
	}
	written := make(chan error, 1)
	go func() { _, writeErr := c.in.Write(append(payload, '\n')); written <- writeErr }()
	select {
	case err := <-written:
		if err != nil {
			return failure(err)
		}
	case <-ctx.Done():
		return failure(ctx.Err())
	case <-c.done:
		return failure(errors.New("MCP connection closed"))
	}
	for notifications := 0; notifications < 128; notifications++ {
		var message incoming
		select {
		case message = <-c.messages:
		case <-ctx.Done():
			return failure(ctx.Err())
		case <-c.done:
			return failure(errors.New("MCP connection closed"))
		}
		if message.err != nil {
			return failure(message.err)
		}
		if err := uniqueJSON(message.raw); err != nil {
			return failure(err)
		}
		var response struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      *int            `json:"id"`
			Method  string          `json:"method"`
			Result  json.RawMessage `json:"result"`
			Error   json.RawMessage `json:"error"`
		}
		if json.Unmarshal(message.raw, &response) != nil || response.JSONRPC != "2.0" {
			return failure(errors.New("invalid MCP response"))
		}
		if response.ID == nil && response.Method != "" {
			continue
		}
		if response.ID == nil || *response.ID != id {
			return failure(errors.New("MCP response ID mismatch"))
		}
		if len(response.Error) > 0 && string(response.Error) != "null" {
			toolErr := &ToolError{Payload: bytes.Clone(response.Error)}
			if write {
				// A protocol error is not an admission receipt. The handler may
				// have committed before reporting its internal error.
				c.Close()
				return nil, fmt.Errorf("%w: %w", ErrDeliveryUnknown, toolErr)
			}
			return nil, toolErr
		}
		if len(response.Result) == 0 {
			return failure(errors.New("MCP result missing"))
		}
		return response.Result, nil
	}
	return failure(errors.New("MCP notification limit exceeded"))
}

// Reject ambiguous duplicate fields before decoding typed protocol projections.
func uniqueJSON(raw []byte) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var value func(int) error
	value = func(depth int) error {
		if depth > 128 {
			return errors.New("JSON nesting limit exceeded")
		}
		token, err := d.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				k, err := d.Token()
				if err != nil {
					return err
				}
				key, ok := k.(string)
				if !ok || seen[key] {
					return errors.New("duplicate JSON field")
				}
				seen[key] = true
				if err := value(depth + 1); err != nil {
					return err
				}
			}
		case '[':
			for d.More() {
				if err := value(depth + 1); err != nil {
					return err
				}
			}
		default:
			return errors.New("invalid JSON delimiter")
		}
		_, err = d.Token()
		return err
	}
	if err := value(0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("trailing JSON value")
	}
	return nil
}
