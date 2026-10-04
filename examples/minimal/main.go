// integrated-recorder-adapter-example is a deterministic Protocol v1 example.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/integrated-recorder/adapter-sdk-go/adapter"
	"github.com/integrated-recorder/adapter-sdk-go/protocol"
)

type minimal struct{}

func (*minimal) Descriptor() protocol.Descriptor {
	return protocol.Descriptor{
		ID: "example", Name: "Minimal Example", Version: "0.1.0", ProtocolVersion: protocol.Version,
		Capabilities: []string{protocol.CapabilityResolve, protocol.CapabilityWatch, protocol.CapabilityMetadata},
		InputSchema: protocol.Schema{Fields: []protocol.Field{{
			Key: "live", Control: "boolean", Label: "Live",
		}}},
		ConfigurationSchema: protocol.Schema{Fields: []protocol.Field{}},
		MediaTypes:          []string{"hls"},
	}
}

func (*minimal) Resolve(_ context.Context, p protocol.ResolveParams) (protocol.ResolveResult, error) {
	if _, err := parseLive(p.Input); err != nil {
		return protocol.ResolveResult{}, adapter.Error("invalid_input", "input must be a JSON object with an optional live boolean")
	}
	return protocol.ResolveResult{Media: fixtureMedia()}, nil
}

func (*minimal) WatchCheck(_ context.Context, p protocol.WatchCheckParams) (protocol.WatchCheckResult, error) {
	live, err := parseLive(p.Input)
	if err != nil {
		return protocol.WatchCheckResult{}, adapter.Error("invalid_input", "input must be a JSON object with an optional live boolean")
	}
	if !live {
		return protocol.WatchCheckResult{State: "offline"}, nil
	}
	started := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	media := fixtureMedia()
	return protocol.WatchCheckResult{State: "live", SessionRef: "example-session-1", Title: "Minimal example stream", StartedAt: &started, Media: &media}, nil
}

func (*minimal) Metadata(_ context.Context, p protocol.MetadataParams) (protocol.MetadataResult, error) {
	if p.Current.Type != "hls" {
		return protocol.MetadataResult{}, adapter.Error("unsupported_media", "metadata is available for HLS media")
	}
	title := "Minimal example stream"
	description := "A deterministic stream returned by the Go SDK example."
	updated := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	return protocol.MetadataResult{Metadata: protocol.StreamMetadata{Title: &title, Description: &description}, SourceUpdatedAt: &updated}, nil
}

func parseLive(raw json.RawMessage) (bool, error) {
	var input struct {
		Live bool `json:"live"`
	}
	if len(raw) == 0 {
		return false, nil
	}
	if err := protocol.DecodeObject(raw, &input); err != nil {
		return false, err
	}
	return input.Live, nil
}

func fixtureMedia() protocol.MediaSource {
	return protocol.MediaSource{Type: "hls", ManifestURL: "https://example.invalid/live.m3u8", SessionRef: "example-session-1"}
}

func main() {
	if err := adapter.Serve(&minimal{}); err != nil {
		// Process diagnostics belong on stderr; stdout remains protocol-only.
		_, _ = fmt.Fprintln(os.Stderr, "adapter stopped:", err)
		os.Exit(1)
	}
}
