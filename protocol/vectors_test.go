package protocol

import (
	"bytes"
	"embed"
	"encoding/json"
	"io/fs"
	"path/filepath"
	"reflect"
	"testing"
)

//go:embed adapter-v1/*.json
var goldenVectors embed.FS

func TestGoldenProtocolV1Vectors(t *testing.T) {
	files, err := fs.Glob(goldenVectors, "adapter-v1/*.json")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"adapter-v1/describe.request.json", "adapter-v1/describe.response.json",
		"adapter-v1/resolve.request.json", "adapter-v1/resolve.response.json", "adapter-v1/resolve-historical.response.json", "adapter-v1/resolve-historical-manifest.response.json",
		"adapter-v1/watch-offline.request.json", "adapter-v1/watch-offline.response.json",
		"adapter-v1/watch-live.request.json", "adapter-v1/watch-live.response.json",
		"adapter-v1/metadata.request.json", "adapter-v1/metadata.response.json",
		"adapter-v1/refresh.request.json", "adapter-v1/refresh.response.json",
		"adapter-v1/resource-list.request.json", "adapter-v1/resource-list.response.json",
		"adapter-v1/workflow-begin.request.json", "adapter-v1/workflow-begin.response.json",
		"adapter-v1/workflow-continue.request.json", "adapter-v1/workflow-continue.response.json",
		"adapter-v1/error-unsupported-method.json", "adapter-v1/shutdown.request.json", "adapter-v1/shutdown.response.json",
	}
	if len(files) != len(want) {
		t.Fatalf("golden fixture count=%d, want %d: %v", len(files), len(want), files)
	}
	for _, name := range want {
		data, err := goldenVectors.ReadFile(name)
		if err != nil {
			t.Fatalf("missing golden vector %s: %v", filepath.Base(name), err)
		}
		frame, err := ParseFrame(data)
		if err != nil {
			t.Errorf("%s: parser rejected vector: %v", filepath.Base(name), err)
			continue
		}
		if filepath.Base(name) == "describe.response.json" {
			var descriptor Descriptor
			if err := json.Unmarshal(frame.Response.Result, &descriptor); err != nil {
				t.Errorf("decode descriptor vector: %v", err)
			} else if err := descriptor.Validate(); err != nil {
				t.Errorf("descriptor vector invalid: %v", err)
			}
		}
		if filepath.Base(name) == "watch-offline.response.json" {
			var result WatchCheckResult
			if err := json.Unmarshal(frame.Response.Result, &result); err != nil {
				t.Errorf("decode offline watch vector: %v", err)
			} else if err := result.Validate(); err != nil {
				t.Errorf("offline watch vector invalid: %v", err)
			}
		}
		if filepath.Base(name) == "watch-live.response.json" {
			var result WatchCheckResult
			if err := json.Unmarshal(frame.Response.Result, &result); err != nil {
				t.Errorf("decode live watch vector: %v", err)
			} else if err := result.Validate(); err != nil {
				t.Errorf("live watch vector invalid: %v", err)
			}
		}
	}
}

func TestHistoricalMediaGoldenRoundTrip(t *testing.T) {
	data, err := goldenVectors.ReadFile("adapter-v1/resolve-historical.response.json")
	if err != nil {
		t.Fatal(err)
	}
	frame, err := ParseFrame(data)
	if err != nil {
		t.Fatal(err)
	}
	var media MediaSource
	if err := json.Unmarshal(frame.Response.Result, &media); err != nil {
		t.Fatal(err)
	}
	if err := media.HistoricalAvailability.Validate(); err != nil {
		t.Fatalf("historical availability invalid: %v", err)
	}
	if media.RequestPolicy == nil || media.RequestPolicy.URLTransform == nil {
		t.Fatal("URL transform missing after decode")
	}
	if err := media.RequestPolicy.URLTransform.Validate(); err != nil {
		t.Fatalf("URL transform invalid: %v", err)
	}
	wire, err := json.Marshal(media)
	if err != nil {
		t.Fatal(err)
	}
	var want bytes.Buffer
	if err := json.Compact(&want, frame.Response.Result); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(wire, want.Bytes()) {
		t.Fatalf("typed JSON differs from golden\n got: %s\nwant: %s", wire, want.Bytes())
	}
	var roundTrip MediaSource
	if err := json.Unmarshal(wire, &roundTrip); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(media, roundTrip) {
		t.Fatalf("typed round trip changed media\n got: %#v\nwant: %#v", roundTrip, media)
	}
}

func TestManifestHistoricalMediaGoldenRoundTrip(t *testing.T) {
	data, err := goldenVectors.ReadFile("adapter-v1/resolve-historical-manifest.response.json")
	if err != nil {
		t.Fatal(err)
	}
	frame, err := ParseFrame(data)
	if err != nil {
		t.Fatal(err)
	}
	var media MediaSource
	if err := json.Unmarshal(frame.Response.Result, &media); err != nil {
		t.Fatal(err)
	}
	if media.HistoricalAvailability == nil || media.HistoricalAvailability.Mode != HistoricalModeManifest {
		t.Fatalf("historical mode = %#v, want %q", media.HistoricalAvailability, HistoricalModeManifest)
	}
	if err := media.HistoricalAvailability.Validate(); err != nil {
		t.Fatalf("historical availability invalid: %v", err)
	}
	wire, err := json.Marshal(media)
	if err != nil {
		t.Fatal(err)
	}
	var want bytes.Buffer
	if err := json.Compact(&want, frame.Response.Result); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(wire, want.Bytes()) {
		t.Fatalf("typed JSON differs from golden\n got: %s\nwant: %s", wire, want.Bytes())
	}
	var roundTrip MediaSource
	if err := json.Unmarshal(wire, &roundTrip); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(media, roundTrip) {
		t.Fatalf("typed round trip changed media\n got: %#v\nwant: %#v", roundTrip, media)
	}
}

func TestOptionalMediaFieldsPreserveLegacyJSON(t *testing.T) {
	data, err := goldenVectors.ReadFile("adapter-v1/resolve.response.json")
	if err != nil {
		t.Fatal(err)
	}
	frame, err := ParseFrame(data)
	if err != nil {
		t.Fatal(err)
	}
	var media MediaSource
	if err := json.Unmarshal(frame.Response.Result, &media); err != nil {
		t.Fatal(err)
	}
	wire, err := json.Marshal(media)
	if err != nil {
		t.Fatal(err)
	}
	var want bytes.Buffer
	if err := json.Compact(&want, frame.Response.Result); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(wire, want.Bytes()) {
		t.Fatalf("legacy JSON changed\n got: %s\nwant: %s", wire, want.Bytes())
	}
}
