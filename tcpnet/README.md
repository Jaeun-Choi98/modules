# tcpnet

> `github.com/Jaeun-Choi98/modules/tcpnet`

스트림 통신 환경에서 웹 프레임워크와 유사한 개발 경험을 제공하기 위한 Go 모듈입니다.  
애플리케이션 3계층 아키텍처(표현·서비스·DB)에 통합하여 사용할 수 있으며,  
스트림 특성상 발생하는 단편화(fragmentation), 병합(coalescing) 등의 변수에 일관성 있게 대응합니다.

---

## 패키지 구성

```
tcpnet/
├── basic/
│   ├── server/      # TCP 서버 소켓 관리, 연결 수락·추적
│   └── client/      # TCP 클라이언트 소켓 관리 및 연결
└── advanced/
    ├── parser/      # 프로토콜별 바이트 스트림 파싱 인터페이스
    ├── model/       # 연결 컨텍스트 (파싱 결과, 요청-응답 대기 등)
    ├── handler/     # 패킷 식별 기반 처리 로직 분배
    └── serializer/  # 바이너리 패킷 조립 (빌더 패턴)
```

### Basic 패키지

TCP 통신의 기본 역할(서버/클라이언트)을 담당합니다.

| 패키지 | 역할 |
|--------|------|
| `basic/server` | 리슨 소켓 생성, Accept 루프, 연결별 고루틴 시작·추적·종료, 패닉 복구 |
| `basic/client` | 서버 연결, 재연결 정책, 메시지 송신 |

### Advanced 패키지

스트림 환경에서 일관된 처리 구조를 제공하기 위한 패키지들로, 아래 의존 방향을 따릅니다.

```
[Raw Bytes Stream]
       │
       ▼
   parser.Parser          ← 프로토콜 정의에 따라 바이트를 파싱
       │
       ▼
   model.ConnContext      ← 파싱된 메시지 + 요청-응답 대기(PendingRequests)를 연결 단위로 보관
       │
       ▼
  handler.Handler         ← 패킷 종류에 따라 처리 함수로 분배 (라우팅)
       │
       ▼
  [비즈니스 로직 / 서비스 계층]
```

| 패키지 | 역할 |
|--------|------|
| `advanced/parser` | `Parser` 인터페이스 제공. 프로토콜마다 구현체를 작성해 스트림을 파싱 |
| `advanced/model` | 연결 컨텍스트. 파싱된 메시지 큐와 요청-응답 대기(`PendingRequests`)를 보관 |
| `advanced/handler` | 패킷 식별자(커맨드·타입 등)를 키로 처리 함수를 등록하고 분배 |
| `advanced/serializer` | `BinaryWriter`로 바이너리 패킷을 체이닝 방식으로 조립 |

---

## 빠른 시작

### 설치

```bash
go get github.com/Jaeun-Choi98/modules/tcpnet
```

### 서버 예제

아래 예제는 2바이트 커맨드 + 4바이트 페이로드 길이 + N바이트 페이로드로 구성된  
간단한 커스텀 프로토콜을 처리하는 에코 서버입니다.

