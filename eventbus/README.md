# eventbus

토픽 기반의 발행-구독(Publish-Subscribe) 패턴 구현체입니다.

## 특징

- **3가지 발행 방식** — 비동기(`Publish`) / 동기(`PublishSync`) / 요청-응답(`Request`)
- **콜백 기반 구독** — 채널 관리 없이 핸들러 함수만 등록
- **클로저 unsubscribe** — `Subscribe` 반환값 호출로 구독 해제
- **에러 알림** — `Publish` 중 핸들러 에러를 `DroppedEventHandler`로 수신

## 설치

```bash
go get github.com/Jaeun-Choi98/modules/eventbus
```

## 핵심 타입

```go
// 이벤트 인터페이스
type Event interface {
    GetEventId() uint32
}

// 핸들러 함수 — Publish에서는 반환값 무시, Request에서는 응답으로 수집
type HandlerFunc func(Event) (any, error)

// Publish 중 핸들러 에러 발생 시 호출되는 콜백
type DroppedEventFunc func(topic string, event Event, err error)
```

## 발행 방식 비교

| 메서드 | 실행 방식 | 반환값 | 용도 |
|---|---|---|---|
| `Publish` | 비동기 (fire-and-forget) | 없음 | 결과가 필요 없는 이벤트 알림 |
| `PublishSync` | 병렬 + 완료 대기 | `[]error` | 모든 핸들러 완료를 확인해야 할 때 |
| `Request` | 병렬 + timeout | `([]any, []error)` | 핸들러 응답값이 필요할 때 |

---

## 사용 예시

### 이벤트 정의

```go
type OrderEvent struct {
    id      uint32
    OrderId string
    Amount  int
}

func (e *OrderEvent) GetEventId() uint32 { return e.id }
```

### 기본 설정

```go
eb := eventbus.NewEventBus(context.Background())
defer eb.Close()

// 핸들러 에러 알림 등록 (선택)
eb.SetDroppedEventHandler(func(topic string, event eventbus.Event, err error) {
    log.Printf("[dropped] topic=%s eventId=%d err=%v", topic, event.GetEventId(), err)
})
```

### Subscribe — 구독 등록 및 해제

```go
// 구독 등록 — 반환된 함수로 해제
unsubscribe := eb.Subscribe("order.created", func(e eventbus.Event) (any, error) {
    order := e.(*OrderEvent)
    fmt.Printf("주문 수신: %s, 금액: %d\n", order.OrderId, order.Amount)
    return nil, nil
})

// 구독 해제
defer unsubscribe()
```

### Publish — 비동기 발행

핸들러를 고루틴으로 실행하고 즉시 반환합니다. 결과가 필요 없는 이벤트 알림에 사용합니다.

```go
event := &OrderEvent{id: 1, OrderId: "ORD-001", Amount: 50000}

eb.Publish("order.created", event)
// 즉시 반환, 핸들러는 백그라운드에서 실행됨
```

### PublishSync — 동기 발행

모든 핸들러가 완료될 때까지 대기합니다. 핸들러들은 병렬로 실행됩니다.

```go
unsubA := eb.Subscribe("order.created", func(e eventbus.Event) (any, error) {
    // DB 저장
    return nil, nil
})
unsubB := eb.Subscribe("order.created", func(e eventbus.Event) (any, error) {
    // 알림 발송 — 실패 상황 가정
    return nil, fmt.Errorf("알림 서버 연결 실패")
})
defer unsubA()
defer unsubB()

event := &OrderEvent{id: 2, OrderId: "ORD-002", Amount: 30000}

errs := eb.PublishSync("order.created", event)
if len(errs) > 0 {
    for _, err := range errs {
        log.Printf("핸들러 실패: %v", err)
    }
}
```

### Request — 응답 수집

모든 핸들러의 반환값을 timeout 내에 수집합니다.

```go
unsubPrice := eb.Subscribe("product.price.query", func(e eventbus.Event) (any, error) {
    // 가격 조회 후 반환
    return map[string]any{"source": "DB", "price": 15000}, nil
})
unsubCache := eb.Subscribe("product.price.query", func(e eventbus.Event) (any, error) {
    // 캐시 조회 후 반환
    return map[string]any{"source": "cache", "price": 15000}, nil
})
defer unsubPrice()
defer unsubCache()

queryEvent := &OrderEvent{id: 3, OrderId: "PROD-001"}

replies, errs := eb.Request("product.price.query", queryEvent, 3*time.Second)

for _, reply := range replies {
    data := reply.(map[string]any)
    fmt.Printf("응답 — source: %s, price: %v\n", data["source"], data["price"])
}
if len(errs) > 0 {
    log.Printf("일부 핸들러 실패: %v", errs)
}
```

### 다수 구독자에게 동시 발행

```go
for i := range 5 {
    idx := i
    eb.Subscribe("user.login", func(e eventbus.Event) (any, error) {
        fmt.Printf("구독자 %d 처리\n", idx)
        return nil, nil
    })
}

// 5명의 구독자가 모두 병렬로 실행되고 완료될 때까지 대기
errs := eb.PublishSync("user.login", &OrderEvent{id: 10})
fmt.Printf("완료, 에러 수: %d\n", len(errs))
```

---

## 주의사항

- `Close()` 호출 후에는 `Publish` / `PublishSync` / `Request`를 호출하지 않아야 합니다.
- `Request`는 timeout 초과 시 그때까지 수집된 응답과 timeout 에러를 함께 반환합니다.
- 핸들러 내 panic은 복구되어 에러로 처리되므로 전체 발행 흐름이 중단되지 않습니다.
