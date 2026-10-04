package adaptertest_test

import (
	"context"
	"testing"

	"github.com/integrated-recorder/adapter-sdk-go/adaptertest"
	"github.com/integrated-recorder/adapter-sdk-go/protocol"
)

type fixture struct{}

func (*fixture) Descriptor() protocol.Descriptor {
	return protocol.Descriptor{ID: "fixture", Name: "fixture", Version: "1", ProtocolVersion: protocol.Version, Capabilities: []string{protocol.CapabilityResolve}, InputSchema: protocol.Schema{Fields: []protocol.Field{}}, ConfigurationSchema: protocol.Schema{Fields: []protocol.Field{}}, MediaTypes: []string{"hls"}}
}
func (*fixture) Resolve(context.Context, protocol.ResolveParams) (protocol.ResolveResult, error) {
	return protocol.ResolveResult{Media: protocol.MediaSource{Type: "hls", ManifestURL: "https://fixture.invalid/live.m3u8"}}, nil
}

func TestValidationAndServeRoundTrip(t *testing.T) {
	a := &fixture{}
	if err := adaptertest.ValidateDescriptor(a.Descriptor()); err != nil {
		t.Fatal(err)
	}
	if err := adaptertest.ValidateAdapter(a); err != nil {
		t.Fatal(err)
	}
	responses, err := adaptertest.ServeRoundTrip(context.Background(), a, protocol.Request{ProtocolVersion: protocol.Version, ID: "resolve-1", Method: protocol.MethodResolve, Params: []byte(`{"input":{}}`)})
	if err != nil {
		t.Fatal(err)
	}
	if len(responses) != 2 {
		t.Fatalf("responses=%d", len(responses))
	}
	if responses[0].ID != "resolve-1" || responses[0].Error != nil {
		t.Fatalf("resolve response %#v", responses[0])
	}
	if responses[1].ID != "adaptertest-shutdown" {
		t.Fatalf("shutdown response %#v", responses[1])
	}
}
