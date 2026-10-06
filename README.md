# Integrated Recorder Adapter SDK for Go

**한국어** | [English](README.en.md)

Integrated Recorder의 Source Plugin을 Go로 작성하기 위한 공식 SDK입니다. Adapter Protocol v1 executable의 framing이나 dispatch를 직접 구현하지 않고 standalone adapter를 만들 수 있습니다.

## 시작하기

```sh
mkdir my-recorder-adapter
cd my-recorder-adapter
go mod init example.com/my-recorder-adapter
go get github.com/integrated-recorder/adapter-sdk-go@v0.2.1
```

`main.go`에서 `Descriptor`와 선언한 capability의 handler를 구현합니다.

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

실행 파일을 빌드한 뒤, 대상 Core commit에서 제공하는 black-box conformance runner로 검증하세요.

```sh
go build -o integrated-recorder-adapter-foo .
git clone https://github.com/integrated-recorder/core.git
cd core
CORE_REF="<the exact Core release tag or commit being targeted>"
git checkout "$CORE_REF"
go run ./cmd/adapter-conformance --binary /absolute/path/to/integrated-recorder-adapter-foo
```

SDK는 authoring API와 테스트 편의를 제공하고, Core의 `adapter-conformance`가 실제 executable 호환성의 최종 기준입니다. Runner는 프로세스를 실행해 stdin/stdout protocol framing을 확인하며 SDK package를 import하지 않습니다.

## Capability와 handler

`adapter.Adapter`는 안정적인 `protocol.Descriptor`를 반환하는 기본 계약입니다. 선언한 capability에 대응하는 선택적 interface를 구현합니다.

- `Resolver` — `resolve`
- `WorkflowResolver` — `resolve_workflow`
- `Watcher` — `watch`
- `MetadataProvider` — `metadata`
- `Refresher` — `refresh`
- `ResourceBrowser` — `resource_browse`

SDK는 선언과 구현이 양방향으로 일치하는지 검사합니다. Protocol v1에는 `status`, `configure`, `interaction`, `events` 이름도 있지만 현재 Core runtime은 이를 dispatch하지 않으며 SDK도 선언을 거부합니다. 알 수 있지만 형식에 맞는 capability 식별자는 descriptor에서 보존될 수 있습니다.

SDK가 `describe`와 `shutdown`을 처리하고 요청을 순차 실행합니다. `ServeContext`는 호출자의 context를 handler에 전달합니다. 일반 Go 오류는 안전한 `internal_error`로 치환됩니다. 의도적으로 안전하고 bounded한 wire message에만 `adapter.Error(code, message)`를 사용하세요.

## 예제와 Protocol 문서

`examples/minimal`에는 외부 계정이나 네트워크 없이 동작하는 결정적 executable 예제가 있습니다.

```sh
go build -o integrated-recorder-adapter-example ./examples/minimal
```

Protocol의 method, schema, resource, workflow, metadata, refresh, watch 의미는 [Protocol v1 문서](docs/PROTOCOL_V1.md)에 있습니다. Machine-readable vector는 [protocol/adapter-v1](protocol/adapter-v1/)에, in-process helper는 `adaptertest`에 있습니다.

## 보안 경계

Adapter는 신뢰된 로컬 native executable이며 sandbox가 아닙니다. Core OS 사용자 권한으로 실행되며 선언된 설정에 필요한 secret을 받을 수 있습니다. stdout은 protocol frame 전용이고 진단은 stderr로 보내세요. secret을 로그나 structured error에 넣지 마세요. Core는 adapter 결과의 media URL, resource 참조, state mutation을 별도로 검증합니다.

## 개발 검증

```sh
GOWORK=off go test -race -count=1 ./...
GOWORK=off go vet ./...
GOWORK=off go build ./...
```

## 생태계 위치와 라이선스

이 저장소는 Go Source Plugin authoring SDK만 제공합니다. Runtime, recording, storage, Plugin Registry 승인, plugin 안전성을 소유하지 않습니다. [Core](https://github.com/integrated-recorder/core)는 실행과 canonical archive를, [Plugin Registry](https://github.com/integrated-recorder/plugin-registry)는 승인된 artifact metadata를 담당합니다. 배포 예제는 [source.owncast](https://github.com/integrated-recorder/source.owncast)를 참고하세요.

[LICENSE](LICENSE)에 따른 라이선스를 적용합니다.
