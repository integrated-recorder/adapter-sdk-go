package protocol

import (
	"strings"
	"testing"
	"time"
)

func TestHistoricalAvailabilityValidate(t *testing.T) {
	start := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	tooManySequences := make([]HistoricalSequenceRange, maxHistoricalRanges+1)
	for i := range tooManySequences {
		tooManySequences[i] = HistoricalSequenceRange{Start: uint64(i * 2), End: uint64(i*2 + 1)}
	}
	tooManyTimes := make([]HistoricalTimeRange, maxHistoricalRanges+1)
	for i := range tooManyTimes {
		from := start.Add(time.Duration(i*2) * time.Hour)
		tooManyTimes[i] = HistoricalTimeRange{Start: from, End: from.Add(time.Hour)}
	}

	tests := []struct {
		name         string
		availability HistoricalAvailability
		wantErr      bool
	}{
		{name: "rolling minimum", availability: HistoricalAvailability{Mode: HistoricalModeRollingWindow, WindowSeconds: 1}},
		{name: "rolling maximum", availability: HistoricalAvailability{Mode: HistoricalModeRollingWindow, WindowSeconds: maxHistoricalWindowSeconds}},
		{name: "sequence ranges", availability: HistoricalAvailability{Mode: HistoricalModeSequenceRanges, SequenceRanges: []HistoricalSequenceRange{{Start: 1, End: 3}, {Start: 5, End: 8}}}},
		{name: "time ranges", availability: HistoricalAvailability{Mode: HistoricalModeTimeRanges, TimeRanges: []HistoricalTimeRange{{Start: start, End: start.Add(time.Hour)}, {Start: start.Add(2 * time.Hour), End: start.Add(3 * time.Hour)}}}},
		{name: "manifest", availability: HistoricalAvailability{Mode: HistoricalModeManifest, HistoricalManifestURL: "https://media.example.test/archive.m3u8?sig=1"}},
		{name: "historical manifest URL", availability: HistoricalAvailability{Mode: HistoricalModeRollingWindow, WindowSeconds: 10, HistoricalManifestURL: "https://media.example.test/archive.m3u8?sig=1"}},
		{name: "unknown mode", availability: HistoricalAvailability{Mode: "unknown", WindowSeconds: 1}, wantErr: true},
		{name: "rolling zero", availability: HistoricalAvailability{Mode: HistoricalModeRollingWindow}, wantErr: true},
		{name: "rolling too large", availability: HistoricalAvailability{Mode: HistoricalModeRollingWindow, WindowSeconds: maxHistoricalWindowSeconds + 1}, wantErr: true},
		{name: "rolling extra sequence field", availability: HistoricalAvailability{Mode: HistoricalModeRollingWindow, WindowSeconds: 1, SequenceRanges: []HistoricalSequenceRange{{Start: 1, End: 1}}}, wantErr: true},
		{name: "sequence empty", availability: HistoricalAvailability{Mode: HistoricalModeSequenceRanges}, wantErr: true},
		{name: "sequence reversed", availability: HistoricalAvailability{Mode: HistoricalModeSequenceRanges, SequenceRanges: []HistoricalSequenceRange{{Start: 2, End: 1}}}, wantErr: true},
		{name: "sequence overlap", availability: HistoricalAvailability{Mode: HistoricalModeSequenceRanges, SequenceRanges: []HistoricalSequenceRange{{Start: 1, End: 4}, {Start: 4, End: 5}}}, wantErr: true},
		{name: "sequence unordered", availability: HistoricalAvailability{Mode: HistoricalModeSequenceRanges, SequenceRanges: []HistoricalSequenceRange{{Start: 5, End: 6}, {Start: 1, End: 2}}}, wantErr: true},
		{name: "sequence too many", availability: HistoricalAvailability{Mode: HistoricalModeSequenceRanges, SequenceRanges: tooManySequences}, wantErr: true},
		{name: "sequence extra window", availability: HistoricalAvailability{Mode: HistoricalModeSequenceRanges, WindowSeconds: 1, SequenceRanges: []HistoricalSequenceRange{{Start: 1, End: 2}}}, wantErr: true},
		{name: "time empty", availability: HistoricalAvailability{Mode: HistoricalModeTimeRanges}, wantErr: true},
		{name: "time zero", availability: HistoricalAvailability{Mode: HistoricalModeTimeRanges, TimeRanges: []HistoricalTimeRange{{End: start}}}, wantErr: true},
		{name: "time equal", availability: HistoricalAvailability{Mode: HistoricalModeTimeRanges, TimeRanges: []HistoricalTimeRange{{Start: start, End: start}}}, wantErr: true},
		{name: "time reversed", availability: HistoricalAvailability{Mode: HistoricalModeTimeRanges, TimeRanges: []HistoricalTimeRange{{Start: start.Add(time.Hour), End: start}}}, wantErr: true},
		{name: "time overlap", availability: HistoricalAvailability{Mode: HistoricalModeTimeRanges, TimeRanges: []HistoricalTimeRange{{Start: start, End: start.Add(2 * time.Hour)}, {Start: start.Add(time.Hour), End: start.Add(3 * time.Hour)}}}, wantErr: true},
		{name: "time unordered", availability: HistoricalAvailability{Mode: HistoricalModeTimeRanges, TimeRanges: []HistoricalTimeRange{{Start: start.Add(3 * time.Hour), End: start.Add(4 * time.Hour)}, {Start: start, End: start.Add(time.Hour)}}}, wantErr: true},
		{name: "time too many", availability: HistoricalAvailability{Mode: HistoricalModeTimeRanges, TimeRanges: tooManyTimes}, wantErr: true},
		{name: "time extra sequence", availability: HistoricalAvailability{Mode: HistoricalModeTimeRanges, SequenceRanges: []HistoricalSequenceRange{{Start: 1, End: 1}}, TimeRanges: []HistoricalTimeRange{{Start: start, End: start.Add(time.Hour)}}}, wantErr: true},
		{name: "sequence extra time", availability: HistoricalAvailability{Mode: HistoricalModeSequenceRanges, SequenceRanges: []HistoricalSequenceRange{{Start: 1, End: 1}}, TimeRanges: []HistoricalTimeRange{{Start: start, End: start.Add(time.Hour)}}}, wantErr: true},
		{name: "manifest missing URL", availability: HistoricalAvailability{Mode: HistoricalModeManifest}, wantErr: true},
		{name: "manifest extra window", availability: HistoricalAvailability{Mode: HistoricalModeManifest, WindowSeconds: 1, HistoricalManifestURL: "https://media.example.test/archive.m3u8"}, wantErr: true},
		{name: "manifest extra sequence ranges", availability: HistoricalAvailability{Mode: HistoricalModeManifest, SequenceRanges: []HistoricalSequenceRange{{Start: 1, End: 1}}, HistoricalManifestURL: "https://media.example.test/archive.m3u8"}, wantErr: true},
		{name: "manifest extra time ranges", availability: HistoricalAvailability{Mode: HistoricalModeManifest, TimeRanges: []HistoricalTimeRange{{Start: start, End: start.Add(time.Hour)}}, HistoricalManifestURL: "https://media.example.test/archive.m3u8"}, wantErr: true},
		{name: "invalid historical URL scheme", availability: HistoricalAvailability{Mode: HistoricalModeRollingWindow, WindowSeconds: 1, HistoricalManifestURL: "ftp://media.example.test/archive.m3u8"}, wantErr: true},
		{name: "historical URL userinfo", availability: HistoricalAvailability{Mode: HistoricalModeRollingWindow, WindowSeconds: 1, HistoricalManifestURL: "https://user@media.example.test/archive.m3u8"}, wantErr: true},
		{name: "historical URL literal hash path", availability: HistoricalAvailability{Mode: HistoricalModeRollingWindow, WindowSeconds: 1, HistoricalManifestURL: "https://media.example.test/archive.m3u8#frag"}},
		{name: "historical URL whitespace", availability: HistoricalAvailability{Mode: HistoricalModeRollingWindow, WindowSeconds: 1, HistoricalManifestURL: "https://media.example.test/archive m3u8"}, wantErr: true},
		{name: "historical URL control", availability: HistoricalAvailability{Mode: HistoricalModeRollingWindow, WindowSeconds: 1, HistoricalManifestURL: "https://media.example.test/archive\nm3u8"}, wantErr: true},
		{name: "historical URL too long", availability: HistoricalAvailability{Mode: HistoricalModeRollingWindow, WindowSeconds: 1, HistoricalManifestURL: "https://media.example.test/" + strings.Repeat("a", maxMediaRequestURLBytes)}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.availability.Validate()
			if (err != nil) != test.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
	if err := (*HistoricalAvailability)(nil).Validate(); err != nil {
		t.Fatalf("nil availability: %v", err)
	}
}

func TestURLTransformPolicyValidate(t *testing.T) {
	validRule := URLTransformRule{
		Scopes:     []ResourceRequestScope{RequestScopeMedia},
		PathSuffix: &PathSuffixRewrite{From: ".old", To: ".new"},
		QueryParameters: []QueryParameterPropagation{
			{From: "signature", To: "token"},
		},
	}
	tooManyRules := make([]URLTransformRule, maxURLTransformRules+1)
	for i := range tooManyRules {
		tooManyRules[i] = URLTransformRule{Scopes: []ResourceRequestScope{RequestScopeMedia}, PathSuffix: &PathSuffixRewrite{From: ".old", To: ".new" + string(rune('a'+i))}}
	}
	tooManyOrigins := make([]string, maxURLTransformOrigins+1)
	for i := range tooManyOrigins {
		tooManyOrigins[i] = "https://origin" + string(rune('a'+i)) + ".example.test"
	}
	tooManyCopies := make([]QueryParameterPropagation, maxURLTransformQueryCopies+1)
	for i := range tooManyCopies {
		tooManyCopies[i] = QueryParameterPropagation{From: "from" + string(rune('a'+i)), To: "to" + string(rune('a'+i))}
	}
	tooManyScopes := []ResourceRequestScope{RequestScopeManifest, RequestScopeVariant, RequestScopeMedia, RequestScopeInit, RequestScopeKey, "extra"}

	tests := []struct {
		name    string
		policy  URLTransformPolicy
		wantErr bool
	}{
		{name: "all request scopes", policy: URLTransformPolicy{Rules: []URLTransformRule{{Scopes: []ResourceRequestScope{RequestScopeManifest, RequestScopeVariant, RequestScopeMedia, RequestScopeInit, RequestScopeKey}, PathSuffix: &PathSuffixRewrite{From: ".old", To: ".new"}}}}},
		{name: "valid combined policy", policy: URLTransformPolicy{AllowedOrigins: []string{"https://cdn.example.test"}, Rules: []URLTransformRule{validRule}}},
		{name: "valid no-op policy", policy: URLTransformPolicy{}},
		{name: "valid empty suffix target", policy: URLTransformPolicy{Rules: []URLTransformRule{{Scopes: []ResourceRequestScope{RequestScopeKey}, PathSuffix: &PathSuffixRewrite{From: ".old"}}}}},
		{name: "invalid scope", policy: URLTransformPolicy{Rules: []URLTransformRule{{Scopes: []ResourceRequestScope{"unknown"}, PathSuffix: &PathSuffixRewrite{From: ".old", To: ".new"}}}}, wantErr: true},
		{name: "duplicate scope", policy: URLTransformPolicy{Rules: []URLTransformRule{{Scopes: []ResourceRequestScope{RequestScopeMedia, RequestScopeMedia}, PathSuffix: &PathSuffixRewrite{From: ".old", To: ".new"}}}}, wantErr: true},
		{name: "no scopes", policy: URLTransformPolicy{Rules: []URLTransformRule{{PathSuffix: &PathSuffixRewrite{From: ".old", To: ".new"}}}}, wantErr: true},
		{name: "too many scopes", policy: URLTransformPolicy{Rules: []URLTransformRule{{Scopes: tooManyScopes, PathSuffix: &PathSuffixRewrite{From: ".old", To: ".new"}}}}, wantErr: true},
		{name: "no operation", policy: URLTransformPolicy{Rules: []URLTransformRule{{Scopes: []ResourceRequestScope{RequestScopeMedia}}}}, wantErr: true},
		{name: "too many rules", policy: URLTransformPolicy{Rules: tooManyRules}, wantErr: true},
		{name: "too many origins", policy: URLTransformPolicy{AllowedOrigins: tooManyOrigins}, wantErr: true},
		{name: "malformed origin scheme", policy: URLTransformPolicy{AllowedOrigins: []string{"ftp://cdn.example.test"}}, wantErr: true},
		{name: "origin path", policy: URLTransformPolicy{AllowedOrigins: []string{"https://cdn.example.test/path"}}, wantErr: true},
		{name: "origin query", policy: URLTransformPolicy{AllowedOrigins: []string{"https://cdn.example.test?x=1"}}, wantErr: true},
		{name: "origin fragment", policy: URLTransformPolicy{AllowedOrigins: []string{"https://cdn.example.test#x"}}, wantErr: true},
		{name: "origin userinfo", policy: URLTransformPolicy{AllowedOrigins: []string{"https://user@cdn.example.test"}}, wantErr: true},
		{name: "origin whitespace", policy: URLTransformPolicy{AllowedOrigins: []string{"https://cdn.example.test/ "}}, wantErr: true},
		{name: "duplicate canonical origins", policy: URLTransformPolicy{AllowedOrigins: []string{"https://cdn.example.test", "https://CDN.example.test:443/"}}, wantErr: true},
		{name: "empty suffix from", policy: URLTransformPolicy{Rules: []URLTransformRule{{Scopes: []ResourceRequestScope{RequestScopeMedia}, PathSuffix: &PathSuffixRewrite{To: ".new"}}}}, wantErr: true},
		{name: "suffix separator", policy: URLTransformPolicy{Rules: []URLTransformRule{{Scopes: []ResourceRequestScope{RequestScopeMedia}, PathSuffix: &PathSuffixRewrite{From: ".old/path", To: ".new"}}}}, wantErr: true},
		{name: "suffix backslash", policy: URLTransformPolicy{Rules: []URLTransformRule{{Scopes: []ResourceRequestScope{RequestScopeMedia}, PathSuffix: &PathSuffixRewrite{From: `.old\\part`, To: ".new"}}}}, wantErr: true},
		{name: "suffix query marker", policy: URLTransformPolicy{Rules: []URLTransformRule{{Scopes: []ResourceRequestScope{RequestScopeMedia}, PathSuffix: &PathSuffixRewrite{From: ".old?x", To: ".new"}}}}, wantErr: true},
		{name: "suffix dot segment", policy: URLTransformPolicy{Rules: []URLTransformRule{{Scopes: []ResourceRequestScope{RequestScopeMedia}, PathSuffix: &PathSuffixRewrite{From: "..", To: ".new"}}}}, wantErr: true},
		{name: "suffix too long", policy: URLTransformPolicy{Rules: []URLTransformRule{{Scopes: []ResourceRequestScope{RequestScopeMedia}, PathSuffix: &PathSuffixRewrite{From: strings.Repeat("a", maxURLTransformSuffixBytes+1), To: ".new"}}}}, wantErr: true},
		{name: "suffix non ASCII", policy: URLTransformPolicy{Rules: []URLTransformRule{{Scopes: []ResourceRequestScope{RequestScopeMedia}, PathSuffix: &PathSuffixRewrite{From: "é", To: ".new"}}}}, wantErr: true},
		{name: "too many query copies", policy: URLTransformPolicy{Rules: []URLTransformRule{{Scopes: []ResourceRequestScope{RequestScopeMedia}, QueryParameters: tooManyCopies}}}, wantErr: true},
		{name: "invalid query name", policy: URLTransformPolicy{Rules: []URLTransformRule{{Scopes: []ResourceRequestScope{RequestScopeMedia}, QueryParameters: []QueryParameterPropagation{{From: "bad name", To: "target"}}}}}, wantErr: true},
		{name: "duplicate query target", policy: URLTransformPolicy{Rules: []URLTransformRule{{Scopes: []ResourceRequestScope{RequestScopeMedia}, QueryParameters: []QueryParameterPropagation{{From: "source_a", To: "target"}, {From: "source_b", To: "target"}}}}}, wantErr: true},
		{name: "duplicate canonical rules", policy: URLTransformPolicy{Rules: []URLTransformRule{{Scopes: []ResourceRequestScope{RequestScopeMedia, RequestScopeInit}, PathSuffix: &PathSuffixRewrite{From: ".old", To: ".new"}}, {Scopes: []ResourceRequestScope{RequestScopeInit, RequestScopeMedia}, PathSuffix: &PathSuffixRewrite{From: ".old", To: ".new"}}}}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.policy.Validate()
			if (err != nil) != test.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
	if err := (*URLTransformPolicy)(nil).Validate(); err != nil {
		t.Fatalf("nil policy: %v", err)
	}
}
