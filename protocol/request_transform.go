package protocol

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	HistoricalModeRollingWindow  = "rolling_window"
	HistoricalModeSequenceRanges = "sequence_ranges"
	HistoricalModeTimeRanges     = "time_ranges"
	HistoricalModeManifest       = "manifest"
)

const (
	RequestScopeManifest ResourceRequestScope = "manifest"
	RequestScopeVariant  ResourceRequestScope = "variant"
	RequestScopeMedia    ResourceRequestScope = "media"
	RequestScopeInit     ResourceRequestScope = "init"
	RequestScopeKey      ResourceRequestScope = "key"
)

const (
	maxHistoricalRanges        = 64
	maxHistoricalWindowSeconds = 366 * 24 * 60 * 60
	maxMediaRequestURLBytes    = 8192
	maxURLTransformRules       = 16
	maxURLTransformScopes      = 5
	maxURLTransformQueryCopies = 8
	maxURLTransformOrigins     = 16
	maxURLTransformSuffixBytes = 128
)

var safeQueryParameterName = regexp.MustCompile(`^[A-Za-z0-9_.~-]{1,128}$`)

// HistoricalAvailability declares source media that Core may acquire later.
// It does not assert that Core already stores the media or guarantee a fetch.
type HistoricalAvailability struct {
	Mode                  string                    `json:"mode"`
	WindowSeconds         int64                     `json:"window_seconds,omitempty"`
	SequenceRanges        []HistoricalSequenceRange `json:"sequence_ranges,omitempty"`
	TimeRanges            []HistoricalTimeRange     `json:"time_ranges,omitempty"`
	HistoricalManifestURL string                    `json:"historical_manifest_url,omitempty"`
}

type HistoricalSequenceRange struct {
	Start uint64 `json:"start"`
	End   uint64 `json:"end"`
}

type HistoricalTimeRange struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// Validate checks bounded historical availability fields before serialization.
func (availability *HistoricalAvailability) Validate() error {
	if availability == nil {
		return nil
	}
	if availability.HistoricalManifestURL != "" {
		if _, err := parseBoundedHTTPURL(availability.HistoricalManifestURL); err != nil {
			return fmt.Errorf("historical manifest URL is invalid")
		}
	}
	switch availability.Mode {
	case HistoricalModeRollingWindow:
		if availability.WindowSeconds < 1 || availability.WindowSeconds > maxHistoricalWindowSeconds || len(availability.SequenceRanges) != 0 || len(availability.TimeRanges) != 0 {
			return fmt.Errorf("rolling historical window is invalid")
		}
	case HistoricalModeSequenceRanges:
		if availability.WindowSeconds != 0 || len(availability.SequenceRanges) == 0 || len(availability.SequenceRanges) > maxHistoricalRanges || len(availability.TimeRanges) != 0 {
			return fmt.Errorf("historical sequence ranges are invalid")
		}
		for i, span := range availability.SequenceRanges {
			if span.Start > span.End || i > 0 && span.Start <= availability.SequenceRanges[i-1].End {
				return fmt.Errorf("historical sequence ranges must be ordered and non-overlapping")
			}
		}
	case HistoricalModeTimeRanges:
		if availability.WindowSeconds != 0 || len(availability.TimeRanges) == 0 || len(availability.TimeRanges) > maxHistoricalRanges || len(availability.SequenceRanges) != 0 {
			return fmt.Errorf("historical time ranges are invalid")
		}
		for i, span := range availability.TimeRanges {
			if span.Start.IsZero() || span.End.IsZero() || !span.Start.Before(span.End) || span.Start.Year() < 1 || span.Start.Year() > 9999 || span.End.Year() < 1 || span.End.Year() > 9999 {
				return fmt.Errorf("historical time range is invalid")
			}
			if i > 0 && !availability.TimeRanges[i-1].End.Before(span.Start) {
				return fmt.Errorf("historical time ranges must be ordered and non-overlapping")
			}
		}
	case HistoricalModeManifest:
		if availability.HistoricalManifestURL == "" || availability.WindowSeconds != 0 || len(availability.SequenceRanges) != 0 || len(availability.TimeRanges) != 0 {
			return fmt.Errorf("historical manifest availability is invalid")
		}
	default:
		return fmt.Errorf("unsupported historical availability mode")
	}
	return nil
}

