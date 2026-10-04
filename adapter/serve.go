// Package adapter provides the high-level authoring API for Protocol v1
// adapter executables. Handlers are called sequentially on the protocol
// process's stdin/stdout streams.
package adapter

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/integrated-recorder/adapter-sdk-go/protocol"
)

type Adapter interface{ Descriptor() protocol.Descriptor }

type Resolver interface {
	Resolve(context.Context, protocol.ResolveParams) (protocol.ResolveResult, error)
}
type WorkflowResolver interface {
	BeginResolution(context.Context, protocol.ResolveBeginParams) (protocol.ResolveWorkflowResult, error)
	ContinueResolution(context.Context, protocol.ResolveContinueParams) (protocol.ResolveWorkflowResult, error)
}
type Watcher interface {
	WatchCheck(context.Context, protocol.WatchCheckParams) (protocol.WatchCheckResult, error)
}
type MetadataProvider interface {
	Metadata(context.Context, protocol.MetadataParams) (protocol.MetadataResult, error)
}
type Refresher interface {
	Refresh(context.Context, protocol.RefreshParams) (protocol.RefreshResult, error)
}
type ResourceBrowser interface {
	ListResources(context.Context, protocol.ResourceListParams) (protocol.ResourcePage, error)
	SearchResources(context.Context, protocol.ResourceSearchParams) (protocol.ResourcePage, error)
}

// OperationError is an adapter-authored, safe protocol error. Only errors
// created with Error are copied into a response; ordinary Go errors are
// replaced with a generic internal_error message.
type OperationError struct {
	Code    string
	Message string
}

func (e *OperationError) Error() string {
	if e == nil {
		return "adapter operation failed"
	}
	return e.Message
}

var errorCodeRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// Error creates a safe structured operation error. Do not place credentials,
// secrets, URLs, or request data in the supplied message.
func Error(code, message string) error {
	code = strings.TrimSpace(code)
	message = strings.TrimSpace(message)
	if !errorCodeRE.MatchString(code) || message == "" || len(message) > 1024 {
		return &OperationError{Code: "invalid_adapter_error", Message: "adapter operation failed"}
	}
	return &OperationError{Code: code, Message: message}
}

// Serve connects an adapter to the process standard streams. Stdout is
// reserved for protocol frames; this SDK intentionally writes no logs.
func Serve(a Adapter) error { return ServeContext(context.Background(), a) }

// ServeContext is the testable form of Serve. It uses process stdin/stdout and
// passes ctx to each handler. The call is sequential; no request handler
// overlaps another.
func ServeContext(ctx context.Context, a Adapter) error { return ServeIO(ctx, a, os.Stdin, os.Stdout) }

// ServeIO serves one adapter over the supplied streams. It is useful for
// adapter tests and is otherwise equivalent to ServeContext.
func ServeIO(ctx context.Context, a Adapter, input io.Reader, output io.Writer) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if a == nil || (reflect.ValueOf(a).Kind() == reflect.Ptr && reflect.ValueOf(a).IsNil()) {
		return errors.New("adapter is nil")
	}
	if input == nil || output == nil {
		return errors.New("adapter protocol streams are required")
	}
	d := a.Descriptor()
	if err := validateDescriptor(d, a); err != nil {
		return err
	}
	reader := bufio.NewReaderSize(input, 64<<10)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		req, err := readIncoming(reader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		response, stop := dispatchSafely(ctx, a, d, req)
		if err := protocol.WriteResponse(output, response); err != nil {
			return err
		}
		if stop {
			return nil
		}
	}
}

// Validate checks the descriptor and declared/implemented active capability
// interfaces without starting protocol I/O.
func Validate(a Adapter) error {
	if a == nil || (reflect.ValueOf(a).Kind() == reflect.Ptr && reflect.ValueOf(a).IsNil()) {
		return errors.New("adapter is nil")
	}
	d := a.Descriptor()
	return validateDescriptor(d, a)
}

func validateDescriptor(d protocol.Descriptor, a Adapter) error {
	if err := d.Validate(); err != nil {
		return fmt.Errorf("invalid adapter descriptor: %w", err)
	}
	return checkCapabilityInterfaces(d, a)
}

