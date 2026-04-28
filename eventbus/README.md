# eventbus

토픽 기반의 발행-구독(Publish-Subscribe) 패턴 구현체입니다.

## 특징

- **Event 인터페이스 없음** — 어떤 타입이든 이벤트로 사용 가능
- **제네릭 함수로 타입 안전 보장** — 핸들러가 구체 타입을 바로 받음, 타입 단언 불필요
- **3가지 발행 방식** — 비동기(`Publish`) / 동기(`PublishSync`) / 요청-응답(`Request`)
- **클로저 unsubscribe** — `Subscribe` 반환값 호출로 구독 해제
- **에러 알림** — `Publish` 중 핸들러 에러를 `DroppedEventHandler`로 수신

## 설치

```bash
go get github.com/Jaeun-Choi98/modules/eventbus
```

## API

```go
// 버스 생성/종료
func NewEventBus(ctx context.Context) *EventBus
func (b *EventBus) Close()
func (b *EventBus) SetDroppedEventHandler(f DroppedEventFunc)

// 패키지 레벨 제네릭 함수
func Subscribe[E any](b *EventBus, topic string, handler func(E) (any, error)) func()
func Publish[E any](b *EventBus, topic string, event E)
func PublishSync[E any](b *EventBus, topic string, event E) []error
func Request[E any](b *EventBus, topic string, event E, timeout time.Duration) ([]any, []error)
```

## 발행 방식 비교

| 함수 | 실행 방식 | 반환값 | 용도 |
|---|---|---|---|
| `Publish` | 비동기 (fire-and-forget) | 없음 | 결과가 필요 없는 이벤트 알림 |
| `PublishSync` | 병렬 + 완료 대기 | `[]error` | 모든 핸들러 완료를 확인해야 할 때 |
| `Request` | 병렬 + timeout | `([]any, []error)` | 핸들러 응답값이 필요할 때 |

---

## 사용 예시

### 이벤트 정의

별도 인터페이스 구현 없이 어떤 타입이든 이벤트로 사용할 수 있습니다.

```go
type OrderEvent struct {
    OrderId string
    Amount  int
}

type UserEvent struct {
    UserId string
    Action string
}
```

### 기본 설정

```go
eb := eventbus.NewEventBus(context.Background())
defer eb.Close()

// 핸들러 에러 알림 등록 (선택)
eb.SetDroppedEventHandler(func(topic string, event any, err error) {
    log.Printf("[dropped] topic=%s err=%v", topic, err)
})
```

### Subscribe — 구독 등록 및 해제

핸들러는 구체 타입을 바로 받습니다. 타입 단언이 필요 없습니다.

```go
unsubscribe := eventbus.Subscribe[*OrderEvent](eb, "order.created",
    func(e *OrderEvent) (any, error) {
        fmt.Printf("주문 수신: %s, 금액: %d\n", e.OrderId, e.Amount)
        return nil, nil
    },
)
defer unsubscribe()
```

타입 파라미터는 대부분 핸들러 시그니처에서 추론됩니다.

```go
// 명시
eventbus.Subscribe[*OrderEvent](eb, "order.created", handler)

// 추론 가능한 경우 생략
handler := func(e *OrderEvent) (any, error) { ... }
eventbus.Subscribe(eb, "order.created", handler)
```

### Publish — 비동기 발행

```go
event := &OrderEvent{OrderId: "ORD-001", Amount: 50000}

eventbus.Publish(eb, "order.created", event)
// 즉시 반환, 핸들러는 백그라운드에서 실행됨
```

### PublishSync — 동기 발행

모든 핸들러가 완료될 때까지 대기합니다. 핸들러들은 병렬로 실행됩니다.

```go
eventbus.Subscribe(eb, "order.created", func(e *OrderEvent) (any, error) {
    return nil, nil // DB 저장
})
eventbus.Subscribe(eb, "order.created", func(e *OrderEvent) (any, error) {
    return nil, fmt.Errorf("알림 서버 연결 실패")
})

errs := eventbus.PublishSync(eb, "order.created", &OrderEvent{OrderId: "ORD-002"})
if len(errs) > 0 {
    for _, err := range errs {
        log.Printf("핸들러 실패: %v", err)
    }
}
```

### Request — 응답 수집

모든 핸들러의 반환값을 timeout 내에 수집합니다.

```go
eventbus.Subscribe(eb, "product.price.query", func(e *ProductQuery) (any, error) {
    return &PriceResult{Source: "DB", Price: 15000}, nil
})
eventbus.Subscribe(eb, "product.price.query", func(e *ProductQuery) (any, error) {
    return &PriceResult{Source: "cache", Price: 15000}, nil
})

replies, errs := eventbus.Request(eb, "product.price.query", &ProductQuery{ProductId: "P-001"}, 3*time.Second)

for _, reply := range replies {
    result := reply.(*PriceResult)
    fmt.Printf("source: %s, price: %d\n", result.Source, result.Price)
}
```

### 여러 타입을 하나의 버스에서 관리

버스 하나로 다양한 이벤트 타입을 독립적으로 운영할 수 있습니다.

```go
eb := eventbus.NewEventBus(ctx)

eventbus.Subscribe(eb, "order.created", func(e *OrderEvent) (any, error) {
    fmt.Println("주문:", e.OrderId)
    return nil, nil
})

eventbus.Subscribe(eb, "user.login", func(e *UserEvent) (any, error) {
    fmt.Println("로그인:", e.UserId)
    return nil, nil
})

eventbus.Publish(eb, "order.created", &OrderEvent{OrderId: "ORD-001"})
eventbus.Publish(eb, "user.login", &UserEvent{UserId: "U-001"})
```

---

## 주의사항

- `Close()` 호출 후에는 발행 함수를 호출하지 않아야 합니다.
- `Request`는 timeout 초과 시 그때까지 수집된 응답과 timeout 에러를 함께 반환합니다.
- 핸들러 내 panic은 복구되어 에러로 처리되므로 다른 핸들러 실행이 중단되지 않습니다.
- 토픽에 등록된 핸들러의 타입과 발행 이벤트 타입이 다르면 해당 핸들러에서 에러가 반환됩니다.
