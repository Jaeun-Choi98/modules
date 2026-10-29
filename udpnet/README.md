# udpnet

> `github.com/Jaeun-Choi98/modules/udpnet`

비연결형 UDP 통신을 위한 Go 모듈입니다.  
UDP에는 연결이 없으므로 연결 상태 플래그나 재연결 개념을 두지 않습니다.  
소켓은 `Start()`에서 한 번 열고 `Shutdown()`에서 닫으며, 상대가 지금 수신 가능한지는 **핑퐁(Probe)**으로 확인합니다.

---

## 패키지 구성

```
udpnet/
├── endpoint.go      # Endpoint: 송수신, 송신 소켓 분리, 멀티캐스트, 종료 순서, 패닉 복구
├── probe.go         # Prober: 핑퐁 기반 생존 확인
└── advanced/
    ├── parser/      # 데이터그램 파싱 인터페이스
    ├── model/       # 세션 컨텍스트 (파싱 결과, 요청-응답 대기 등)
    ├── handler/     # 패킷 식별 기반 처리 로직 분배
    └── serializer/  # 바이너리 패킷 조립 (빌더 패턴)
```

---

## 설치

```bash
go get github.com/Jaeun-Choi98/modules/udpnet
```

---

## Endpoint

연결하지 않은(unconnected) UDP 소켓을 감싼 송수신 단위입니다. 클라이언트·서버 구분 없이 하나의 타입으로 사용합니다.  
수신은 `ReadFromUDP`, 송신은 `WriteToUDP`를 사용하므로 상대가 어느 포트에서 보내든 수신하고, 목적지는 매번 지정합니다.

### Config

| 필드 | 설명 |
|------|------|
| `LocalAddr` | 수신 소켓 바인드 주소. 비우거나 포트가 0이면 임의 포트 |
| `SendBindAddr` | (선택) 송신 전용 소켓을 바인드할 로컬 주소 = 송신 패킷의 **출발 주소** (목적지 아님). 비우면 수신 소켓으로 송신 |
| `MulticastGroup` | (선택) `LocalAddr`의 포트로 가입할 멀티캐스트 그룹 (예: `"239.0.0.1"`) |
| `MulticastInterface` | (선택) 멀티캐스트 가입 인터페이스 이름. 비우면 시스템 기본 |
| `ReadBufferSize` | 수신 버퍼 크기. 0이면 65535 |

### 메서드

| 메서드 | 설명 |
|--------|------|
| `SetHandler(func(data, src))` | 수신 패킷 처리 함수. 패킷마다 별도 고루틴에서 호출 |
| `SetPanicHandler(func(src, data, r, stack))` | 핸들러 패닉 시 호출. nil이면 로그만 남김 |
| `Start()` | 소켓을 열고 수신 시작 |
| `SendTo(data, addr)` | 전송. 브로드캐스트·멀티캐스트 주소도 가능 |
| `StartProbe(Probe)` | 핑퐁 생존 확인 시작 (`Start()` 이후) |
| `LocalAddr()` / `SendLocalAddr()` | 수신·송신 소켓의 실제 로컬 주소 |
| `Shutdown()` | 순서대로 정리 후 종료 |

### 포트 구성

| 구성 | 방법 |
|------|------|
| 송신 소켓과 수신 소켓 분리 | `Config.SendBindAddr` |
| 상대의 수신 포트가 송신 포트와 다름 | 핸들러에서 `SendTo(data, &net.UDPAddr{IP: src.IP, Port: 9001})` |
| 멀티캐스트 수신 | `Config.MulticastGroup`, `Config.MulticastInterface` |
| 브로드캐스트 송신 | `SendTo(data, &net.UDPAddr{IP: net.IPv4bcast, Port: p})` (Go가 `SO_BROADCAST`를 기본 설정) |

```go
// 여러 장비의 요청을 받아 응답하는 서버.
// 장비는 임의 포트에서 보내지만 9001번 포트에서만 수신하고,
// 우리 쪽 송신은 방화벽 정책에 맞춰 9100번 송신 전용 소켓을 사용.
ep, _ := udpnet.NewEndpoint(ctx, udpnet.Config{
    LocalAddr:    ":9000",
    SendBindAddr: ":9100",
})
ep.SetHandler(func(data []byte, src *net.UDPAddr) {
    ep.SendTo(data, &net.UDPAddr{IP: src.IP, Port: 9001})
})
ep.Start()
defer ep.Shutdown()
```

---

## Probe (핑퐁 생존 확인)

