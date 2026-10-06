# Integrated Recorder Adapter SDK for Go

[한국어](README.md) | **English**

The official Go SDK for building Integrated Recorder Source Plugins. It helps create standalone Adapter Protocol v1 executables without implementing framing or dispatch from scratch.

## Quick start

```sh
mkdir my-recorder-adapter
cd my-recorder-adapter
go mod init example.com/my-recorder-adapter
go get github.com/integrated-recorder/adapter-sdk-go@v0.2.1
```

Implement `Descriptor` and handlers for the capabilities you declare in `main.go`.

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

Build the executable and validate it with the black-box conformance runner from the exact Core commit you target.

```sh
go build -o integrated-recorder-adapter-foo .
git clone https://github.com/integrated-recorder/core.git
cd core
CORE_REF="<the exact Core release tag or commit being targeted>"
git checkout "$CORE_REF"
go run ./cmd/adapter-conformance --binary /absolute/path/to/integrated-recorder-adapter-foo
```

The SDK provides an authoring API and test helpers. Core's `adapter-conformance` runner is the final executable compatibility authority. It starts the process and checks the stdin/stdout protocol; it does not import the SDK package.

## Capabilities and handlers

`adapter.Adapter` is the base contract and returns a stable `protocol.Descriptor`. Implement the optional interface for each active capability:

- `Resolver` — `resolve`
- `WorkflowResolver` — `resolve_workflow`
- `Watcher` — `watch`
- `MetadataProvider` — `metadata`
- `Refresher` — `refresh`
- `ResourceBrowser` — `resource_browse`

The SDK checks that declarations and implementations match in both directions. Protocol v1 also names `status`, `configure`, `interaction`, and `events`, but the current Core runtime does not dispatch these method families and the SDK rejects declaring them. Unknown but valid capability identifiers can be preserved in descriptors.

The SDK handles `describe` and `shutdown`, and runs requests sequentially. `ServeContext` passes its caller context to handlers. Ordinary Go errors are replaced by a safe `internal_error`; use `adapter.Error(code, message)` only for deliberate, bounded wire messages.

## Example and protocol docs

The deterministic executable in `examples/minimal` runs without an external account or network service.

```sh
go build -o integrated-recorder-adapter-example ./examples/minimal
```

Method, schema, resource, workflow, metadata, refresh, and watch semantics are documented in [Protocol v1](docs/PROTOCOL_V1.md). Machine-readable vectors are in [protocol/adapter-v1](protocol/adapter-v1/), and `adaptertest` provides in-process helpers.

## Security boundary

An adapter is trusted local native code, not a sandbox. It runs as the Core operating-system user and may receive secrets required by its declared configuration. Stdout is reserved for protocol frames; send diagnostics to stderr. Keep secrets out of logs and structured errors. Core independently validates returned media URLs, resource references, and state mutations.

## Development checks

```sh
GOWORK=off go test -race -count=1 ./...
GOWORK=off go vet ./...
GOWORK=off go build ./...
```

## Ecosystem role and license

This repository provides only the Go Source Plugin authoring SDK. It does not own the Runtime, recording, storage, Registry approval, or plugin safety. [Core](https://github.com/integrated-recorder/core) owns execution and canonical archives; the [Plugin Registry](https://github.com/integrated-recorder/plugin-registry) indexes approved artifact metadata. See [source.owncast](https://github.com/integrated-recorder/source.owncast) as a plugin example.

See [LICENSE](LICENSE) for the applicable terms.