// ResourceRequestScope selects request classes where a URL transform applies.
type ResourceRequestScope string

// URLTransformPolicy declares bounded URL transformations. Core owns URL
// resolution, request execution, and network safety checks.
type URLTransformPolicy struct {
	AllowedOrigins []string           `json:"allowed_origins,omitempty"`
	Rules          []URLTransformRule `json:"rules,omitempty"`
}

// URLTransformRule applies declarative operations to selected request scopes.
type URLTransformRule struct {
	Scopes          []ResourceRequestScope      `json:"scopes"`
	PathSuffix      *PathSuffixRewrite          `json:"path_suffix,omitempty"`
	QueryParameters []QueryParameterPropagation `json:"query_parameters,omitempty"`
}

// PathSuffixRewrite replaces a literal path suffix.
type PathSuffixRewrite struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// QueryParameterPropagation copies one selected query value from the source URL.
type QueryParameterPropagation struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Validate checks bounds and syntax for a declarative URL transform policy.
// Core still applies the policy and performs authoritative network checks.
func (policy *URLTransformPolicy) Validate() error {
	if policy == nil {
		return nil
	}
	if len(policy.Rules) > maxURLTransformRules || len(policy.AllowedOrigins) > maxURLTransformOrigins {
		return fmt.Errorf("URL transform policy exceeds limit")
	}
	seenOrigins := map[mediaOrigin]bool{}
	for _, raw := range policy.AllowedOrigins {
		origin, err := parseAllowedOrigin(raw)
		if err != nil {
			return fmt.Errorf("invalid URL transform allowed origin")
		}
		if seenOrigins[origin] {
			return fmt.Errorf("URL transform origins must be unique")
		}
		seenOrigins[origin] = true
	}
	seenRules := map[string]bool{}
	for _, rule := range policy.Rules {
		if len(rule.Scopes) == 0 || len(rule.Scopes) > maxURLTransformScopes || len(rule.QueryParameters) > maxURLTransformQueryCopies {
			return fmt.Errorf("URL transform rule exceeds limit")
		}
		if rule.PathSuffix == nil && len(rule.QueryParameters) == 0 {
			return fmt.Errorf("URL transform rule has no operation")
		}
		seenScopes := map[ResourceRequestScope]bool{}
		scopes := append([]ResourceRequestScope(nil), rule.Scopes...)
		for _, scope := range scopes {
			if !validRequestScope(scope) || seenScopes[scope] {
				return fmt.Errorf("URL transform scopes must be valid and unique")
			}
			seenScopes[scope] = true
		}
		sort.Slice(scopes, func(i, j int) bool { return scopes[i] < scopes[j] })
		if rule.PathSuffix != nil && (!validPathSuffix(rule.PathSuffix.From, false) || !validPathSuffix(rule.PathSuffix.To, true)) {
			return fmt.Errorf("URL transform path suffix is invalid")
		}
		seenTargets := map[string]bool{}
		seenPairs := map[string]bool{}
		copies := append([]QueryParameterPropagation(nil), rule.QueryParameters...)
		sort.Slice(copies, func(i, j int) bool {
			if copies[i].From == copies[j].From {
				return copies[i].To < copies[j].To
			}
			return copies[i].From < copies[j].From
		})
		for _, copyRule := range copies {
			if !safeQueryParameterName.MatchString(copyRule.From) || !safeQueryParameterName.MatchString(copyRule.To) {
				return fmt.Errorf("URL transform query parameter name is invalid")
			}
			if seenTargets[copyRule.To] || seenPairs[copyRule.From+"\x00"+copyRule.To] {
				return fmt.Errorf("URL transform query mappings must be unique")
			}
			seenTargets[copyRule.To] = true
			seenPairs[copyRule.From+"\x00"+copyRule.To] = true
		}
		key := canonicalTransformRuleKey(scopes, rule.PathSuffix, copies)
		if seenRules[key] {
			return fmt.Errorf("URL transform rules must be unique")
		}
		seenRules[key] = true
	}
	return nil
}