func checkCapabilityInterfaces(d protocol.Descriptor, a Adapter) error {
	declared := make(map[string]bool, len(d.Capabilities))
	for _, c := range d.Capabilities {
		declared[c] = true
	}
	// Protocol v1 reserves these capabilities, but the current Core runtime
	// does not dispatch their method families. This SDK therefore fails early
	// rather than advertising support it cannot provide.
	for _, dormant := range []string{protocol.CapabilityStatus, protocol.CapabilityConfigure, protocol.CapabilityInteraction, protocol.CapabilityEvents} {
		if declared[dormant] {
			return fmt.Errorf("capability %q is reserved and not served by this SDK", dormant)
		}
	}
	checks := []struct {
		cap         string
		implemented bool
	}{
		{protocol.CapabilityResolve, implements[Resolver](a)},
		{protocol.CapabilityResolveWorkflow, implements[WorkflowResolver](a)},
		{protocol.CapabilityWatch, implements[Watcher](a)},
		{protocol.CapabilityMetadata, implements[MetadataProvider](a)},
		{protocol.CapabilityRefresh, implements[Refresher](a)},
		{protocol.CapabilityResourceBrowse, implements[ResourceBrowser](a)},
	}
	for _, c := range checks {
		if declared[c.cap] != c.implemented {
			if c.implemented {
				return fmt.Errorf("adapter implements capability %q but descriptor does not declare it", c.cap)
			}
			return fmt.Errorf("descriptor declares capability %q but adapter does not implement it", c.cap)
		}
	}
	return nil
}
func implements[T any](a Adapter) bool { _, ok := a.(T); return ok }