```go
probe, _ := ep.StartProbe(udpnet.Probe{
    Target:   "192.168.0.10:6000",
    Interval: time.Second,
    Ping:     []byte{0x01},
    IsPong:   func(data []byte, _ *net.UDPAddr) bool { return len(data) == 1 && data[0] == 0x81 },
    OnChange: func(reachable bool) { log.Printf("device reachable: %v", reachable) },
})

if probe.Reachable() {
    ep.SendTo(req, device)
}
```

| 필드 | 설명 |
|------|------|
| `Target` | ping을 보낼 주소 (필수) |
| `Interval` | 확인 주기 (필수) |
| `Timeout` | 마지막 수신 후 이 시간이 지나면 수신 불가. 0이면 `Interval*3` |
| `Ping` | 전송할 ping 데이터. nil이면 ping 없이 수신 트래픽만 감시 |
| `IsPong` | true인 패킷은 생존 신호로만 쓰고 핸들러로 전달하지 않음 |
| `MatchSource` | 생존 신호로 볼 패킷인지 판단. nil이면 **출발지 IP만** 비교 (포트 무시) |
| `OnChange` | 수신 가능 상태가 바뀔 때 호출 (probe 고루틴에서 순서대로) |

- 대상에서 온 패킷은 pong이 아니어도 모두 생존 신호로 봅니다.
- 마지막 수신 이후 `Interval` 동안 조용할 때만 ping을 보내므로, 트래픽이 있는 동안에는 ping을 생략합니다.
- 기본 매칭이 IP만 비교하므로 장비가 다른 포트에서 pong을 보내도 인정됩니다.  
  같은 IP에 장비가 여러 대 있거나(NAT 등) `Target`이 멀티캐스트·브로드캐스트 주소면 `MatchSource`를 지정해야 합니다.

---

## 종료 순서와 패닉 복구

`Shutdown()`은 아래 순서로 정리하며, 처리 중인 핸들러가 응답을 보낼 수 있도록 소켓을 마지막에 닫습니다.

1. ctx 취소 → 이후 `Start`, `StartProbe` 차단
2. 읽기 대기 해제 → 수신 루프가 새 패킷을 받지 않고 종료
3. 수신 루프, probe 종료 대기
4. 처리 중인 패킷 핸들러 종료 대기 (이 동안 `SendTo` 가능)
5. 소켓 닫기

패킷 핸들러는 별도 고루틴에서 실행되므로 `Start()`를 호출한 쪽의 `recover`로는 잡히지 않습니다.  
Endpoint가 패킷마다 패닉을 복구하고 `SetPanicHandler`로 등록한 함수를 호출하며, 소켓은 닫지 않고 다음 패킷을 계속 처리합니다.

읽기 에러가 발생해도 소켓을 다시 열지 않고 계속 수신합니다 (예: Windows에서는 ICMP port unreachable이 `ReadFrom` 에러로 올라옴).

---

## advanced: 요청-응답 흐름

장비에 요청 패킷을 보내고, 응답 패킷이 오면 로직을 처리한 뒤 필요하면 결과를 다른 곳으로 보내는 흐름입니다.

```
 요청 고루틴                                  수신 핸들러 (Endpoint → parser → handler)
 ───────────                                  ───────────────────────────────────────
 Register(seq, ch)       ① 응답 대기 등록
 SendTo(req, device)     ② 요청 전송 ────────► 장비 ────► 응답 패킷
                                                          parse → seq 추출
                         ③ 응답 전달 ◄──────────────────── Resolve(seq, resp)
 resp := <-ch            ④ 응답 수신
 logic(resp)             ⑤ 로직 처리
 SendTo(result, remote)  ⑥ (선택) 결과 전송
 Remove(seq)             ⑦ 정리
```

- `SessionContext`는 `Sender` 인터페이스(`SendTo`)를 받으며 `Endpoint`가 이를 구현합니다.  
  핸들러에서는 `HandleContext.Send(data)`(출발지로 전송)나 `HandleContext.SendTo(data, addr)`를 사용합니다.
- key는 주소가 아니라 **시퀀스 번호·커맨드 ID 등 프로토콜 값**을 사용해야 합니다. 장비가 다른 포트에서 응답할 수 있기 때문입니다.
- 응답 채널은 **버퍼 1**로 만들어야 합니다. `Resolve`는 블로킹하지 않으므로 버퍼가 없으면 요청 쪽이 대기에 들어가기 전에 도착한 응답이 `ErrResponseBusy`로 버려집니다.
- UDP는 요청·응답이 유실될 수 있으므로 타임아웃 후 재전송이 필요할 수 있습니다.

