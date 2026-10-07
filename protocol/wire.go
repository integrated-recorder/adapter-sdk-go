// Package protocol contains the standalone Go wire types for Integrated
// Recorder Adapter Protocol v1. It has no dependency on Integrated Recorder.
package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/png"
	"math"
	"math/big"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	Version       = 1
	MaxFrameBytes = 8 << 20
)

const (
	CapabilityResolve         = "resolve"
	CapabilityStatus          = "status"
	CapabilityConfigure       = "configure"
	CapabilityInteraction     = "interaction"
	CapabilityMetadata        = "metadata"
	CapabilityEvents          = "events"
	CapabilityRefresh         = "refresh"
	CapabilityResolveWorkflow = "resolve_workflow"
	CapabilityResourceBrowse  = "resource_browse"
	CapabilityWatch           = "watch"
)

const (
	MethodDescribe            = "describe"
	MethodResolve             = "resolve"
	MethodResolveBegin        = "resolve.begin"
	MethodResolveContinue     = "resolve.continue"
	MethodShutdown            = "shutdown"
	MethodGetStatus           = "get_status"
	MethodConfigure           = "configure"
	MethodInteractionBegin    = "interaction.begin"
	MethodInteractionContinue = "interaction.continue"
	MethodMetadata            = "metadata"
	MethodEvents              = "events"
	MethodRefresh             = "refresh"
	MethodResourceList        = "resource.list"
	MethodResourceSearch      = "resource.search"
	MethodWatchCheck          = "watch.check"
)

const (
	PersistenceForbidden       = "forbidden"
	PersistenceOptional        = "optional"
	PersistenceRequired        = "required"
	PersistencePlugin          = "plugin"
	PersistenceCurrent         = "current_resource"
	PersistenceResource        = "resource"
	HeaderForwardingSameOrigin = "same_origin"
	HeaderForwardingAllowlist  = "allowlist"
)

type Request struct {
	ProtocolVersion int             `json:"protocol_version"`
	ID              string          `json:"id"`
	Method          string          `json:"method"`
	Params          json.RawMessage `json:"params,omitempty"`
}

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

type Response struct {
	ProtocolVersion int             `json:"protocol_version"`
	ID              string          `json:"id"`
	Result          json.RawMessage `json:"result,omitempty"`
	Error           *Error          `json:"error,omitempty"`
}

type FrameType string

const (
	FrameTypeRequest      FrameType = "request"
	FrameTypeResponse     FrameType = "response"
	FrameTypeNotification FrameType = "notification"
)

type Frame struct {
	Kind         FrameType
	Request      *Request
	Response     *Response
	Notification *Notification
}

type Notification struct {
	ProtocolVersion int             `json:"protocol_version"`
	Method          string          `json:"method"`
	Params          json.RawMessage `json:"params,omitempty"`
}

type Descriptor struct {
	ID                  string         `json:"id"`
	Name                string         `json:"name"`
	Version             string         `json:"version"`
	ProtocolVersion     int            `json:"protocol_version"`
	Capabilities        []string       `json:"capabilities,omitempty"`
	InputSchema         Schema         `json:"input_schema"`
	ConfigurationSchema Schema         `json:"configuration_schema"`
	ResourceTypes       []ResourceType `json:"resource_types,omitempty"`
	MediaTypes          []string       `json:"media_types"`
	Branding            *Branding      `json:"branding,omitempty"`
}

type Branding struct {
	Icon *BrandIcon `json:"icon,omitempty"`
}
type BrandIcon struct {
	MediaType string `json:"media_type"`
	Data      []byte `json:"data"`
}
type Schema struct {
	Fields []Field `json:"fields"`
}
type Field struct {
	Key         string            `json:"key"`
	Control     string            `json:"control"`
	Label       string            `json:"label"`
	Description string            `json:"description,omitempty"`
	Required    bool              `json:"required,omitempty"`
	Inherit     bool              `json:"inherit,omitempty"`
	Default     json.RawMessage   `json:"default,omitempty"`
	Constraints *Constraints      `json:"constraints,omitempty"`
	Options     []Option          `json:"options,omitempty"`
	VisibleWhen json.RawMessage   `json:"visible_when,omitempty"`
	Persistence *FieldPersistence `json:"persistence,omitempty"`
}
type FieldPersistence struct {
	Mode   string            `json:"mode"`
	Target PersistenceTarget `json:"target"`
}
type PersistenceTarget struct {
	Scope    string       `json:"scope"`
	Resource *ResourceRef `json:"resource,omitempty"`
}
type Constraints struct {
	Min       *float64 `json:"min,omitempty"`
	Max       *float64 `json:"max,omitempty"`
	MinLength *int     `json:"min_length,omitempty"`
	MaxLength *int     `json:"max_length,omitempty"`
	Pattern   string   `json:"pattern,omitempty"`
	MinItems  *int     `json:"min_items,omitempty"`
	MaxItems  *int     `json:"max_items,omitempty"`
}
type Option struct {
	Value any    `json:"value"`
	Label string `json:"label"`
}