```go
import (
	"context"
	"encoding/binary"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"time"

	"github.com/Jaeun-Choi98/modules/tcpnet/advanced/handler"
	tcpmd "github.com/Jaeun-Choi98/modules/tcpnet/advanced/model"
	"github.com/Jaeun-Choi98/modules/tcpnet/advanced/serializer"
	"github.com/Jaeun-Choi98/modules/tcpnet/basic/server"
)

const CmdEcho uint16 = 0x0001

// [cmd 2B][len 4B][payload NB] 프로토콜 파서.
// io.ReadFull로 정해진 길이만큼 읽으므로 단편화·병합된 스트림도 패킷 단위로 잘림.
type echoParser struct{}

func (echoParser) Parse(conn net.Conn) (tcpmd.ParseMsg, error) {
	header := make([]byte, 6)
	if _, err := io.ReadFull(conn, header); err != nil {
		return nil, err
	}
	cmd := binary.BigEndian.Uint16(header[0:2])
	payload := make([]byte, binary.BigEndian.Uint32(header[2:6]))
	if _, err := io.ReadFull(conn, payload); err != nil {
		return nil, err
	}
	return (&tcpmd.GenericParseMsg[uint16, []byte]{}).AddPacket(cmd, payload), nil
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	srv, _ := server.NewServerBase(ctx, 5*time.Second, 3)
	srv.SetIpAndPort("0.0.0.0", "8080")
	srv.SetPanicHandler(func(conn net.Conn, r any, stack []byte) {
		log.Printf("panic from %s: %v\n%s", conn.RemoteAddr(), r, stack)
	})
	srv.SetHandleConnectFunc(func(conn net.Conn) {
		defer conn.Close()

		connCtx := tcpmd.NewConnContext(ctx, 16)
		defer connCtx.Close()

		// HandleContext에는 conn이 없으므로 응답이 필요한 핸들러는 연결 단위로 등록
		manager := handler.New[uint16]()
		manager.RegisterHandle(CmdEcho, func(c *tcpmd.HandleContext) error {
			payload := c.GetParseMsg().GetPacket().([]byte)
			resp := serializer.NewBinaryWriter(binary.BigEndian).
				WriteUint16(CmdEcho).
				WriteUint32(uint32(len(payload))).
				WriteBytes(payload).
				Bytes()
			_, err := conn.Write(resp)
			return err
		})

		var parser echoParser
		for {
			msg, err := parser.Parse(conn)
			if err != nil {
				return // EOF, 또는 Shutdown이 conn을 닫은 경우
			}
			if err := connCtx.SetParsedMsg(msg); err != nil {
				return
			}
			if err := manager.HandleMessageSync(connCtx); err != nil {
				log.Printf("handle: %v", err)
			}
		}
	})

	go func() {
		if err := srv.Start(); err != nil { // Shutdown 전까지 블로킹
			log.Println(err)
		}
	}()
	srv.StartTCPServerHeartbeat()

	<-ctx.Done()
	srv.Shutdown() // 리스너·활성 연결을 닫고 모든 핸들러가 반환될 때까지 대기
}
```

---

## 서버 생명주기

### 연결 추적과 종료 순서

`ServerBase`는 `handleConnectFunc`가 실행 중인 연결을 추적합니다(`ActiveConnCount()`).  
`Shutdown()`은 아래 순서로 정리하며, **모든 연결 핸들러가 반환된 뒤에** 반환됩니다.

1. ctx 취소 → 이후 새 연결 등록, 고루틴 추가, 리스너 재생성 차단
2. 리스너 닫기 → Accept 루프 종료
3. 추적 중인 conn 모두 닫기 → 핸들러의 블로킹된 `Read` 해제
4. Accept 루프, heartbeat, 모든 연결 핸들러가 끝날 때까지 대기

`Shutdown()` 이후 `Start()`·`Listening()`은 에러를 반환하고, `StartTCPServerHeartbeat()`는 아무 동작도 하지 않습니다.

> 핸들러가 conn이 아닌 다른 곳(외부 채널, 멈춘 DB 호출 등)에서 블로킹되어 있으면 `Shutdown()`도 그만큼 기다립니다.

### 패닉 복구

연결 핸들러는 별도 고루틴에서 실행되므로 `Start()`를 호출한 쪽의 `recover`로는 잡히지 않습니다.  
서버가 연결 고루틴마다 패닉을 복구하고, 해당 conn을 닫은 뒤 다른 연결은 계속 처리합니다.

```go
srv.SetPanicHandler(func(conn net.Conn, r any, stack []byte) {
    log.Printf("panic from %s: %v\n%s", conn.RemoteAddr(), r, stack)
})
```

핸들러를 등록하지 않으면 패닉 값과 스택을 `log`로 남깁니다. 패닉 핸들러 자체의 패닉도 복구됩니다.

---

## 요청-응답 대기 (`PendingRequests`)

