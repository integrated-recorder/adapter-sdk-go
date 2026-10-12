// integrated-recorder-adapter-timed-events is a deterministic example of the
// optional source timed-event capability. It contains no platform transport.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/integrated-recorder/adapter-sdk-go/adapter"
	"github.com/integrated-recorder/adapter-sdk-go/protocol"
)

type timedEventsExample struct{}

func (*timedEventsExample) Descriptor() protocol.Descriptor {
	return protocol.Descriptor{
		ID: "example-timed-events", Name: "Timed Events Example", Version: "0.1.0", ProtocolVersion: protocol.Version,
		Capabilities:        []string{protocol.CapabilityResolve, protocol.CapabilityEvents},
		InputSchema:         protocol.Schema{Fields: []protocol.Field{}},
		ConfigurationSchema: protocol.Schema{Fields: []protocol.Field{}},
		MediaTypes:          []string{"hls"},
	}
}

func (*timedEventsExample) Resolve(context.Context, protocol.ResolveParams) (protocol.ResolveResult, error) {
	return protocol.ResolveResult{Media: protocol.MediaSource{Type: "hls", ManifestURL: "https://example.invalid/live.m3u8"}}, nil
}

func (*timedEventsExample) StreamTimedEvents(ctx context.Context, params protocol.TimedEventStartParams, emit func(protocol.TimedEventBatch) error) error {
	var resume struct {
		Cursor string `json:"cursor"`
	}
	if len(params.ResumeState) > 0 {
		if err := protocol.DecodeObject(params.ResumeState, &resume); err != nil {
			return adapter.Error("invalid_cursor", "timed event resume state is invalid")
		}
	}
	var events []protocol.TimedEvent
	var nextCursor string
	switch resume.Cursor {
	case "":
		events = []protocol.TimedEvent{{
			EventType: "chat.message", SourceEventID: "example-message-1",
				Actor: &protocol.TimedEventActor{DisplayName: "viewer"},
				Message: &protocol.TimedEventMessage{Text: "first durable message"},
		}}
		nextCursor = "example-message-1"
	case "example-message-1":
		// A reconnect replays message 1 and then adds message 2. Core's stable
		// source identity deduplicates the replay without losing the new event.
		events = []protocol.TimedEvent{
			{EventType: "chat.message", SourceEventID: "example-message-1", Message: &protocol.TimedEventMessage{Text: "first durable message"}},
			{EventType: "chat.message", SourceEventID: "example-message-2", Message: &protocol.TimedEventMessage{Text: "after reconnect"}},
		}
		nextCursor = "example-message-2"
	case "example-message-2":
		<-ctx.Done()
		return nil
	default:
		return adapter.Error("invalid_cursor", "timed event resume state is unknown")
	}
	resumeState, err := json.Marshal(map[string]string{"cursor": nextCursor})
	if err != nil {
		return err
	}
	if err := emit(protocol.TimedEventBatch{
		StreamID: params.StreamID, BatchID: params.StreamID + "-" + nextCursor,
		Events: events, ResumeState: resumeState,
	}); err != nil {
		return err
	}
	<-ctx.Done()
	return nil
}

func main() {
	if err := adapter.Serve(&timedEventsExample{}); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "adapter stopped:", err)
		os.Exit(1)
	}
}
