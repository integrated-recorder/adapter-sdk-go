// Package adaptertest provides in-process helpers for SDK adapter tests. It
// does not replace the Core-owned black-box adapter-conformance executable.
package adaptertest

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/integrated-recorder/adapter-sdk-go/adapter"
	"github.com/integrated-recorder/adapter-sdk-go/protocol"
)

func ValidateDescriptor(d protocol.Descriptor) error { return d.Validate() }
func ValidateAdapter(a adapter.Adapter) error        { return adapter.Validate(a) }

// ServeRoundTrip runs a real SDK ServeIO loop over in-memory streams. It
// appends shutdown if necessary and parses every output as a response frame.
func ServeRoundTrip(ctx context.Context, a adapter.Adapter, requests ...protocol.Request) ([]protocol.Response, error) {
	var input bytes.Buffer
	shutdownSeen := false
	for _, req := range requests {
		if req.Method == protocol.MethodShutdown {
			shutdownSeen = true
		}
		if err := protocol.WriteRequest(&input, req); err != nil {
			return nil, err
		}
	}
	if !shutdownSeen {
		if err := protocol.WriteRequest(&input, protocol.Request{ProtocolVersion: protocol.Version, ID: "adaptertest-shutdown", Method: protocol.MethodShutdown}); err != nil {
			return nil, err
		}
	}
	var output bytes.Buffer
	if err := adapter.ServeIO(ctx, a, &input, &output); err != nil {
		return nil, err
	}
	reader := bufio.NewReader(&output)
	responses := make([]protocol.Response, 0, len(requests)+1)
	for {
		response, err := protocol.ReadResponse(reader)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		responses = append(responses, response)
	}
	if len(responses) == 0 {
		return nil, fmt.Errorf("SDK emitted no responses")
	}
	return responses, nil
}
