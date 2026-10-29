# modules

재사용 가능한 Go 모듈 모음입니다. 각 패키지는 독립적인 `go.mod`를 가진 별도 모듈입니다.

---

## 패키지 목록

| 패키지 | 설명 |
| --- | --- |
| [eventbus](#eventbus) | 토픽 기반 Pub/Sub 이벤트 버스 |
| [sse](#sse) | Server-Sent Events 세션 관리 |
| [tcpnet](#tcpnet) | TCP 서버/클라이언트 + 패킷 핸들러 |
| [udpnet](#udpnet) | 비연결형 UDP Endpoint + 핑퐁 생존 확인 + 패킷 핸들러 |
| [serialport](#serialport) | 시리얼 포트 통신 + 패킷 핸들러 |
| [orm/redisorm](#redisorm) | Redis 기반 제네릭 Repository ORM |
| [utils](#utils) | 범용 유틸리티 함수 모음 |
| [shell](#shell) | 쉘 스크립트 실행기 |
| [logger](#logger) | 날짜별 디렉터리 로그(app.log) + 프레임 hex 덤프(dump.log), 보존 기간 자동 정리 |

---

## eventbus

토픽 기반의 발행-구독(Publish-Subscribe) 패턴 구현체입니다.

### 주요 기능

- **Event 인터페이스 없음** — 어떤 타입이든 이벤트로 사용 가능합니다.
- **`Subscribe[E]`** — 핸들러를 타입 안전하게 등록하고 unsubscribe 클로저를 반환합니다.
- **`Publish[E]`** — 비동기 fire-and-forget 발행. 핸들러 에러는 `DroppedEventHandler`로 전달됩니다.
- **`PublishSync[E]`** — 모든 핸들러가 완료될 때까지 대기하고 `[]error`를 반환합니다.
- **`Request[E]`** — 모든 핸들러 응답을 timeout 내에 수집해 `([]any, []error)`를 반환합니다.

### 사용 예시

```go
eb := eventbus.NewEventBus(ctx)
defer eb.Close()

// 에러 알림 등록
eb.SetDroppedEventHandler(func(topic string, event any, err error) {
    log.Printf("[dropped] topic=%s err=%v", topic, err)
})

// 구독 — 핸들러가 구체 타입을 바로 받음, 타입 단언 불필요
unsubscribe := eventbus.Subscribe(eb, "order.created", func(e *OrderEvent) (any, error) {
    fmt.Println("주문 처리:", e.OrderId)
    return nil, nil
})
defer unsubscribe()

// 비동기 발행
eventbus.Publish(eb, "order.created", &OrderEvent{OrderId: "ORD-001"})

// 동기 발행 — 모든 핸들러 완료 대기
errs := eventbus.PublishSync(eb, "order.created", &OrderEvent{OrderId: "ORD-002"})

// 요청/응답 — 핸들러 반환값 수집
replies, errs := eventbus.Request(eb, "order.created", &OrderEvent{OrderId: "ORD-003"}, 3*time.Second)
```

자세한 내용은 [eventbus/README.md](eventbus/README.md)를 참고하세요.

---

## sse

Server-Sent Events(SSE) 연결을 계층적으로 관리하는 패키지입니다. Gin 프레임워크를 기반으로 합니다.

### 계층 구조

```
SessionManager
  └── Session  (userId 1 : 세션 1)
        └── SSEClient  (기기/탭 등 다수 연결)
```

### 주요 타입

- **`SSEClient[T, K]`** — `http.Flusher`를 이용해 `event / data / id / retry` 필드를 SSE 형식으로 전송합니다.
- **`Session[T, K]`** — 한 유저의 여러 클라이언트를 묶어 관리하며, `Broadcast()`로 전체에 전송합니다.
- **`SessionManager[T, K]`** — `userId ↔ sessionId` 이중 인덱스로 O(1) 조회를 지원합니다.

### 사용 예시

```go
manager := sse.NewSessionManager[int, string]()

session := manager.NewSession(userId)
client, _ := sse.NewSSEClient[int, string](clientId, userId, ginCtx)
session.AddClient(clientId, client)

session.Broadcast(myEvent)
manager.RemoveSession(userId)
```

---

## tcpnet

TCP 네트워크 통신을 위한 패키지입니다. `basic`과 `advanced` 두 레이어로 구성됩니다. 자세한 내용은 [tcpnet/README.md](tcpnet/README.md)를 참고하세요.

### basic

원시 TCP 연결을 관리하는 기반 구조입니다.

#### ServerBase

```go
server, _ := server.NewServerBase(ctx, heartbeat, maxTimeOutCnt)
server.SetIpAndPort("0.0.0.0", "8080")
server.SetHandleConnectFunc(func(conn net.Conn) { ... })
server.SetPanicHandler(func(conn net.Conn, r any, stack []byte) { ... })
server.Start() // Shutdown 전까지 블로킹
```

- `Listening()` — TCP 리스너를 시작합니다.
- `StartTCPServerHeartbeat()` — 하트비트로 리스너 상태를 감지하고 리스너를 다시 엽니다.
- `SetPanicHandler()` — 연결 핸들러 고루틴의 패닉을 복구한 뒤 호출됩니다. 해당 conn은 서버가 닫고 다른 연결은 계속 처리합니다.
- `ActiveConnCount()` — 현재 핸들러가 실행 중인 연결 수입니다.
- `Shutdown()` — 리스너와 활성 연결을 모두 닫고, Accept 루프·heartbeat·모든 연결 핸들러가 반환될 때까지 기다립니다. 이후 재시작은 에러를 반환합니다.

#### ClientBase

```go
client, _ := client.NewClientBase(ctx, connectTimeout, heartbeat)
client.SetIpAndPort("127.0.0.1", "8080")
client.SetHandleConnectFunc(func(conn net.Conn) { ... })
client.Start()
client.SendMessage(data, timeout) // 연결 전이면 에러 반환
```

- `StartTCPClientHeartbeat()` — 하트비트로 연결 상태를 감지하고 자동 재연결합니다.
- `Shutdown()` — 연결을 닫고 heartbeat 고루틴이 끝날 때까지 기다립니다. 이후 재연결하지 않습니다.

### advanced

패킷 단위 메시지 처리를 위한 구조입니다.

#### model

- **`ConnContext`** — 파싱된 메시지를 채널로 전달하는 연결 컨텍스트입니다. 닫힌 뒤 `SetParsedMsg`는 `ErrContextClosed`를 반환합니다.
- **`HandleContext`** — 핸들러 함수에 전달되는 처리 컨텍스트입니다.
- **`PendingRequests`** — 요청-응답 패턴을 위한 관리자입니다. 요청을 key로 `Register`하고, 응답 패킷을 받으면 `Resolve`로 대기 중인 요청에 `Response`를 전달합니다.

#### parser

```go
type Parser interface {
    Parse(conn net.Conn) (tcpmd.ParseMsg, error)
}
```

`net.Conn`에서 읽어 `ParseMsg`를 반환하는 인터페이스입니다. 구현은 사용처에서 정의합니다.

#### handler

```go
manager := handler.New[uint16]()
manager.RegisterHandle(packetId, func(c *tcpmd.HandleContext) error {
    msg := c.GetParseMsg()
    // ...
    return nil
})

// 비동기 처리
manager.HandleMessage(connCtx)

// 동기 처리
manager.HandleMessageSync(connCtx)
```

패킷 ID 타입을 제네릭으로 지정하며, 핸들러를 ID별로 등록해 자동 디스패치합니다.  
메시지의 packetId 타입이 다르면 에러를 반환하고, `HandleMessageSync`는 핸들러 패닉을 에러로 변환해 반환합니다.

#### serializer

빌더 패턴으로 바이너리 패킷을 조립합니다.

```go
writer := serializer.NewBinaryWriter(binary.BigEndian)
data := writer.
    WriteUint16(packetType).
    WriteUint32(payloadLen).
    WriteBytes(payload).
    Bytes()
```

---

## udpnet

비연결형 UDP 통신을 위한 패키지입니다. 연결 상태나 재연결 개념 없이 `Endpoint` 하나로 송수신하고, 상대의 수신 가능 여부는 핑퐁으로 확인합니다. 자세한 내용은 [udpnet/README.md](udpnet/README.md)를 참고하세요.

### Endpoint

```go
ep, _ := udpnet.NewEndpoint(ctx, udpnet.Config{
    LocalAddr:    ":7000", // 수신 포트
    SendBindAddr: ":7100", // (선택) 송신 전용 소켓 = 출발 포트
})
ep.SetHandler(func(data []byte, src *net.UDPAddr) {
    ep.SendTo(resp, &net.UDPAddr{IP: src.IP, Port: 9001}) // 상대 수신 포트가 다르면 직접 지정
})
ep.Start()
defer ep.Shutdown()

ep.SendTo(data, device)
```

- `SendBindAddr` — 송신 소켓을 수신 소켓과 분리합니다 (목적지가 아니라 출발 주소).
- `MulticastGroup`, `MulticastInterface` — 멀티캐스트 그룹에 가입해 수신합니다. 브로드캐스트는 `SendTo`로 전송합니다.
- `SetPanicHandler()` — 패킷 핸들러 패닉을 복구한 뒤 호출됩니다.
- `Shutdown()` — 수신을 멈추고 처리 중인 핸들러가 끝날 때까지 기다린 뒤 소켓을 닫습니다 (그동안 응답 전송 가능).

### Probe

```go
probe, _ := ep.StartProbe(udpnet.Probe{
    Target:   "192.168.0.10:6000",
    Interval: time.Second,
    Ping:     []byte{0x01},
    IsPong:   func(data []byte, _ *net.UDPAddr) bool { return data[0] == 0x81 },
    OnChange: func(reachable bool) { ... },
})
probe.Reachable()
```

대상 IP에서 온 패킷은 pong이 아니어도 생존 신호로 보며, 트래픽이 있는 동안에는 ping을 생략합니다.

### advanced

tcpnet과 같은 `parser` / `model` / `handler` / `serializer` 구조입니다.  
`SessionContext`는 `Sender`(`Endpoint`가 구현)를 받아 핸들러에서 `HandleContext.Send` / `SendTo`로 전송하며, 요청-응답 대기는 `PendingRequests`를 사용합니다.

```go
type Parser interface {
    Parse(data []byte, addr *net.UDPAddr) (udpmd.ParseMsg, error)
}
```

---

## serialport

시리얼 포트 통신 패키지입니다. tcpnet과 동일한 설계 철학을 `go.bug.st/serial` 기반으로 적용합니다.

### basic/SerialBase

```go
base := serialport.NewSerialBase(ctx)
base.SetPortAndMode("/dev/ttyUSB0", &serial.Mode{BaudRate: 9600})
base.SetHandleConnectFunc(func(port serial.Port) { ... })
base.Start()
base.WriteAll(data)
```

### advanced/handler

tcpnet의 `handler.Manager`와 동일한 구조입니다. 패킷 ID 기반 핸들러 등록 및 비동기/동기 디스패치를 지원합니다.

---

## redisorm

Redis를 관계형 DB처럼 사용하는 제네릭 Repository 패턴 구현체입니다. RedisJSON 모듈이 필요합니다.

### Model 인터페이스 구현

```go
type User struct {
    Id    int64  `json:"id"`
    Email string `json:"email"`
}

func (u *User) GetId() int64               { return u.Id }
func (u *User) SetId(id int64)             { u.Id = id }
func (u *User) TableName() string          { return "user" }
func (u *User) GetIndexFields() map[string]any { return map[string]any{"email": u.Email} }
func (u *User) GetTTL() time.Duration      { return 0 }
```

### Redis 키 구조

| 키 패턴 | 타입 | 용도 |
| --- | --- | --- |
| `{table}:{id}` | JSON | 레코드 본체 |
| `{table}:idx:{field}` | Hash | 인덱스 필드 → ID 매핑 |
| `{table}:all_ids` | Set | 전체 ID 목록 |
| `next_{table}_id` | String | 자동 증가 ID |

### 주요 기능

```go
client := redisorm.NewRedisClient(addr, passwd, db, protocol, timeout)
repo := redisorm.NewRepository[*User](client, &User{})

repo.Create(&User{Email: "a@example.com"})
repo.FindById(1)
repo.FindByIndex("email", "a@example.com")
repo.FindAll()
repo.Update(&User{Id: 1, Email: "b@example.com"})
repo.Delete(1)

repo.GetTTL(1)
repo.RenewTTL(1, 24*time.Hour)
repo.RemoveTTL(1)
repo.CleanupExpired()
```

- **트랜잭션**: `TxPipelined`으로 원자적 쓰기를 보장합니다.
- **중복 방지**: 인덱스 필드 값 중복 시 `Create`가 에러를 반환합니다.
- **TTL**: 모델의 `GetTTL()` 반환값을 우선 적용하고, `0`이면 저장소 기본값을 사용합니다.

---

## utils

범용 유틸리티 함수 모음입니다.

### 구조체 조작

```go
// reflect 기반 깊은 복사 (포인터 전달)
utils.DeepCopy(&src, &dst)

// 인덱스 범위로 구조체 필드 일괄 수정
utils.ModifyStructByIndex(&s, 0, -1, []interface{}{val1, val2})

// 필드명 맵으로 구조체 필드 선택 수정
utils.ModifyStructByMap(&s, map[string]interface{}{"Name": "Alice", "Age": 30})
```

### 비트마스크 변환 (제네릭)

```go
// 정수값 → 비트 위치 인덱스 배열
idxs := utils.ValToIdx[int](0b1010)  // [2, 4]

// 인덱스 배열 → 정수값
val := utils.IdxToVal[int]([]int{2, 4})  // 10
```

### 수치 처리

```go
// precision/scale 기반 소수 반올림 및 범위 클램핑
result := utils.ConvertDecimal(3.14159, 5, 2)  // 3.14

// SHA-256 파일 체크섬 (파일 포인터 위치 보존)
checksum, _ := utils.CalculateChecksumFromFile(file)
```

### 시간/타임존

```go
// 시간값은 유지하고 타임존만 교체
converted := utils.ConvertOnlyTimezone(t, utils.LocalKorea)
```

---

## shell

쉘 스크립트를 동적으로 실행하는 패키지입니다.

```go
executor := shell.NewScriptExecutor("/tmp/scripts")

result, err := executor.Execute(
    ctx,
    "bash",           // 쉘 이름
    "deploy.sh",      // 스크립트 파일명
    scriptContent,    // 스크립트 내용
    true,             // 실행 후 파일 자동 삭제
)

fmt.Println(result.ExitCode)
fmt.Println(result.Stdout)
fmt.Println(result.Stderr)
fmt.Println(result.Duration)
```

스크립트 내용을 파일로 저장한 뒤 지정한 쉘로 실행하며, stdout과 stderr를 줄 단위로 수집합니다.

---

## logger

```go
l, err := logger.Init(logger.Options{
    Dir:        "log",      // log/YYYY/MM/D/app.log, dump.log
    Level:      lv,         // logger.ParseLevel(cfg.Log.Level)
    Dump:       cfg.Log.Dump,
    MaxAgeDays: 3,          // 오늘 + 지난 3일 보존. 음수면 정리 안 함
    Console:    os.Stderr,  // nil 이면 콘솔 출력 없음
})
if err != nil { ... }
defer logger.Close()

logger.Infof("started")
logger.Dump("TCP", logger.Tx, clientID, op, frame)
logger.SetLevel(logger.DEBUG) // 실행 중 변경 (저장 안 됨)
```

인스턴스를 직접 들고 써도 된다 (`l.Infof`, `l.Dump` …). 한 프로세스에서 로그 디렉터리를 나누고 싶을 때 쓴다.

---

## 설계 공통 패턴

| 패턴 | 적용 패키지 |
| --- | --- |
| 제네릭 타입 파라미터 | sse, tcpnet, udpnet, serialport, orm, utils |
| `context.Context` + `cancel` | 전 패키지 |
| `sync.RWMutex` 읽기/쓰기 분리 | 전 패키지 |
| 하트비트 기반 자동 재연결 | tcpnet, serialport |
| 핑퐁 기반 생존 확인 | udpnet |
| 핸들러 함수 주입 패턴 | tcpnet, udpnet, serialport |
| 핸들러 고루틴 패닉 복구 + 훅 | tcpnet, udpnet |
| 빌더 패턴 | tcpnet/serializer, udpnet/serializer |
