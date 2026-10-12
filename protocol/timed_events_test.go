package protocol

import (
	"encoding/json"
	"testing"
)

func TestTimedEventValidationAcceptsCustomAndRejectsSecrets(t *testing.T) {
	valid := TimedEventBatch{StreamID: "stream", BatchID: "batch-1", Events: []TimedEvent{{EventType: "custom.example", Extension: json.RawMessage(`{"platform_field":"preserved"}`)}}}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid event rejected: %v", err)
	}
	secret := TimedEventBatch{StreamID: "stream", BatchID: "batch-2", Events: []TimedEvent{{EventType: "chat.message", Payload: json.RawMessage(`{"authorization":"Bearer secret"}`)}}}
	if err := secret.Validate(); err == nil {
		t.Fatal("credential-bearing event accepted")
	}
	signedURL := TimedEventBatch{StreamID: "stream", BatchID: "batch-3", Events: []TimedEvent{{EventType: "chat.message", Extension: json.RawMessage(`{"url":"https://example.test/live?X-Amz-Signature=secret"}`)}}}
	if err := signedURL.Validate(); err == nil {
		t.Fatal("signed URL was accepted")
	}
	keyURL := TimedEventBatch{StreamID: "stream", BatchID: "batch-4", Events: []TimedEvent{{EventType: "chat.message", Payload: json.RawMessage(`{"url":"https://example.test/live?key=secret"}`)}}}
	if err := keyURL.Validate(); err == nil {
		t.Fatal("credential URL was accepted")
	}
}