func canonicalTransformRuleKey(scopes []ResourceRequestScope, suffix *PathSuffixRewrite, copies []QueryParameterPropagation) string {
	var key strings.Builder
	for _, scope := range scopes {
		key.WriteString(string(scope))
		key.WriteByte(',')
	}
	key.WriteByte('|')
	if suffix != nil {
		key.WriteString(suffix.From)
		key.WriteByte('>')
		key.WriteString(suffix.To)
	}
	key.WriteByte('|')
	for _, copyRule := range copies {
		key.WriteString(copyRule.From)
		key.WriteByte('>')
		key.WriteString(copyRule.To)
		key.WriteByte(',')
	}
	return key.String()
}

func validRequestScope(scope ResourceRequestScope) bool {
	switch scope {
	case RequestScopeManifest, RequestScopeVariant, RequestScopeMedia, RequestScopeInit, RequestScopeKey:
		return true
	default:
		return false
	}
}

func validPathSuffix(value string, allowEmpty bool) bool {
	if value == "" {
		return allowEmpty
	}
	if !validBoundedText(value, maxURLTransformSuffixBytes, false) || strings.ContainsAny(value, `/\\?#%`) || value == "." || value == ".." {
		return false
	}
	for _, r := range value {
		if r < 0x21 || r > 0x7e {
			return false
		}
	}
	return true
}

type mediaOrigin struct {
	scheme   string
	hostname string
	port     string
}

func parseAllowedOrigin(raw string) (mediaOrigin, error) {
	if raw == "" || strings.ContainsAny(raw, "\\\r\n\t #?") {
		return mediaOrigin{}, fmt.Errorf("origin must be an HTTP(S) origin URL")
	}
	u, err := url.Parse(raw)
	if err != nil || u == nil || u.Path != "" && u.Path != "/" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawFragment != "" || strings.Contains(raw, "#") {
		return mediaOrigin{}, fmt.Errorf("origin must not include path, query, or fragment")
	}
	return originFromURL(u)
}

func originFromURL(u *url.URL) (mediaOrigin, error) {
	if u == nil || u.Opaque != "" || u.User != nil || !strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https") || u.Host == "" || u.Hostname() == "" || strings.HasSuffix(u.Host, ":") {
		return mediaOrigin{}, fmt.Errorf("URL does not have a valid HTTP(S) origin")
	}
	scheme := strings.ToLower(u.Scheme)
	hostname := strings.ToLower(u.Hostname())
	port := u.Port()
	if port == "" {
		if scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	} else {
		portNumber, err := strconv.Atoi(port)
		if err != nil || portNumber < 0 || portNumber > 65535 {
			return mediaOrigin{}, fmt.Errorf("URL port is invalid")
		}
		port = strconv.Itoa(portNumber)
	}
	return mediaOrigin{scheme: scheme, hostname: hostname, port: port}, nil
}

func validBoundedText(value string, max int, allowEmpty bool) bool {
	if (!allowEmpty && value == "") || len(value) > max || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if r == 0x7f || r < 0x20 {
			return false
		}
	}
	return true
}

func parseBoundedHTTPURL(raw string) (*url.URL, error) {
	if !validBoundedText(raw, maxMediaRequestURLBytes, false) || strings.ContainsAny(raw, "\\\r\n\t ") {
		return nil, fmt.Errorf("URL is malformed")
	}
	u, err := url.ParseRequestURI(raw)
	if err != nil || u == nil || u.Opaque != "" || u.User != nil || u.Fragment != "" || u.RawFragment != "" {
		return nil, fmt.Errorf("URL is malformed")
	}
	if _, err := originFromURL(u); err != nil {
		return nil, err
	}
	return u, nil
}