func readIncoming(r *bufio.Reader) (protocol.Request, error) {
	// The low-level v1 parser also rejects unsupported versions. Read the
	// envelope first so Serve can answer with the matching request ID/version.
	line, err := readLine(r)
	if err != nil {
		return protocol.Request{}, err
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(line, &envelope); err != nil || envelope == nil {
		return protocol.Request{}, fmt.Errorf("malformed protocol request")
	}
	if _, hasType := envelope["type"]; hasType {
		var typ string
		if json.Unmarshal(envelope["type"], &typ) != nil || typ != "request" {
			return protocol.Request{}, fmt.Errorf("expected request frame")
		}
	}
	if _, ok := envelope["protocol_version"]; !ok {
		return protocol.Request{}, fmt.Errorf("protocol version is required")
	}
	if _, hasResult := envelope["result"]; hasResult {
		return protocol.Request{}, fmt.Errorf("request frame cannot contain result")
	}
	if _, hasError := envelope["error"]; hasError {
		return protocol.Request{}, fmt.Errorf("request frame cannot contain error")
	}
	var req protocol.Request
	if err := json.Unmarshal(line, &req); err != nil {
		return protocol.Request{}, fmt.Errorf("malformed protocol request")
	}
	if strings.TrimSpace(req.ID) == "" || strings.TrimSpace(req.Method) == "" {
		return protocol.Request{}, fmt.Errorf("request id and method are required")
	}
	if len(req.Params) > 0 && !json.Valid(req.Params) {
		return protocol.Request{}, fmt.Errorf("request parameters are invalid JSON")
	}
	return req, nil
}

func readLine(r *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		part, err := r.ReadSlice('\n')
		if len(line)+len(part) > protocol.MaxFrameBytes+1 {
			return nil, fmt.Errorf("protocol frame exceeds %d bytes", protocol.MaxFrameBytes)
		}
		line = append(line, part...)
		if err == nil {
			line = line[:len(line)-1]
			if len(line) == 0 {
				return nil, fmt.Errorf("empty protocol frame")
			}
			if !utf8.Valid(line) {
				return nil, fmt.Errorf("protocol frame is not valid UTF-8")
			}
			return line, nil
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if errors.Is(err, io.EOF) {
			if len(line) == 0 {
				return nil, io.EOF
			}
			return nil, fmt.Errorf("unterminated protocol frame")
		}
		return nil, err
	}
}

func dispatchSafely(ctx context.Context, a Adapter, d protocol.Descriptor, req protocol.Request) (response protocol.Response, stop bool) {
	defer func() {
		if recover() != nil {
			response = protocol.Response{ProtocolVersion: req.ProtocolVersion, ID: req.ID, Error: &protocol.Error{Code: "internal_error", Message: "adapter operation failed"}}
			stop = false
		}
	}()
	if req.ProtocolVersion != protocol.Version {
		return protocol.Response{ProtocolVersion: req.ProtocolVersion, ID: req.ID, Error: &protocol.Error{Code: "unsupported_protocol_version", Message: "protocol version is not supported"}}, false
	}
	var result any
	var err error
	switch req.Method {
	case protocol.MethodDescribe:
		result = d
	case protocol.MethodShutdown:
		result = struct{}{}
		stop = true
	case protocol.MethodResolve:
		h, ok := a.(Resolver)
		if !ok || !has(d, protocol.CapabilityResolve) {
			return failure(req, "unsupported_method", "method is not supported"), false
		}
		var p protocol.ResolveParams
		if err = decodeParams(req.Params, &p); err == nil {
			result, err = h.Resolve(ctx, p)
		}
	case protocol.MethodResolveBegin:
		h, ok := a.(WorkflowResolver)
		if !ok || !has(d, protocol.CapabilityResolveWorkflow) {
			return failure(req, "unsupported_method", "method is not supported"), false
		}
		var p protocol.ResolveBeginParams
		if err = decodeParams(req.Params, &p); err == nil {
			result, err = h.BeginResolution(ctx, p)
		}
	case protocol.MethodResolveContinue:
		h, ok := a.(WorkflowResolver)
		if !ok || !has(d, protocol.CapabilityResolveWorkflow) {
			return failure(req, "unsupported_method", "method is not supported"), false
		}
		var p protocol.ResolveContinueParams
		if err = decodeParams(req.Params, &p); err == nil {
			result, err = h.ContinueResolution(ctx, p)
		}
	case protocol.MethodWatchCheck:
		h, ok := a.(Watcher)
		if !ok || !has(d, protocol.CapabilityWatch) {
			return failure(req, "unsupported_method", "method is not supported"), false
		}
		var p protocol.WatchCheckParams
		if err = decodeParams(req.Params, &p); err == nil {
			result, err = h.WatchCheck(ctx, p)
		}
	case protocol.MethodMetadata:
		h, ok := a.(MetadataProvider)
		if !ok || !has(d, protocol.CapabilityMetadata) {
			return failure(req, "unsupported_method", "method is not supported"), false
		}
		var p protocol.MetadataParams
		if err = decodeParams(req.Params, &p); err == nil {
			result, err = h.Metadata(ctx, p)
		}
	case protocol.MethodRefresh:
		h, ok := a.(Refresher)
		if !ok || !has(d, protocol.CapabilityRefresh) {
			return failure(req, "unsupported_method", "method is not supported"), false
		}
		var p protocol.RefreshParams
		if err = decodeParams(req.Params, &p); err == nil {
			result, err = h.Refresh(ctx, p)
		}
	case protocol.MethodResourceList:
		h, ok := a.(ResourceBrowser)
		if !ok || !has(d, protocol.CapabilityResourceBrowse) {
			return failure(req, "unsupported_method", "method is not supported"), false
		}
		var p protocol.ResourceListParams
		if err = decodeParams(req.Params, &p); err == nil {
			result, err = h.ListResources(ctx, p)
		}
	case protocol.MethodResourceSearch:
		h, ok := a.(ResourceBrowser)
		if !ok || !has(d, protocol.CapabilityResourceBrowse) {
			return failure(req, "unsupported_method", "method is not supported"), false
		}
		var p protocol.ResourceSearchParams
		if err = decodeParams(req.Params, &p); err == nil {
			result, err = h.SearchResources(ctx, p)
		}
	default:
		return failure(req, "unsupported_method", "method is not supported"), false
	}
	if err != nil {
		var op *OperationError
		if errors.As(err, &op) && op != nil && errorCodeRE.MatchString(op.Code) && op.Message != "" && len(op.Message) <= 1024 {
			return protocol.Response{ProtocolVersion: req.ProtocolVersion, ID: req.ID, Error: &protocol.Error{Code: op.Code, Message: op.Message}}, stop
		}
		return failure(req, "internal_error", "adapter operation failed"), stop
	}
	if err = validateResult(req.Method, result, d); err != nil {
		return failure(req, "invalid_result", "adapter returned an invalid result"), stop
	}
	b, err := json.Marshal(result)
	if err != nil {
		return failure(req, "internal_error", "adapter operation failed"), stop
	}
	if len(b) > protocol.MaxFrameBytes {
		return failure(req, "result_too_large", "adapter result exceeds the protocol frame limit"), stop
	}
	return protocol.Response{ProtocolVersion: req.ProtocolVersion, ID: req.ID, Result: b}, stop
}
func decodeParams(raw json.RawMessage, dst any) error {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	return protocol.DecodeObject(raw, dst)
}
func has(d protocol.Descriptor, c string) bool {
	for _, x := range d.Capabilities {
		if x == c {
			return true
		}
	}
	return false
}
func failure(req protocol.Request, code, msg string) protocol.Response {
	return protocol.Response{ProtocolVersion: req.ProtocolVersion, ID: req.ID, Error: &protocol.Error{Code: code, Message: msg}}
}
func validateResult(method string, result any, d protocol.Descriptor) error {
	switch v := result.(type) {
	case protocol.WatchCheckResult:
		if err := v.Validate(); err != nil {
			return err
		}
		if v.Media != nil {
			return validateMedia(*v.Media, d.MediaTypes)
		}
	case protocol.MetadataResult:
		return v.Validate()
	case protocol.ResolveResult:
		return validateMedia(v.Media, d.MediaTypes)
	case protocol.RefreshResult:
		return validateMedia(v.Media, d.MediaTypes)
	case protocol.ResolveWorkflowResult:
		return validateWorkflowResult(v, d)
	case protocol.ResourcePage:
		if v.Items == nil {
			return fmt.Errorf("resource page items must be present")
		}
		if len(v.NextCursor) > 4096 {
			return fmt.Errorf("resource cursor exceeds limit")
		}
	}
	return nil
}

func validateWorkflowResult(result protocol.ResolveWorkflowResult, d protocol.Descriptor) error {
	// Core treats an explicit workflow error as a terminal failure before
	// validating the ordinary success-state shape.
	if result.State == "error" {
		return nil
	}
	if err := protocol.ValidateResourceRef(result.Resource); err != nil {
		return fmt.Errorf("workflow resource is invalid")
	}
	switch result.State {
	case "resource_discovered":
		if result.Resource == nil || result.Media != nil || result.Challenge != nil {
			return fmt.Errorf("resource discovery result is inconsistent")
		}
	case "configuration_required", "interaction_required":
		if result.Challenge == nil || result.Media != nil {
			return fmt.Errorf("workflow challenge result is inconsistent")
		}
		if err := result.Challenge.Schema.Validate(); err != nil {
			return fmt.Errorf("workflow challenge schema is invalid")
		}
		if result.Challenge.Prompt != nil {
			if err := validateWorkflowPrompt(*result.Challenge.Prompt); err != nil {
				return fmt.Errorf("workflow prompt is invalid")
			}
		}
	case "resolved":
		if result.Media == nil || result.Challenge != nil {
			return fmt.Errorf("resolved workflow result is inconsistent")
		}
		return validateMedia(*result.Media, d.MediaTypes)
	default:
		return fmt.Errorf("workflow state is invalid")
	}
	return nil
}

func validateWorkflowPrompt(message protocol.InteractionMessage) error {
	if strings.TrimSpace(message.InteractionID) == "" || len(message.InteractionID) > 256 {
		return fmt.Errorf("interaction id is invalid")
	}
	switch message.Type {
	case "action", "prompt", "secret_prompt", "navigate", "display", "status", "complete", "error":
	default:
		return fmt.Errorf("interaction type is invalid")
	}
	if len(message.Title) > 512 || len(message.Message) > 4096 || len(message.Data) > 64<<10 || len(message.Data) > 0 && !json.Valid(message.Data) {
		return fmt.Errorf("interaction message exceeds limits")
	}
	fields := protocol.Schema{Fields: make([]protocol.Field, 0, len(message.Fields))}
	for _, field := range message.Fields {
		switch field.Control {
		case "text", "secret", "number", "boolean", "select", "multi-select", "textarea":
		default:
			return fmt.Errorf("interaction field control is invalid")
		}
		fields.Fields = append(fields.Fields, protocol.Field{
			Key: field.Key, Control: field.Control, Label: field.Label,
			Description: field.Description, Required: field.Required, Options: field.Options,
		})
	}
	return fields.Validate()
}

func validateMedia(m protocol.MediaSource, supported []string) error {
	allowed := false
	for _, t := range supported {
		if t == m.Type {
			allowed = true
			break
		}
	}
	if !allowed || m.ManifestURL == "" {
		return fmt.Errorf("media type or URL is invalid")
	}
	return nil
}