// UnmarshalJSON preserves JSON integer precision for option values.
func (o *Option) UnmarshalJSON(data []byte) error {
	var w struct {
		Value json.RawMessage `json:"value"`
		Label string          `json:"label"`
	}
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	if len(w.Value) == 0 {
		return fmt.Errorf("option value is required")
	}
	d := json.NewDecoder(bytes.NewReader(w.Value))
	d.UseNumber()
	if err := d.Decode(&o.Value); err != nil {
		return err
	}
	o.Label = w.Label
	return nil
}

type ResourceType struct {
	Type                string   `json:"type"`
	ParentTypes         []string `json:"parent_types,omitempty"`
	ConfigurationSchema Schema   `json:"configuration_schema,omitempty"`
}
type ResourceRef struct {
	Type   string       `json:"resource_type"`
	ID     string       `json:"resource_id"`
	Parent *ResourceRef `json:"parent,omitempty"`
}
type Resource struct {
	ResourceRef
	DisplayName string                     `json:"display_name,omitempty"`
	Attributes  map[string]json.RawMessage `json:"attributes,omitempty"`
}
type ResourceListParams struct {
	Parent       *ResourceRef `json:"parent,omitempty"`
	ResourceType string       `json:"resource_type,omitempty"`
	Cursor       string       `json:"cursor,omitempty"`
	Limit        int          `json:"limit"`
}
type ResourceSearchParams struct {
	Parent       *ResourceRef `json:"parent,omitempty"`
	ResourceType string       `json:"resource_type,omitempty"`
	Query        string       `json:"query"`
	Cursor       string       `json:"cursor,omitempty"`
	Limit        int          `json:"limit"`
}
type ResourcePage struct {
	Items      []Resource `json:"items"`
	NextCursor string     `json:"next_cursor,omitempty"`
}

