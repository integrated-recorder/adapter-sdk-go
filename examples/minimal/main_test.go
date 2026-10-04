package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/integrated-recorder/adapter-sdk-go/protocol"
)

func TestDeterministicFixtureCapabilities(t *testing.T) {
	a := &minimal{}
	d := a.Descriptor()
	if d.ID != "example" || d.ProtocolVersion != protocol.Version {
		t.Fatalf("unstable example descriptor: %#v", d)
	}
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(d.InputSchema.Fields) != 1 || d.InputSchema.Fields[0].Key != "live" || d.InputSchema.Fields[0].Control != "boolean" || d.InputSchema.Fields[0].Required {
		t.Fatalf("example must expose `live` as an optional boolean input: %#v", d.InputSchema.Fields)
	}
	var input json.RawMessage = []byte(`{"live":false}`)
	offline, err := a.WatchCheck(context.Background(), protocol.WatchCheckParams{Input: input})
	if err != nil {
		t.Fatal(err)
	}
	if offline.State != "offline" || offline.Media != nil {
		t.Fatalf("offline result: %#v", offline)
	}
	input = []byte(`{"live":true}`)
	live, err := a.WatchCheck(context.Background(), protocol.WatchCheckParams{Input: input})
	if err != nil {
		t.Fatal(err)
	}
	if err := live.Validate(); err != nil {
		t.Fatal(err)
	}
	if live.State != "live" || live.Media == nil || live.SessionRef != "example-session-1" {
		t.Fatalf("live result: %#v", live)
	}
	resolved, err := a.Resolve(context.Background(), protocol.ResolveParams{Input: input})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Media.ManifestURL != "https://example.invalid/live.m3u8" {
		t.Fatalf("resolve result: %#v", resolved)
	}
	metadata, err := a.Metadata(context.Background(), protocol.MetadataParams{Current: resolved.Media})
	if err != nil {
		t.Fatal(err)
	}
	if err := metadata.Validate(); err != nil {
		t.Fatal(err)
	}
	if metadata.Metadata.Title == nil || metadata.Metadata.Description == nil {
		t.Fatalf("metadata result: %#v", metadata)
	}
}
