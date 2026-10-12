package adapter

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/integrated-recorder/adapter-sdk-go/protocol"
)

type testAdapter struct {
	descriptor protocol.Descriptor
	resolve    func(context.Context, protocol.ResolveParams) (protocol.ResolveResult, error)
	calls      atomic.Int32
}

func (a *testAdapter) Descriptor() protocol.Descriptor { return a.descriptor }
func (a *testAdapter) Resolve(ctx context.Context, p protocol.ResolveParams) (protocol.ResolveResult, error) {
	a.calls.Add(1)
	if a.resolve != nil {
		return a.resolve(ctx, p)
	}
	return protocol.ResolveResult{Media: protocol.MediaSource{Type: "hls", ManifestURL: "https://example.test/live.m3u8"}}, nil
}

func newTestAdapter() *testAdapter {
	return &testAdapter{descriptor: protocol.Descriptor{ID: "sdk-test", Name: "SDK test", Version: "1", ProtocolVersion: protocol.Version, Capabilities: []string{protocol.CapabilityResolve}, InputSchema: protocol.Schema{Fields: []protocol.Field{}}, ConfigurationSchema: protocol.Schema{Fields: []protocol.Field{}}, MediaTypes: []string{"hls"}}}
}

func TestServeIOFramesDescribeResolveUnsupportedAndShutdown(t *testing.T) {
	a := newTestAdapter()
	input := strings.Join([]string{
		`{"protocol_version":1,"id":"describe-1","method":"describe"}`,
		`{"protocol_version":1,"id":"resolve-1","method":"resolve","params":{"input":{"url":"https://source.invalid"}}}`,
		`{"protocol_version":1,"id":"unknown-1","method":"made.up"}`,
		`{"protocol_version":1,"id":"shutdown-1","method":"shutdown"}`,
		`{"protocol_version":1,"id":"ignored-1","method":"resolve"}`,
	}, "\n") + "\n"
	var output strings.Builder
	if err := ServeIO(context.Background(), a, strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(strings.NewReader(output.String()))
	wantIDs := []string{"describe-1", "resolve-1", "unknown-1", "shutdown-1"}
	for i, id := range wantIDs {
		res, err := protocol.ReadResponse(reader)
		if err != nil {
			t.Fatalf("response %d: %v", i, err)
		}
		if res.ID != id || res.ProtocolVersion != protocol.Version {
			t.Fatalf("response envelope mismatch: %#v", res)
		}
		if id == "unknown-1" && (res.Error == nil || res.Error.Code != "unsupported_method") {
			t.Fatalf("unknown method response %#v", res)
		}
		if id == "describe-1" {
			var d protocol.Descriptor
			if err := json.Unmarshal(res.Result, &d); err != nil {
				t.Fatal(err)
			}
			if d.ID != "sdk-test" {
				t.Fatalf("describe ID %q", d.ID)
			}
		}
		if id == "shutdown-1" && string(res.Result) != "{}" {
			t.Fatalf("shutdown result = %s", res.Result)
		}
	}
	if _, err := protocol.ReadResponse(reader); !errors.Is(err, io.EOF) {
		t.Fatalf("extra response: %v", err)
	}
	if a.calls.Load() != 1 {
		t.Fatalf("resolve call count = %d", a.calls.Load())
	}
}

type timedEventTestAdapter struct{ descriptor protocol.Descriptor }

func (a *timedEventTestAdapter) Descriptor() protocol.Descriptor { return a.descriptor }
func (*timedEventTestAdapter) Resolve(context.Context, protocol.ResolveParams) (protocol.ResolveResult, error) {
	return protocol.ResolveResult{}, nil
}
func (*timedEventTestAdapter) StreamTimedEvents(ctx context.Context, params protocol.TimedEventStartParams, emit func(protocol.TimedEventBatch) error) error {
	return emit(protocol.TimedEventBatch{StreamID: params.StreamID, BatchID: "batch-1", Events: []protocol.TimedEvent{{EventType: "chat.message", Payload: json.RawMessage(`{"text":"hello"}`)}}})
}

func TestTimedEventStreamWaitsForDurableCoreAck(t *testing.T) {
	a := &timedEventTestAdapter{descriptor: protocol.Descriptor{
		ID: "timed-test", Name: "Timed test", Version: "1", ProtocolVersion: protocol.Version,
		Capabilities: []string{protocol.CapabilityResolve, protocol.CapabilityEvents}, InputSchema: protocol.Schema{Fields: []protocol.Field{}},
		ConfigurationSchema: protocol.Schema{Fields: []protocol.Field{}}, MediaTypes: []string{"hls"},
	}}
	inputReader, inputWriter := io.Pipe()
	outputReader, outputWriter := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- ServeIO(context.Background(), a, inputReader, outputWriter) }()
	reader := bufio.NewReader(outputReader)
	write := func(req protocol.Request) {
		t.Helper()
		if err := protocol.WriteRequest(inputWriter, req); err != nil {
			t.Fatal(err)
		}
	}
	read := func() protocol.Frame {
		t.Helper()
		frame, err := protocol.ReadFrame(reader)
		if err != nil {
			t.Fatal(err)
		}
		return frame
	}
	start, _ := json.Marshal(protocol.TimedEventStartParams{StreamID: "stream-1"})
	write(protocol.Request{ProtocolVersion: 1, ID: "start", Method: protocol.MethodEventsStart, Params: start})
	startFrame := read()
	if startFrame.Kind != protocol.FrameTypeResponse || startFrame.Response.Error != nil {
		t.Fatalf("start response: %#v", startFrame)
	}
	notification := read()
	if notification.Kind != protocol.FrameTypeNotification || notification.Notification.Method != protocol.MethodEventsBatch {
		t.Fatalf("event notification: %#v", notification)
	}
	var batch protocol.TimedEventBatch
	if err := json.Unmarshal(notification.Notification.Params, &batch); err != nil || batch.BatchID != "batch-1" {
		t.Fatalf("batch=%#v err=%v", batch, err)
	}
	ack, _ := json.Marshal(protocol.TimedEventAckParams{StreamID: "stream-1", BatchID: "batch-1"})
	write(protocol.Request{ProtocolVersion: 1, ID: "ack", Method: protocol.MethodEventsAck, Params: ack})
	ackFrame := read()
	if ackFrame.Kind != protocol.FrameTypeResponse || ackFrame.Response.Error != nil {
		t.Fatalf("ack response: %#v", ackFrame)
	}
	ended := read()
	if ended.Kind != protocol.FrameTypeNotification || ended.Notification.Method != protocol.MethodEventsEnded {
		t.Fatalf("stream completion notification: %#v", ended)
	}
	write(protocol.Request{ProtocolVersion: 1, ID: "shutdown", Method: protocol.MethodShutdown})
	if frame := read(); frame.Kind != protocol.FrameTypeResponse || frame.Response.Error != nil {
		t.Fatalf("shutdown response: %#v", frame)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	_ = inputWriter.Close()
	_ = outputReader.Close()
}

func TestTimedEventBatcherFlushesByTimeAndWaitsForAck(t *testing.T) {
	emitted := make(chan protocol.TimedEventBatch, 1)
	ack := make(chan struct{})
	batcher, err := NewTimedEventBatcher("stream", func(batch protocol.TimedEventBatch) error {
		emitted <- batch
		<-ack
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := batcher.Add(protocol.TimedEvent{EventType: "chat.message", Payload: json.RawMessage(`{"text":"hello"}`)}, json.RawMessage(`{"cursor":"c1"}`)); err != nil {
		t.Fatal(err)
	}
	select {
	case batch := <-emitted:
		if len(batch.Events) != 1 || string(batch.ResumeState) != `{"cursor":"c1"}` {
			t.Fatalf("batch=%#v", batch)
		}
	case <-time.After(time.Second):
		t.Fatal("event batch did not flush on the time bound")
	}
	flushDone := make(chan error, 1)
	go func() { flushDone <- batcher.Flush(context.Background()) }()
	close(ack)
	select {
	case err := <-flushDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("flush did not finish after Core acknowledgement")
	}
}

func TestTimedEventBatcherBoundsBatchBytesAndQueuedEvents(t *testing.T) {
	started := make(chan protocol.TimedEventBatch, 8)
	batcher, err := NewTimedEventBatcher("stream", func(batch protocol.TimedEventBatch) error {
		started <- batch
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	large := protocol.TimedEvent{EventType: "chat.message", Message: &protocol.TimedEventMessage{Text: strings.Repeat("x", 30<<10)}}
	addDone := make(chan error, 1)
	go func() {
		for i := 0; i < 32; i++ {
			if err := batcher.Add(large, nil); err != nil {
				addDone <- err
				return
			}
		}
		addDone <- nil
	}()
	select {
	case err := <-addDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("bounded batches did not drain")
	}
	total := 0
	for {
		select {
		case batch := <-started:
			encoded, err := json.Marshal(batch)
			if err != nil || len(encoded) > protocol.MaxTimedEventBatchBytes {
				t.Fatalf("batch bytes=%d err=%v", len(encoded), err)
			}
			if len(batch.Events) > timedEventBatcherMaxEvents {
				t.Fatalf("batch event count=%d", len(batch.Events))
			}
			total += len(batch.Events)
		default:
			if total != 32 {
				t.Fatalf("flushed event count=%d", total)
			}
			return
		}
	}
}

func TestTimedEventBatcherAppliesQueueBackpressureWhileAwaitingAck(t *testing.T) {
	started := make(chan protocol.TimedEventBatch, 4)
	release := make(chan struct{})
	batcher, err := NewTimedEventBatcher("stream", func(batch protocol.TimedEventBatch) error {
		started <- batch
		<-release
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	addDone := make(chan error, 1)
	go func() {
		for i := 0; i < timedEventBatcherMaxEvents; i++ {
			if err := batcher.Add(protocol.TimedEvent{EventType: "custom.example"}, nil); err != nil {
				addDone <- err
				return
			}
		}
		addDone <- nil
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("count threshold did not start a batch")
	}
	for index := 0; index < timedEventBatcherMaxQueued; index++ {
		if err := batcher.Add(protocol.TimedEvent{EventType: "custom.example"}, nil); err != nil {
			t.Fatalf("queue item %d unexpectedly rejected: %v", index, err)
		}
	}
	if err := batcher.Add(protocol.TimedEvent{EventType: "custom.example"}, nil); !errors.Is(err, ErrTimedEventBackpressure) {
		t.Fatalf("queue overflow error=%v", err)
	}
	close(release)
	select {
	case err := <-addDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("queued batches did not drain after acknowledgement")
	}
}

func TestServeRedactsOrdinaryErrorsAndDoesNotEchoParams(t *testing.T) {
	a := newTestAdapter()
	a.resolve = func(context.Context, protocol.ResolveParams) (protocol.ResolveResult, error) {
		return protocol.ResolveResult{}, errors.New("secret=super-sensitive from request")
	}
	var out strings.Builder
	in := `{"protocol_version":1,"id":"r1","method":"resolve","params":{"secrets":{"password":"dont-echo"}}}` + "\n" + `{"protocol_version":1,"id":"s1","method":"shutdown"}` + "\n"
	if err := ServeIO(context.Background(), a, strings.NewReader(in), &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "super-sensitive") || strings.Contains(out.String(), "dont-echo") {
		t.Fatalf("sensitive value leaked in response: %s", out.String())
	}
	r := bufio.NewReader(strings.NewReader(out.String()))
	res, err := protocol.ReadResponse(r)
	if err != nil {
		t.Fatal(err)
	}
	if res.Error == nil || res.Error.Code != "internal_error" || res.Error.Message != "adapter operation failed" {
		t.Fatalf("unsafe or malformed error: %#v", res.Error)
	}
}

func TestServePreservesExplicitSafeError(t *testing.T) {
	a := newTestAdapter()
	a.resolve = func(context.Context, protocol.ResolveParams) (protocol.ResolveResult, error) {
		return protocol.ResolveResult{}, Error("auth_required", "sign in is required")
	}
	var out strings.Builder
	if err := ServeIO(context.Background(), a, strings.NewReader(`{"protocol_version":1,"id":"r1","method":"resolve"}`+"\n"), &out); err != nil {
		t.Fatal(err)
	}
	r, err := protocol.ReadResponse(bufio.NewReader(strings.NewReader(out.String())))
	if err != nil {
		t.Fatal(err)
	}
	if r.Error == nil || r.Error.Code != "auth_required" || r.Error.Message != "sign in is required" {
		t.Fatalf("structured error mismatch: %#v", r.Error)
	}
}

func TestServeHandlesUnsupportedVersionAndCleanEOF(t *testing.T) {
	a := newTestAdapter()
	var out strings.Builder
	if err := ServeIO(context.Background(), a, strings.NewReader(`{"protocol_version":7,"id":"version-7","method":"describe"}`+"\n"), &out); err != nil {
		t.Fatal(err)
	}
	var r protocol.Response
	err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &r)
	if err != nil {
		t.Fatal(err)
	}
	if r.ProtocolVersion != 7 || r.ID != "version-7" || r.Error == nil || r.Error.Code != "unsupported_protocol_version" {
		t.Fatalf("version response mismatch: %#v", r)
	}
	var empty strings.Builder
	if err := ServeIO(context.Background(), a, strings.NewReader(""), &empty); err != nil {
		t.Fatalf("clean EOF: %v", err)
	}
	if empty.Len() != 0 {
		t.Fatalf("unexpected EOF output %q", empty.String())
	}
}

func TestServeRejectsCapabilityInterfaceMismatchBeforeOutput(t *testing.T) {
	a := newTestAdapter()
	a.descriptor.Capabilities = append(a.descriptor.Capabilities, protocol.CapabilityWatch)
	var out strings.Builder
	if err := ServeIO(context.Background(), a, strings.NewReader(""), &out); err == nil {
		t.Fatal("declared but missing interface accepted")
	}
	if out.Len() != 0 {
		t.Fatalf("startup wrote output: %q", out.String())
	}
	undeclared := newTestAdapter()
	undeclared.descriptor.Capabilities = []string{protocol.CapabilityResolveWorkflow}
	if err := ServeIO(context.Background(), undeclared, strings.NewReader(""), &out); err == nil {
		t.Fatal("implemented but undeclared resolver capability accepted")
	}
	dormant := newTestAdapter()
	dormant.descriptor.Capabilities = append(dormant.descriptor.Capabilities, protocol.CapabilityStatus)
	if err := ServeIO(context.Background(), dormant, strings.NewReader(""), &out); err == nil {
		t.Fatal("dormant capability was advertised")
	}
}

func TestServePassesCancellationToHandlerAndSerializesRequests(t *testing.T) {
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	var active atomic.Int32
	var peak atomic.Int32
	a := newTestAdapter()
	a.resolve = func(ctx context.Context, _ protocol.ResolveParams) (protocol.ResolveResult, error) {
		n := active.Add(1)
		for {
			old := peak.Load()
			if n <= old || peak.CompareAndSwap(old, n) {
				break
			}
		}
		entered <- struct{}{}
		if n == 1 {
			select {
			case <-release:
			case <-ctx.Done():
			}
		}
		active.Add(-1)
		return protocol.ResolveResult{Media: protocol.MediaSource{Type: "hls", ManifestURL: "https://example.test/live.m3u8"}}, nil
	}
	in := strings.Join([]string{`{"protocol_version":1,"id":"r1","method":"resolve"}`, `{"protocol_version":1,"id":"r2","method":"resolve"}`, `{"protocol_version":1,"id":"s","method":"shutdown"}`}, "\n") + "\n"
	var out strings.Builder
	done := make(chan error, 1)
	go func() { done <- ServeIO(context.Background(), a, strings.NewReader(in), &out) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("first handler did not start")
	}
	select {
	case <-entered:
		t.Fatal("second handler overlapped first")
	case <-time.After(25 * time.Millisecond):
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := peak.Load(); got != 1 {
		t.Fatalf("peak concurrent handlers=%d", got)
	}
	if a.calls.Load() != 2 {
		t.Fatalf("calls=%d", a.calls.Load())
	}
}

func TestServeForwardsContextCancellation(t *testing.T) {
	a := newTestAdapter()
	entered := make(chan struct{})
	a.resolve = func(ctx context.Context, _ protocol.ResolveParams) (protocol.ResolveResult, error) {
		close(entered)
		<-ctx.Done()
		return protocol.ResolveResult{}, ctx.Err()
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var out strings.Builder
	done := make(chan error, 1)
	go func() {
		done <- ServeIO(ctx, a, strings.NewReader(`{"protocol_version":1,"id":"r","method":"resolve"}`+"\n"), &out)
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("handler did not start")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("ServeIO error = %v", err)
	}
}

func TestServeRejectsOverlongAndUnterminatedFrames(t *testing.T) {
	a := newTestAdapter()
	for _, input := range []string{`{"protocol_version":1,"id":"x","method":"describe"}`, fmt.Sprintf("%s%s\n", `{"protocol_version":1,"id":"x","method":"describe","padding":"`, strings.Repeat("x", protocol.MaxFrameBytes))} {
		var out strings.Builder
		if err := ServeIO(context.Background(), a, strings.NewReader(input), &out); err == nil {
			t.Fatal("invalid frame accepted")
		}
	}
}

type workflowTestAdapter struct{ descriptor protocol.Descriptor }

func (a *workflowTestAdapter) Descriptor() protocol.Descriptor { return a.descriptor }
func (*workflowTestAdapter) BeginResolution(_ context.Context, p protocol.ResolveBeginParams) (protocol.ResolveWorkflowResult, error) {
	mutations := make([]protocol.StateMutation, 65)
	return protocol.ResolveWorkflowResult{
		State: "resolved", WorkflowID: p.WorkflowID,
		Media:          &protocol.MediaSource{Type: "hls", ManifestURL: "https://example.test/live.m3u8"},
		StateMutations: mutations,
	}, nil
}
func (*workflowTestAdapter) ContinueResolution(_ context.Context, p protocol.ResolveContinueParams) (protocol.ResolveWorkflowResult, error) {
	return protocol.ResolveWorkflowResult{State: "resolved", WorkflowID: p.WorkflowID, Media: &protocol.MediaSource{Type: "hls", ManifestURL: "https://example.test/live.m3u8"}}, nil
}

func TestServeAcceptsWorkflowStateMutationsAboveWatchMetadataLimit(t *testing.T) {
	a := &workflowTestAdapter{descriptor: protocol.Descriptor{
		ID: "workflow-test", Name: "Workflow test", Version: "1", ProtocolVersion: protocol.Version,
		Capabilities: []string{protocol.CapabilityResolveWorkflow},
		InputSchema:  protocol.Schema{Fields: []protocol.Field{}}, ConfigurationSchema: protocol.Schema{Fields: []protocol.Field{}},
		MediaTypes: []string{"hls"},
	}}
	input := "{\"protocol_version\":1,\"id\":\"wf-1\",\"method\":\"resolve.begin\",\"params\":{\"workflow_id\":\"workflow-1\",\"input\":{}}}\n" +
		"{\"protocol_version\":1,\"id\":\"shutdown-1\",\"method\":\"shutdown\"}\n"
	var output strings.Builder
	if err := ServeIO(context.Background(), a, strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(strings.NewReader(output.String()))
	response, err := protocol.ReadResponse(reader)
	if err != nil {
		t.Fatal(err)
	}
	if response.Error != nil {
		t.Fatalf("workflow with 65 state mutations rejected: %#v", response.Error)
	}
	var result protocol.ResolveWorkflowResult
	if err := json.Unmarshal(response.Result, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.StateMutations) != 65 {
		t.Fatalf("state mutation count = %d", len(result.StateMutations))
	}
}

type workflowResultTestAdapter struct {
	descriptor protocol.Descriptor
	result     protocol.ResolveWorkflowResult
}

func (a *workflowResultTestAdapter) Descriptor() protocol.Descriptor { return a.descriptor }
func (a *workflowResultTestAdapter) BeginResolution(_ context.Context, p protocol.ResolveBeginParams) (protocol.ResolveWorkflowResult, error) {
	result := a.result
	result.WorkflowID = p.WorkflowID
	return result, nil
}
func (a *workflowResultTestAdapter) ContinueResolution(_ context.Context, p protocol.ResolveContinueParams) (protocol.ResolveWorkflowResult, error) {
	result := a.result
	result.WorkflowID = p.WorkflowID
	return result, nil
}

func TestServeAcceptsCoreWorkflowResultStates(t *testing.T) {
	resource := &protocol.ResourceRef{Type: "channel", ID: "channel-1"}
	media := &protocol.MediaSource{Type: "hls", ManifestURL: "https://example.test/live.m3u8"}
	prompt := &protocol.InteractionMessage{Type: "prompt", InteractionID: "choose-channel"}
	tests := []struct {
		name   string
		result protocol.ResolveWorkflowResult
	}{
		{name: "resource_discovered", result: protocol.ResolveWorkflowResult{State: "resource_discovered", Resource: resource}},
		{name: "configuration_required", result: protocol.ResolveWorkflowResult{State: "configuration_required", Challenge: &protocol.WorkflowChallenge{Schema: protocol.Schema{Fields: []protocol.Field{}}}}},
		{name: "interaction_required", result: protocol.ResolveWorkflowResult{State: "interaction_required", Challenge: &protocol.WorkflowChallenge{Schema: protocol.Schema{Fields: []protocol.Field{}}, Prompt: prompt}}},
		{name: "resolved", result: protocol.ResolveWorkflowResult{State: "resolved", Media: media}},
		{name: "error", result: protocol.ResolveWorkflowResult{State: "error"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := serveWorkflowResult(t, test.result)
			if response.Error != nil {
				t.Fatalf("Core workflow state rejected: %#v", response.Error)
			}
			var result protocol.ResolveWorkflowResult
			if err := json.Unmarshal(response.Result, &result); err != nil {
				t.Fatal(err)
			}
			if result.State != test.name {
				t.Fatalf("workflow state = %q, want %q", result.State, test.name)
			}
		})
	}
}

func TestServeRejectsInvalidWorkflowResultStatesAndShapes(t *testing.T) {
	media := &protocol.MediaSource{Type: "hls", ManifestURL: "https://example.test/live.m3u8"}
	challenge := &protocol.WorkflowChallenge{Schema: protocol.Schema{Fields: []protocol.Field{}}}
	tests := []struct {
		name   string
		result protocol.ResolveWorkflowResult
	}{
		{name: "old challenge state", result: protocol.ResolveWorkflowResult{State: "challenge", Challenge: challenge}},
		{name: "unknown state", result: protocol.ResolveWorkflowResult{State: "future_state"}},
		{name: "resource discovery without resource", result: protocol.ResolveWorkflowResult{State: "resource_discovered"}},
		{name: "resource discovery with media", result: protocol.ResolveWorkflowResult{State: "resource_discovered", Resource: &protocol.ResourceRef{Type: "channel", ID: "channel-1"}, Media: media}},
		{name: "configuration required without challenge", result: protocol.ResolveWorkflowResult{State: "configuration_required"}},
		{name: "interaction required with media", result: protocol.ResolveWorkflowResult{State: "interaction_required", Challenge: challenge, Media: media}},
		{name: "challenge schema invalid", result: protocol.ResolveWorkflowResult{State: "configuration_required", Challenge: &protocol.WorkflowChallenge{Schema: protocol.Schema{Fields: []protocol.Field{{Key: "bad", Control: "not-a-control", Label: "Bad"}}}}}},
		{name: "challenge prompt invalid", result: protocol.ResolveWorkflowResult{State: "interaction_required", Challenge: &protocol.WorkflowChallenge{Schema: protocol.Schema{Fields: []protocol.Field{}}, Prompt: &protocol.InteractionMessage{Type: "unknown", InteractionID: "prompt-1"}}}},
		{name: "resolved without media", result: protocol.ResolveWorkflowResult{State: "resolved"}},
		{name: "resolved with challenge", result: protocol.ResolveWorkflowResult{State: "resolved", Media: media, Challenge: challenge}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := serveWorkflowResult(t, test.result)
			if response.Error == nil || response.Error.Code != "invalid_result" {
				t.Fatalf("invalid workflow result response = %#v", response)
			}
		})
	}
}

func serveWorkflowResult(t *testing.T, result protocol.ResolveWorkflowResult) protocol.Response {
	t.Helper()
	a := &workflowResultTestAdapter{
		descriptor: protocol.Descriptor{
			ID: "workflow-shape-test", Name: "Workflow shape test", Version: "1", ProtocolVersion: protocol.Version,
			Capabilities: []string{protocol.CapabilityResolveWorkflow},
			InputSchema:  protocol.Schema{Fields: []protocol.Field{}}, ConfigurationSchema: protocol.Schema{Fields: []protocol.Field{}},
			MediaTypes: []string{"hls"},
		},
		result: result,
	}
	input := "{\"protocol_version\":1,\"id\":\"wf-1\",\"method\":\"resolve.begin\",\"params\":{\"workflow_id\":\"workflow-1\",\"input\":{}}}\n" +
		"{\"protocol_version\":1,\"id\":\"shutdown-1\",\"method\":\"shutdown\"}\n"
	var output strings.Builder
	if err := ServeIO(context.Background(), a, strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(strings.NewReader(output.String()))
	response, err := protocol.ReadResponse(reader)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func TestServeValidatesHistoricalMediaAndURLTransforms(t *testing.T) {
	cases := []struct {
		name    string
		media   protocol.MediaSource
		wantErr bool
	}{
		{
			name: "valid combined declaration",
			media: protocol.MediaSource{
				Type: "hls", ManifestURL: "https://media.example.test/live.m3u8",
				HistoricalAvailability: &protocol.HistoricalAvailability{
					Mode:           protocol.HistoricalModeSequenceRanges,
					SequenceRanges: []protocol.HistoricalSequenceRange{{Start: 1, End: 10}},
				},
				RequestPolicy: &protocol.RequestPolicy{URLTransform: &protocol.URLTransformPolicy{
					Rules: []protocol.URLTransformRule{{Scopes: []protocol.ResourceRequestScope{protocol.RequestScopeMedia}, QueryParameters: []protocol.QueryParameterPropagation{{From: "signature", To: "token"}}}},
				}},
			},
		},
		{
			name: "invalid historical declaration",
			media: protocol.MediaSource{
				Type: "hls", ManifestURL: "https://media.example.test/live.m3u8",
				HistoricalAvailability: &protocol.HistoricalAvailability{Mode: protocol.HistoricalModeRollingWindow},
			},
			wantErr: true,
		},
		{
			name: "invalid URL transform",
			media: protocol.MediaSource{
				Type: "hls", ManifestURL: "https://media.example.test/live.m3u8",
				RequestPolicy: &protocol.RequestPolicy{URLTransform: &protocol.URLTransformPolicy{
					Rules: []protocol.URLTransformRule{{Scopes: []protocol.ResourceRequestScope{"unknown"}, PathSuffix: &protocol.PathSuffixRewrite{From: ".old", To: ".new"}}},
				}},
			},
			wantErr: true,
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			a := newTestAdapter()
			a.resolve = func(context.Context, protocol.ResolveParams) (protocol.ResolveResult, error) {
				return protocol.ResolveResult{Media: test.media}, nil
			}
			input := `{"protocol_version":1,"id":"resolve","method":"resolve"}` + "\n" + `{"protocol_version":1,"id":"shutdown","method":"shutdown"}` + "\n"
			var output strings.Builder
			if err := ServeIO(context.Background(), a, strings.NewReader(input), &output); err != nil {
				t.Fatal(err)
			}
			reader := bufio.NewReader(strings.NewReader(output.String()))
			response, err := protocol.ReadResponse(reader)
			if err != nil {
				t.Fatal(err)
			}
			if test.wantErr {
				if response.Error == nil || response.Error.Code != "invalid_result" {
					t.Fatalf("invalid media response = %#v", response)
				}
				return
			}
			if response.Error != nil {
				t.Fatalf("valid media rejected: %#v", response.Error)
			}
			var result protocol.ResolveResult
			if err := json.Unmarshal(response.Result, &result); err != nil {
				t.Fatal(err)
			}
			if result.Media.HistoricalAvailability == nil || result.Media.RequestPolicy == nil || result.Media.RequestPolicy.URLTransform == nil {
				t.Fatalf("new declarations missing in output: %s", response.Result)
			}
		})
	}
}