type ResolveParams struct {
	Input         json.RawMessage            `json:"input"`
	Resource      *ResourceRef               `json:"resource,omitempty"`
	Configuration map[string]json.RawMessage `json:"configuration,omitempty"`
	Secrets       map[string]string          `json:"secrets,omitempty"`
	State         []StateDocument            `json:"state,omitempty"`
}
type WatchCheckParams struct {
	Input         json.RawMessage            `json:"input"`
	Resource      *ResourceRef               `json:"resource,omitempty"`
	Configuration map[string]json.RawMessage `json:"configuration,omitempty"`
	Secrets       map[string]string          `json:"secrets,omitempty"`
	State         []StateDocument            `json:"state,omitempty"`
}
type WatchCheckResult struct {
	State          string          `json:"state"`
	SessionRef     string          `json:"session_ref,omitempty"`
	Title          string          `json:"title,omitempty"`
	StartedAt      *time.Time      `json:"started_at,omitempty"`
	Media          *MediaSource    `json:"media,omitempty"`
	StateMutations []StateMutation `json:"state_mutations,omitempty"`
}
type ResolveResult struct {
	Media MediaSource     `json:"media"`
	State []StateMutation `json:"state,omitempty"`
}
type ResolveBeginParams struct {
	WorkflowID    string                     `json:"workflow_id"`
	Input         json.RawMessage            `json:"input"`
	Resource      *ResourceRef               `json:"resource,omitempty"`
	Configuration map[string]json.RawMessage `json:"configuration,omitempty"`
	Secrets       map[string]string          `json:"secrets,omitempty"`
	State         []StateDocument            `json:"state,omitempty"`
}
type ResolveContinueParams struct {
	WorkflowID    string                     `json:"workflow_id"`
	Resource      *ResourceRef               `json:"resource,omitempty"`
	Configuration map[string]json.RawMessage `json:"configuration,omitempty"`
	Secrets       map[string]string          `json:"secrets,omitempty"`
	Answers       map[string]json.RawMessage `json:"answers,omitempty"`
	AnswerSecrets map[string]string          `json:"answer_secrets,omitempty"`
	State         []StateDocument            `json:"state,omitempty"`
}
type WorkflowChallenge struct {
	Schema      Schema              `json:"schema"`
	Prompt      *InteractionMessage `json:"prompt,omitempty"`
	Persistable bool                `json:"persistable,omitempty"`
}
type StateDocument struct {
	Resource *ResourceRef               `json:"resource,omitempty"`
	Values   map[string]json.RawMessage `json:"values,omitempty"`
	Secrets  map[string]string          `json:"secrets,omitempty"`
}
type StateMutation struct {
	Resource     *ResourceRef               `json:"resource,omitempty"`
	Values       map[string]json.RawMessage `json:"values,omitempty"`
	Secrets      map[string]string          `json:"secrets,omitempty"`
	ClearValues  []string                   `json:"clear_values,omitempty"`
	ClearSecrets []string                   `json:"clear_secrets,omitempty"`
}
type ResolveWorkflowResult struct {
	State          string             `json:"state"`
	WorkflowID     string             `json:"workflow_id"`
	Resource       *ResourceRef       `json:"resource,omitempty"`
	Challenge      *WorkflowChallenge `json:"challenge,omitempty"`
	Media          *MediaSource       `json:"media,omitempty"`
	StateMutations []StateMutation    `json:"state_mutations,omitempty"`
}
type AdapterProvenance struct {
	ID              string `json:"id"`
	Version         string `json:"version"`
	ProtocolVersion int    `json:"protocol_version"`
	Fingerprint     string `json:"descriptor_fingerprint,omitempty"`
}
type MediaSource struct {
	Type                   string                  `json:"type"`
	ManifestURL            string                  `json:"manifest_url"`
	Headers                map[string]string       `json:"headers,omitempty"`
	RequestPolicy          *RequestPolicy          `json:"request_policy,omitempty"`
	SessionRef             string                  `json:"session_ref,omitempty"`
	Refresh                json.RawMessage         `json:"refresh,omitempty"`
	Metadata               json.RawMessage         `json:"metadata,omitempty"`
	ArchivePolicy          *ArchivePolicy          `json:"archive_policy,omitempty"`
	RefreshPolicy          *RefreshPolicy          `json:"refresh_policy,omitempty"`
	HistoricalAvailability *HistoricalAvailability `json:"historical_availability,omitempty"`
}
type RefreshPolicy struct {
	ExpiresAt            *time.Time `json:"expires_at,omitempty"`
	RefreshBeforeSeconds int        `json:"refresh_before_seconds,omitempty"`
	OnHTTPStatus         []int      `json:"on_http_status,omitempty"`
}
type RefreshParams struct {
	Resource *ResourceRef    `json:"resource,omitempty"`
	Current  MediaSource     `json:"current"`
	State    []StateDocument `json:"state,omitempty"`
}
type RefreshResult struct {
	Media MediaSource     `json:"media"`
	State []StateMutation `json:"state,omitempty"`
}
type MetadataParams struct {
	Resource      *ResourceRef               `json:"resource,omitempty"`
	Current       MediaSource                `json:"current"`
	Configuration map[string]json.RawMessage `json:"configuration,omitempty"`
	Secrets       map[string]string          `json:"secrets,omitempty"`
	State         []StateDocument            `json:"state,omitempty"`
}
type StreamMetadata struct {
	Title       *string `json:"title,omitempty"`
	Description *string `json:"description,omitempty"`
}
type MetadataResult struct {
	Metadata        StreamMetadata  `json:"metadata"`
	SourceUpdatedAt *time.Time      `json:"source_updated_at,omitempty"`
	StateMutations  []StateMutation `json:"state_mutations,omitempty"`
}
type ArchivePolicy struct {
	SourceURI string `json:"source_uri,omitempty"`
}
type RequestPolicy struct {
	HeaderForwarding *HeaderForwardingPolicy `json:"header_forwarding,omitempty"`
	URLTransform     *URLTransformPolicy     `json:"url_transform,omitempty"`
}
type HeaderForwardingPolicy struct {
	Mode    string   `json:"mode,omitempty"`
	Origins []string `json:"origins,omitempty"`
}
type InteractionField struct {
	Key         string   `json:"key"`
	Control     string   `json:"control"`
	Label       string   `json:"label"`
	Description string   `json:"description,omitempty"`
	Required    bool     `json:"required,omitempty"`
	Options     []Option `json:"options,omitempty"`
}
type InteractionMessage struct {
	Type          string             `json:"type"`
	InteractionID string             `json:"interaction_id"`
	Title         string             `json:"title,omitempty"`
	Message       string             `json:"message,omitempty"`
	Fields        []InteractionField `json:"fields,omitempty"`
	Data          json.RawMessage    `json:"data,omitempty"`
}

var identifierRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
var mediaTypeRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,63}$`)

func IsValidIdentifier(s string) bool { return identifierRE.MatchString(s) }

// Validate applies the public, deterministic descriptor checks. Core remains
// the authoritative validator; this catches common authoring errors locally.
func (d Descriptor) Validate() error {
	if !IsValidIdentifier(d.ID) {
		return fmt.Errorf("adapter id is invalid")
	}
	if strings.TrimSpace(d.Name) == "" || strings.TrimSpace(d.Version) == "" {
		return fmt.Errorf("adapter name and version are required")
	}
	if d.ProtocolVersion != Version {
		return fmt.Errorf("unsupported protocol version %d", d.ProtocolVersion)
	}
	if err := validateBranding(d.Branding); err != nil {
		return err
	}
	if err := d.InputSchema.Validate(); err != nil {
		return fmt.Errorf("input schema: %w", err)
	}
	if err := d.ConfigurationSchema.Validate(); err != nil {
		return fmt.Errorf("configuration schema: %w", err)
	}
	seen := map[string]bool{}
	for _, c := range d.Capabilities {
		if !IsValidIdentifier(c) || seen[c] {
			return fmt.Errorf("capability is malformed or duplicated")
		}
		seen[c] = true
	}
	if !seen[CapabilityResolve] && !seen[CapabilityResolveWorkflow] {
		return fmt.Errorf("adapter must declare resolve or resolve_workflow capability")
	}
	if len(d.MediaTypes) == 0 {
		return fmt.Errorf("adapter must declare at least one media type")
	}
	media := map[string]bool{}
	for _, m := range d.MediaTypes {
		if !mediaTypeRE.MatchString(m) || media[m] {
			return fmt.Errorf("media type is malformed or duplicated")
		}
		media[m] = true
	}
	if len(d.ResourceTypes) > 256 {
		return fmt.Errorf("descriptor exceeds resource type limit")
	}
	resources := map[string]ResourceType{}
	for _, r := range d.ResourceTypes {
		if !IsValidIdentifier(r.Type) {
			return fmt.Errorf("resource type is malformed")
		}
		if _, ok := resources[r.Type]; ok {
			return fmt.Errorf("resource type is duplicated")
		}
		if err := r.ConfigurationSchema.Validate(); err != nil {
			return fmt.Errorf("resource schema: %w", err)
		}
		resources[r.Type] = r
	}
	for _, r := range d.ResourceTypes {
		parents := map[string]bool{}
		for _, p := range r.ParentTypes {
			if _, ok := resources[p]; !ok || parents[p] {
				return fmt.Errorf("resource parent type is malformed, duplicated, or undeclared")
			}
			parents[p] = true
		}
	}
	visiting, visited := map[string]bool{}, map[string]bool{}
	var visit func(string) bool
	visit = func(kind string) bool {
		if visiting[kind] {
			return false
		}
		if visited[kind] {
			return true
		}
		visiting[kind] = true
		for _, parent := range resources[kind].ParentTypes {
			if !visit(parent) {
				return false
			}
		}
		delete(visiting, kind)
		visited[kind] = true
		return true
	}
	for kind := range resources {
		if !visit(kind) {
			return fmt.Errorf("resource parent declarations contain a cycle")
		}
	}
	return nil
}

// Validate checks the schema vocabulary and bounded shape shared by Core.
func (s Schema) Validate() error {
	if len(s.Fields) > 512 {
		return fmt.Errorf("schema exceeds field limit")
	}
	fields := map[string]Field{}
	for _, f := range s.Fields {
		if !IsValidIdentifier(f.Key) {
			return fmt.Errorf("field key is malformed")
		}
		if _, ok := fields[f.Key]; ok {
			return fmt.Errorf("field keys must be unique")
		}
		fields[f.Key] = f
		if strings.TrimSpace(f.Label) == "" {
			return fmt.Errorf("field %q label is required", f.Key)
		}
		switch f.Control {
		case "text", "secret", "number", "boolean", "select", "multi-select", "textarea", "action", "status":
		default:
			return fmt.Errorf("field %q has unsupported control", f.Key)
		}
		if len(f.Options) > 2048 {
			return fmt.Errorf("field %q exceeds option limit", f.Key)
		}
		if (f.Control == "select" || f.Control == "multi-select") && len(f.Options) == 0 {
			return fmt.Errorf("field %q requires options", f.Key)
		}
		if f.Control != "select" && f.Control != "multi-select" && len(f.Options) > 0 {
			return fmt.Errorf("field %q has unused options", f.Key)
		}
		if f.Control == "secret" && len(f.Default) > 0 {
			return fmt.Errorf("field %q cannot define a secret default", f.Key)
		}
		if (f.Control == "action" || f.Control == "status") && (f.Required || len(f.Default) > 0 || f.Constraints != nil || f.Persistence != nil) {
			return fmt.Errorf("field %q has editable properties on a display-only control", f.Key)
		}
		if len(f.Default) > 0 && !json.Valid(f.Default) {
			return fmt.Errorf("field %q has invalid default JSON", f.Key)
		}
		if len(f.Default) > 0 {
			if err := validateFieldValue(f, f.Default); err != nil {
				return fmt.Errorf("field %q has invalid default", f.Key)
			}
		}
		if f.Constraints != nil {
			c := f.Constraints
			if c.Pattern != "" {
				if _, err := regexp.Compile(c.Pattern); err != nil {
					return fmt.Errorf("field %q has invalid pattern", f.Key)
				}
			}
		}
		if f.Control == "select" || f.Control == "multi-select" {
			seenOptions := map[string]bool{}
			for _, o := range f.Options {
				encoded, err := json.Marshal(o.Value)
				if err != nil {
					return fmt.Errorf("field %q has invalid option", f.Key)
				}
				key := string(encoded)
				if seenOptions[key] {
					return fmt.Errorf("field %q has duplicate options", f.Key)
				}
				seenOptions[key] = true
			}
		}
		if len(f.VisibleWhen) > 0 && !json.Valid(f.VisibleWhen) {
			return fmt.Errorf("field %q has invalid visibility JSON", f.Key)
		}
		if c := f.Constraints; c != nil {
			if (c.Min != nil || c.Max != nil) && f.Control != "number" {
				return fmt.Errorf("field %q has numeric constraints for non-number control", f.Key)
			}
			if (c.MinLength != nil || c.MaxLength != nil || c.Pattern != "") && f.Control != "text" && f.Control != "secret" && f.Control != "textarea" {
				return fmt.Errorf("field %q has string constraints for non-string control", f.Key)
			}
			if (c.MinItems != nil || c.MaxItems != nil) && f.Control != "multi-select" {
				return fmt.Errorf("field %q has item constraints for non-multi-select control", f.Key)
			}
			if (c.Min != nil && (math.IsNaN(*c.Min) || math.IsInf(*c.Min, 0))) || (c.Max != nil && (math.IsNaN(*c.Max) || math.IsInf(*c.Max, 0))) {
				return fmt.Errorf("field %q has non-finite constraints", f.Key)
			}
			if c.Min != nil && c.Max != nil && *c.Min > *c.Max {
				return fmt.Errorf("field %q has inverted numeric bounds", f.Key)
			}
			if c.MinLength != nil && *c.MinLength < 0 || c.MaxLength != nil && *c.MaxLength < 0 || c.MinLength != nil && c.MaxLength != nil && *c.MinLength > *c.MaxLength {
				return fmt.Errorf("field %q has invalid length bounds", f.Key)
			}
			if c.MinItems != nil && *c.MinItems < 0 || c.MaxItems != nil && *c.MaxItems < 0 || c.MinItems != nil && c.MaxItems != nil && *c.MinItems > *c.MaxItems {
				return fmt.Errorf("field %q has invalid item bounds", f.Key)
			}
		}
		for _, o := range f.Options {
			if strings.TrimSpace(o.Label) == "" {
				return fmt.Errorf("field %q option label is required", f.Key)
			}
		}
		if f.Persistence != nil {
			if err := f.Persistence.Validate(); err != nil {
				return fmt.Errorf("field %q persistence: %w", f.Key, err)
			}
		}
	}
	graph := map[string][]string{}
	for _, f := range s.Fields {
		if len(f.VisibleWhen) == 0 {
			continue
		}
		nodes := 0
		refs, err := visibleReferences(f.VisibleWhen, 0, &nodes)
		if err != nil || nodes > 2048 {
			return fmt.Errorf("field %q has invalid visibility condition", f.Key)
		}
		for _, ref := range refs {
			dep, ok := fields[ref]
			if !ok || ref == f.Key || dep.Control == "secret" || dep.Control == "action" || dep.Control == "status" {
				return fmt.Errorf("field %q visibility references an invalid field", f.Key)
			}
			if err := validateVisibilityValues(f.VisibleWhen, fields); err != nil {
				return fmt.Errorf("field %q visibility comparison is invalid", f.Key)
			}
			graph[f.Key] = append(graph[f.Key], ref)
		}
	}
	visiting, visited := map[string]bool{}, map[string]bool{}
	var visit func(string) bool
	visit = func(key string) bool {
		if visiting[key] {
			return false
		}
		if visited[key] {
			return true
		}
		visiting[key] = true
		for _, dep := range graph[key] {
			if !visit(dep) {
				return false
			}
		}
		delete(visiting, key)
		visited[key] = true
		return true
	}
	for key := range graph {
		if !visit(key) {
			return fmt.Errorf("visibility conditions contain a cycle")
		}
	}
	return nil
}

func validateBranding(branding *Branding) error {
	if branding == nil || branding.Icon == nil {
		return nil
	}
	icon := branding.Icon
	if icon.MediaType != "image/png" || len(icon.Data) == 0 || len(icon.Data) > 64<<10 {
		return fmt.Errorf("branding icon media type or size is invalid")
	}
	config, err := png.DecodeConfig(bytes.NewReader(icon.Data))
	if err != nil || config.Width < 1 || config.Height < 1 || config.Width > 512 || config.Height > 512 {
		return fmt.Errorf("branding icon is not a bounded PNG")
	}
	if _, err := png.Decode(bytes.NewReader(icon.Data)); err != nil {
		return fmt.Errorf("branding icon is not a valid PNG")
	}
	return nil
}

func validateFieldValue(field Field, raw json.RawMessage) error {
	if len(raw) == 0 || !json.Valid(raw) {
		return fmt.Errorf("value is invalid JSON")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return err
	}
	constraints := field.Constraints
	switch field.Control {
	case "text", "secret", "textarea":
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("expected string")
		}
		if constraints != nil {
			length := len([]rune(text))
			if constraints.MinLength != nil && length < *constraints.MinLength || constraints.MaxLength != nil && length > *constraints.MaxLength {
				return fmt.Errorf("string length is outside bounds")
			}
			if constraints.Pattern != "" {
				re, err := regexp.Compile(constraints.Pattern)
				if err != nil || !re.MatchString(text) {
					return fmt.Errorf("string does not match pattern")
				}
			}
		}
	case "number":
		n, ok := value.(json.Number)
		if !ok {
			return fmt.Errorf("expected number")
		}
		f, err := n.Float64()
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return fmt.Errorf("number is invalid")
		}
		if constraints != nil && ((constraints.Min != nil && f < *constraints.Min) || (constraints.Max != nil && f > *constraints.Max)) {
			return fmt.Errorf("number is outside bounds")
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("expected boolean")
		}
	case "select":
		if !matchesOption(field.Options, value) {
			return fmt.Errorf("value is not a declared option")
		}
	case "multi-select":
		items, ok := value.([]any)
		if !ok {
			return fmt.Errorf("expected array")
		}
		if constraints != nil && ((constraints.MinItems != nil && len(items) < *constraints.MinItems) || (constraints.MaxItems != nil && len(items) > *constraints.MaxItems)) {
			return fmt.Errorf("array length is outside bounds")
		}
		seen := map[string]bool{}
		for _, item := range items {
			if !matchesOption(field.Options, item) {
				return fmt.Errorf("array item is not a declared option")
			}
			key, err := canonicalValue(item)
			if err != nil || seen[key] {
				return fmt.Errorf("array items are duplicated or invalid")
			}
			seen[key] = true
		}
	default:
		return fmt.Errorf("control has no editable value")
	}
	return nil
}

func matchesOption(options []Option, candidate any) bool {
	key, err := canonicalValue(candidate)
	if err != nil {
		return false
	}
	for _, option := range options {
		k, e := canonicalValue(option.Value)
		if e == nil && k == key {
			return true
		}
	}
	return false
}

// canonicalValue gives JSON values a type-tagged canonical representation;
// number values are compared by exact rational value (so 1 and 1.0 match).
func canonicalValue(v any) (string, error) {
	switch x := v.(type) {
	case nil:
		return "null", nil
	case bool:
		if x {
			return "bool:true", nil
		}
		return "bool:false", nil
	case string:
		b, _ := json.Marshal(x)
		return "string:" + string(b), nil
	case json.Number:
		r, ok := new(big.Rat).SetString(string(x))
		if !ok {
			return "", fmt.Errorf("invalid number")
		}
		return "number:" + r.RatString(), nil
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return "", fmt.Errorf("invalid number")
		}
		r := new(big.Rat)
		r.SetFloat64(x)
		return "number:" + r.RatString(), nil
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			k, err := canonicalValue(e)
			if err != nil {
				return "", err
			}
			parts[i] = k
		}
		return "array:[" + strings.Join(parts, ",") + "]", nil
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			k, _ := json.Marshal(key)
			v, err := canonicalValue(x[key])
			if err != nil {
				return "", err
			}
			parts = append(parts, string(k)+":"+v)
		}
		return "object:{" + strings.Join(parts, ",") + "}", nil
	default:
		b, err := json.Marshal(x)
		if err != nil {
			return "", err
		}
		var decoded any
		d := json.NewDecoder(bytes.NewReader(b))
		d.UseNumber()
		if err := d.Decode(&decoded); err != nil {
			return "", err
		}
		return canonicalValue(decoded)
	}
}

func visibleReferences(raw json.RawMessage, depth int, nodes *int) ([]string, error) {
	if depth > 32 {
		return nil, fmt.Errorf("visibility condition is too deep")
	}
	*nodes++
	if *nodes > 2048 {
		return nil, fmt.Errorf("visibility condition is too large")
	}
	var node map[string]json.RawMessage
	if err := json.Unmarshal(raw, &node); err != nil || node == nil {
		return nil, fmt.Errorf("condition must be an object")
	}
	for _, group := range []string{"all", "any"} {
		if value, ok := node[group]; ok {
			if len(node) != 1 {
				return nil, fmt.Errorf("group operator must be alone")
			}
			var children []json.RawMessage
			if err := json.Unmarshal(value, &children); err != nil || len(children) == 0 {
				return nil, fmt.Errorf("condition group must be nonempty")
			}
			var refs []string
			for _, child := range children {
				r, err := visibleReferences(child, depth+1, nodes)
				if err != nil {
					return nil, err
				}
				refs = append(refs, r...)
			}
			return refs, nil
		}
	}
	fieldRaw, ok := node["field"]
	if !ok || len(node) != 2 {
		return nil, fmt.Errorf("predicate must have one operator")
	}
	var field string
	if json.Unmarshal(fieldRaw, &field) != nil || !IsValidIdentifier(field) {
		return nil, fmt.Errorf("predicate field is invalid")
	}
	if value, ok := node["equals"]; ok {
		if !json.Valid(value) {
			return nil, fmt.Errorf("invalid equals value")
		}
		return []string{field}, nil
	}
	if value, ok := node["not_equals"]; ok {
		if !json.Valid(value) {
			return nil, fmt.Errorf("invalid not_equals value")
		}
		return []string{field}, nil
	}
	if value, ok := node["truthy"]; ok {
		var truth bool
		if json.Unmarshal(value, &truth) != nil {
			return nil, fmt.Errorf("truthy must be boolean")
		}
		return []string{field}, nil
	}
	return nil, fmt.Errorf("unknown predicate operator")
}

func validateVisibilityValues(raw json.RawMessage, fields map[string]Field) error {
	var node map[string]json.RawMessage
	if err := json.Unmarshal(raw, &node); err != nil || node == nil {
		return fmt.Errorf("invalid condition")
	}
	for _, group := range []string{"all", "any"} {
		if value, ok := node[group]; ok {
			var children []json.RawMessage
			if err := json.Unmarshal(value, &children); err != nil {
				return err
			}
			for _, child := range children {
				if err := validateVisibilityValues(child, fields); err != nil {
					return err
				}
			}
			return nil
		}
	}
	var key string
	if json.Unmarshal(node["field"], &key) != nil {
		return fmt.Errorf("invalid condition key")
	}
	field, ok := fields[key]
	if !ok {
		return fmt.Errorf("condition references unknown field")
	}
	if truthy, ok := node["truthy"]; ok {
		if field.Control != "boolean" {
			return fmt.Errorf("truthy requires boolean field")
		}
		var b bool
		return json.Unmarshal(truthy, &b)
	}
	for _, op := range []string{"equals", "not_equals"} {
		if value, ok := node[op]; ok {
			return validateFieldValue(field, value)
		}
	}
	return fmt.Errorf("condition value is missing")
}

func (p FieldPersistence) Validate() error {
	switch p.Mode {
	case PersistenceForbidden, PersistenceOptional, PersistenceRequired:
	default:
		return fmt.Errorf("unknown persistence mode")
	}
	switch p.Target.Scope {
	case PersistencePlugin, PersistenceCurrent:
		if p.Target.Resource != nil {
			return fmt.Errorf("scope does not accept a resource")
		}
	case PersistenceResource:
		if p.Target.Resource == nil {
			return fmt.Errorf("resource scope requires reference")
		}
		if err := ValidateResourceRef(p.Target.Resource); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown persistence scope")
	}
	return nil
}

func ValidateResourceRef(r *ResourceRef) error {
	seen := map[*ResourceRef]bool{}
	depth := 0
	for x := r; x != nil; x = x.Parent {
		depth++
		if depth > 64 {
			return fmt.Errorf("resource hierarchy exceeds limit")
		}
		if seen[x] {
			return fmt.Errorf("resource hierarchy contains a cycle")
		}
		seen[x] = true
		if !IsValidIdentifier(x.Type) || strings.TrimSpace(x.ID) == "" || len(x.ID) > 4096 || !utf8.ValidString(x.ID) {
			return fmt.Errorf("resource type or id is invalid")
		}
	}
	return nil
}

const (
	MaxTitleBytes       = 4 << 10
	MaxDescriptionBytes = 64 << 10
)

func (m MetadataResult) Validate() error {
	if err := validateSourceText(m.Metadata.Title, MaxTitleBytes); err != nil {
		return fmt.Errorf("metadata title is invalid")
	}
	if err := validateSourceText(m.Metadata.Description, MaxDescriptionBytes); err != nil {
		return fmt.Errorf("metadata description is invalid")
	}
	if m.SourceUpdatedAt != nil && (m.SourceUpdatedAt.IsZero() || m.SourceUpdatedAt.Year() < 1 || m.SourceUpdatedAt.Year() > 9999) {
		return fmt.Errorf("source metadata timestamp is invalid")
	}
	if len(m.StateMutations) > 64 {
		return fmt.Errorf("metadata state mutation list exceeds limit")
	}
	return nil
}

func (r WatchCheckResult) Validate() error {
	if r.State != "offline" && r.State != "live" {
		return fmt.Errorf("watch result state must be offline or live")
	}
	if len(r.SessionRef) > 4096 || len(r.Title) > 4096 || !utf8.ValidString(r.SessionRef) || !utf8.ValidString(r.Title) || strings.ContainsRune(r.SessionRef, 0) || strings.ContainsRune(r.Title, 0) {
		return fmt.Errorf("watch result text is invalid")
	}
	if r.StartedAt != nil && (r.StartedAt.IsZero() || r.StartedAt.Year() < 1 || r.StartedAt.Year() > 9999) {
		return fmt.Errorf("watch result timestamp is invalid")
	}
	if r.Media != nil && r.State != "live" {
		return fmt.Errorf("offline result cannot contain media")
	}
	if len(r.StateMutations) > 64 {
		return fmt.Errorf("watch state mutation list exceeds limit")
	}
	return nil
}

func validateSourceText(v *string, max int) error {
	if v == nil {
		return nil
	}
	if len(*v) > max || !utf8.ValidString(*v) || strings.ContainsRune(*v, 0) {
		return fmt.Errorf("invalid text")
	}
	return nil
}