```go
import (
	"context"
	"encoding/binary"
	"errors"
	"log"
	"net"
	"sync/atomic"
	"time"

	"github.com/Jaeun-Choi98/modules/udpnet"
	"github.com/Jaeun-Choi98/modules/udpnet/advanced/handler"
	udpmd "github.com/Jaeun-Choi98/modules/udpnet/advanced/model"
)

const (
	CmdStatusReq  uint16 = 0x0010
	CmdStatusResp uint16 = 0x8010
)

type StatusResp struct {
	Seq  uint16
	Body []byte
}

// [cmd 2B][seq 2B][body] 프로토콜 파서. UDP는 데이터그램 하나가 패킷 하나.
type statusParser struct{}

func (statusParser) Parse(data []byte, addr *net.UDPAddr) (udpmd.ParseMsg, error) {
	if len(data) < 4 {
		return nil, errors.New("short packet")
	}
	cmd := binary.BigEndian.Uint16(data[0:2])
	resp := StatusResp{Seq: binary.BigEndian.Uint16(data[2:4]), Body: data[4:]}
	return udpmd.NewGenericParseMsg(addr, cmd, resp), nil
}

func main() {
	ctx := context.Background()
	device := &net.UDPAddr{IP: net.ParseIP("192.168.0.10"), Port: 6000}

	ep, err := udpnet.NewEndpoint(ctx, udpnet.Config{LocalAddr: ":7000"})
	if err != nil {
		log.Fatal(err)
	}

	sess := udpmd.NewSessionContext(ctx, ep, device, 16)
	defer sess.Close()

	// 응답 패킷 → seq로 대기 중인 요청을 찾아 전달
	manager := handler.New[uint16]()
	manager.RegisterHandle(CmdStatusResp, func(c *udpmd.HandleContext) error {
		resp := c.GetParseMsg().GetPacket().(StatusResp)
		return c.GetPending().Resolve(resp.Seq, &udpmd.GenericResponse[StatusResp]{Payload: resp})
	})

	var parser statusParser
	ep.SetHandler(func(data []byte, src *net.UDPAddr) {
		msg, err := parser.Parse(data, src)
		if err != nil {
			return
		}
		if err := sess.SetParsedMsg(msg); err != nil {
			return
		}
		manager.HandleMessageSync(sess)
	})
	if err := ep.Start(); err != nil {
		log.Fatal(err)
	}
	defer ep.Shutdown()

	// 요청 → 응답 대기 → 로직 처리 → (선택) 결과 전송
	var seqGen atomic.Uint32
	seq := uint16(seqGen.Add(1))

	ch := make(chan udpmd.Response, 1) // 버퍼 1 필수: Resolve는 블로킹하지 않음
	sess.GetPending().Register(seq, ch)
	defer sess.GetPending().Remove(seq)

	req := binary.BigEndian.AppendUint16(binary.BigEndian.AppendUint16(nil, CmdStatusReq), seq)
	if err := ep.SendTo(req, device); err != nil {
		log.Fatal(err)
	}

	select {
	case resp, ok := <-ch:
		if !ok {
			log.Println("session closed")
			return
		}
		status := resp.GetPayload().(StatusResp)
		log.Printf("status: % x", status.Body)
		// 필요하면 결과를 다른 곳으로 전송
		// ep.SendTo(result, remote)
	case <-time.After(500 * time.Millisecond):
		log.Println("timeout (UDP는 유실될 수 있으므로 필요하면 재전송)")
	}
}
```

### PendingRequests

| 메서드 | 역할 |
|--------|------|
| `Register(key, ch)` | 응답 대기 등록 |
| `Resolve(key, resp)` | 대기 중인 요청에 응답 전달. 블로킹하지 않으며 `Cancel`과 동시에 호출해도 안전 |
| `Get(key)` | 등록된 채널 조회 |
| `Remove(key)` | 채널을 닫지 않고 등록 해제 (요청 쪽 정리용) |
| `Cancel(key)` / `CancelAll()` | 채널을 닫아 대기 중인 요청을 취소. `SessionContext.Close()` 시 `CancelAll` 호출 |

| 에러 | 의미 |
|------|------|
| `ErrNoPendingRequest` | 해당 key로 기다리는 요청이 없음 (이미 타임아웃으로 떠남) |
| `ErrResponseBusy` | 채널 버퍼가 가득 참 (이미 응답이 전달됨) |
| `ErrContextClosed` | 닫힌 `SessionContext`에 `SetParsedMsg` 호출 |

---

## 라이선스

MIT
