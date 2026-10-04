# Integrated Recorder Adapter SDK for Go

Build a standalone Adapter Protocol v1 executable without importing Integrated Recorder or writing JSON framing code.

## Quick start

```sh
mkdir my-recorder-adapter
cd my-recorder-adapter
go mod init example.com/my-recorder-adapter
go get github.com/integrated-recorder/adapter-sdk-go@latest
```

Create `main.go` and implement `Descriptor` plus the methods for the capabilities you declare:

```go
package main

import (
    "context"
    "fmt"
    "os"

    "github.com/integrated-recorder/adapter-sdk-go/adapter"
    "github.com/integrated-recorder/adapter-sdk-go/protocol"
)

type myAdapter struct{}

func (*myAdapter) Descriptor() protocol.Descriptor {
    return protocol.Descriptor{
        ID: "my-adapter", Name: "My Adapter", Version: "0.1.0",
        ProtocolVersion: protocol.Version,
        Capabilities: []string{protocol.CapabilityResolve},
        InputSchema: protocol.Schema{Fields: []protocol.Field{}},
        ConfigurationSchema: protocol.Schema{Fields: []protocol.Field{}},
        MediaTypes: []string{"hls"},
    }
}

func (*myAdapter) Resolve(_ context.Context, p protocol.ResolveParams) (protocol.ResolveResult, error) {
    // Interpret p.Input and return a validated HTTP(S) media source.
    _ = p
    return protocol.ResolveResult{}, adapter.Error("not_configured", "configure a source first")
}

func main() {
    if err := adapter.Serve(&myAdapter{}); err != nil {
        _, _ = fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
}
```

Build and validate the actual executable with the Core-owned black-box runner:

```sh
go build -o integrated-recorder-adapter-foo .
go run github.com/integrated-recorder/core/cmd/adapter-conformance@a245a06020f3017ab329c72a86c0e79133472643 --binary ./integrated-recorder-adapter-foo
```

After it passes, install the binary in the Integrated Recorder `adapter-binaries` directory and restart Integrated Recorder so it discovers the executable:

```sh
install -m 0755 integrated-recorder-adapter-foo /path/to/adapter-binaries/integrated-recorder-adapter-foo
# Restart the Integrated Recorder service using its normal service manager.
```

The binary name must start with `integrated-recorder-adapter-`. The conformance runner checks the executable over its real stdin/stdout protocol; it does not import this SDK.

## Capabilities and handlers

`adapter.Adapter` is the base contract: it returns a stable `protocol.Descriptor`. Implement optional interfaces for each declared active capability:

- `Resolver` for `resolve`
- `WorkflowResolver` for `resolve_workflow`
- `Watcher` for `watch`
- `MetadataProvider` for `metadata`
- `Refresher` for `refresh`
- `ResourceBrowser` for `resource_browse`

The SDK checks both directions before serving: an active capability must have its interface, and an implemented interface must be declared. Protocol v1 also names `status`, `configure`, `interaction`, and `events`, but the current Core runtime does not dispatch those method families; this SDK rejects declaring them. Unknown valid capability identifiers can be preserved in descriptors, but no handler is inferred for them.

The SDK handles `describe` and `shutdown` automatically. Requests execute sequentially. `ServeContext` passes its caller context to handlers so applications can connect it to signal or lifecycle cancellation; `Serve` uses `context.Background()`. Ordinary Go errors are replaced with a generic safe `internal_error`. Use `adapter.Error(code, message)` only for a deliberately safe, bounded wire message.

Protocol contract and all currently published method, schema, resource, workflow, metadata, refresh, and watch semantics are in [`docs/PROTOCOL_V1.md`](docs/PROTOCOL_V1.md). Machine-readable vectors are in [`protocol/adapter-v1/`](protocol/adapter-v1/). The SDK package's `adaptertest` helpers support local, in-process tests; Core's executable conformance runner remains the compatibility authority.

## Standalone example

The repository includes a deterministic executable:

```sh
go build -o integrated-recorder-adapter-example ./examples/minimal
```

It supports `resolve`, `watch`, and `metadata` with local fixture behavior and no platform account or network service. It imports only the standard library and this SDK.

## Security model

An adapter is a trusted local executable, not a sandbox. It runs under the Core process's operating-system user and may receive secrets required by the configuration it declares. Keep secrets out of logs and structured errors. Adapter stdout is reserved exclusively for protocol frames; diagnostics belong on stderr. The Core independently validates returned media URLs, resource references, state mutations, and other results.

## Development checks

```sh
GOWORK=off go test -race -count=1 ./...
GOWORK=off go vet ./...
GOWORK=off go build ./...
```