장비에 요청 패킷을 보내고 그 응답 패킷이 오면 로직을 처리하는 흐름을 위한 구조입니다.  
key는 시퀀스 번호·커맨드 ID 등 요청과 응답을 짝지을 수 있는 값을 사용합니다.

```go
pending := connCtx.GetPending()

// 요청 쪽
ch := make(chan tcpmd.Response, 1) // 버퍼 1 필수: Resolve는 블로킹하지 않음
pending.Register(seq, ch)
defer pending.Remove(seq)

client.SendMessage(req, time.Second)

select {
case resp, ok := <-ch:
    if !ok {
        return // 연결 컨텍스트가 닫힘 (CancelAll)
    }
    result := logic(resp.GetPayload())
    _ = result // 필요하면 결과를 다른 곳으로 전송
case <-time.After(500 * time.Millisecond):
    // 타임아웃
}

// 응답 패킷을 처리하는 핸들러 쪽
manager.RegisterHandle(CmdStatusResp, func(c *tcpmd.HandleContext) error {
    resp := c.GetParseMsg().GetPacket().(StatusResp)
    return c.GetPending().Resolve(resp.Seq, &tcpmd.GenericResponse[StatusResp]{Payload: resp})
})
```

| 메서드 | 역할 |
|--------|------|
| `Register(key, ch)` | 응답 대기 등록 |
| `Resolve(key, resp)` | 대기 중인 요청에 응답 전달. 블로킹하지 않으며 `Cancel`과 동시에 호출해도 안전 |
| `Get(key)` | 등록된 채널 조회 |
| `Remove(key)` | 채널을 닫지 않고 등록 해제 (요청 쪽 정리용) |
| `Cancel(key)` / `CancelAll()` | 채널을 닫아 대기 중인 요청을 취소. `ConnContext.Close()` 시 `CancelAll` 호출 |

| 에러 | 의미 |
|------|------|
| `ErrNoPendingRequest` | 해당 key로 기다리는 요청이 없음 (이미 타임아웃으로 떠남) |
| `ErrResponseBusy` | 채널 버퍼가 가득 참 (이미 응답이 전달됨) |
| `ErrContextClosed` | 닫힌 `ConnContext`에 `SetParsedMsg` 호출 |

---

## 핸들러 동작

- 등록 시 packetId 타입이 `Manager[T]`의 `T`와 다르면 패닉합니다 (프로그래머 오류).
- 처리 시 메시지의 packetId 타입이 다르면 패닉 대신 에러를 반환합니다.
- `HandleMessage`는 별도 고루틴에서 처리하며 패닉을 복구해 로그로 남깁니다.
- `HandleMessageSync`는 핸들러 패닉을 에러로 변환해 반환합니다. 핸들러가 반환한 에러는 로그로 남깁니다.

---

## 아키텍처 통합 예시

```
┌─────────────────────────────────────────────┐
│              TCP Client (현장 장비)           │
└────────────────────┬────────────────────────┘
                     │ Raw Bytes (Stream)
┌────────────────────▼────────────────────────┐
│            basic/server                      │  ← 연결 수락 / 고루틴 관리
│  ┌──────────────────────────────────────┐   │
│  │         advanced/parser              │   │  ← 표현 계층
│  │  바이트 스트림 → model.ConnContext    │   │
│  └──────────────────┬───────────────────┘   │
│  ┌──────────────────▼───────────────────┐   │
│  │         advanced/handler             │   │  ← 표현 계층
│  │  Context.cmd → 처리 함수 분배         │   │
│  └──────────────────┬───────────────────┘   │
└────────────────────┬────────────────────────┘
                     │
┌────────────────────▼────────────────────────┐
│            Service Layer                     │  ← 서비스 계층
│  (비즈니스 로직, 집계, 이벤트 처리 등)        │
└────────────────────┬────────────────────────┘
                     │
┌────────────────────▼────────────────────────┐
│            DB Layer (MariaDB 등)             │  ← DB 계층
└─────────────────────────────────────────────┘
```

---

## 라이선스

MIT
