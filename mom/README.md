# RabbitMQ

## Producer

---

- 데이터 생산자. 메시지를 보내는 애플리케이션

## Consumer

---

- 데이터 수신. 메시지 수신을 대기하는 애플리케이션
- **Message acknowledgment**
작업을 수행하는데 시간이 다소 걸릴 수 있고, 작업을 완료하기 전에 종료하면 메시지 손실이 발생할 수 있음 
**→** Consumer은 특정 메시지가 수신되어 처리되었다는 것을 알림으로써 해결.

```go
msgs, err := ch.Consume(
  q.Name, // queue
  "",     // consumer
  false,  // auto-ack -> false로 지정함으로 써 수동 메시지 확인을 사용.
  false,  // exclusive
  false,  // no-local
  false,  // no-wait
  nil,    // args
)

go func() {
  for d := range msgs {
    log.Printf("Received a message: %s", d.Body)
    dotCount := bytes.Count(d.Body, []byte("."))
    t := time.Duration(dotCount)
    time.Sleep(t * time.Second)
    log.Printf("Done")
    d.Ack(false) // 메시지가 처리되었음을 rabbitmq 서버에 알림.
  }
}()
// ...
```

## Queue

---

- 우체국 상자. 호스트 메모리 및 디스크 용량 제한에 영향을 받으며 대용량 메시지 버퍼.
- 메시지를 Consumer에게 라운드 로빈 방식으로 전달. → 공평한 처리를 하지 못함. ( fair dispatch 문제 발생)
channel에 prefetch를 설정함으로써 공평한 일 배분
    
    ```go
    ch.Qos(
    	1, // prefetch count
    	0, // prefetch size
    	false // global
    )
    ```
    
- RabbitMQ가 종료되거나 충돌 시 큐와 메시지 삭제 발생.
→ 큐와 메시지 모두 내구성(durability)를 표시

```go
// 큐 정
q, err := ch.QueueDeclare(
  "task_queue", // name
  true,         // durable, true로 표시함으로 써 큐에 내구성을 표시 
  false,        // delete when unused
  false,        // exclusive
  false,        // no-wait
  nil,          // arguments
)

// 메시지를 송신할 때
err = ch.PublishWithContext(ctx,
  "",           // exchange
  q.Name,       // routing key
  false,        // mandatory
  false,
  amqp.Publishing {
    DeliveryMode: amqp.Persistent, // 내구성을 표시함으로 써. 메시지 손실 방지.
    ContentType:  "text/plain",
    Body:         []byte(body),
})
```

- 이전 메시지에 상관없이 현재 메시지만 중요할 경우 → Temporary queues
1. 무작위 이름을 가진 새로운 Queue 생성.
2. Consumer과 연결이 끊기면 자동으로 삭제.

```go
q, err := ch.QueueDeclare(
  "",    // name, 이름을 빈 스트링으로 줌으로 써 무작위 이름을 가진 큐 생성.
  false, // durable
  false, // delete when unused
  true,  // exclusive, 배타적으로 생성함으로써 자동으로 삭제.
  false, // no-wait
  nil,   // arguments
)
```

## Exchange

---

- **Producer가 직접 메시지를 Queue전달하지 않음**.
- Producer은 메시지를 Exchange에게 송신하고, Exchange는 다시 Queue에게 메시지를 전달함.
- Exchange에서 Queue에게 메시지를 전달하는 것을 Binding이라 함.

```go
err = ch.QueueBind(
  q.Name, // queue name
  "",     // routing key
  "logs", // exchange
  false,
  nil,
)
```

- **default, direct, topic, headers, fanout** 타입이 있음.
    - Default
    Default Exchange가 메시지를 수신하면, **라우팅 키**를 확인한 후에 **같은 이름의 큐**에 자동 바인딩.
    따로 Exchagne와 Queue를 바인딩하지 않아도 됨.
    - Fanout
    수신한 모든 메시지를 알고 있는 모든 큐에 브로드캐스트하는 방식.
    
    ```go
    //======Producer======//
    // 채널 재정의
    err = ch.ExchangeDeclare(
      "exchagneName",   // name
      "fanout", // type
      true,     // durable
      false,    // auto-deleted
      false,    // internal
      false,    // no-wait
      nil,      // arguments
    )
    // 정의한 exchange의 이름을 사용함으로 써. producer가 메시지를 exchange에게 전달
    err = ch.PublishWithContext(ctx,
      "exchagneName", // exchange 
      "",     // routing key -> 바인딩 키는 Exchange 유형에 따라 달라짐.
      false,  // mandatory
      false,  // immediate
      amqp.Publishing{
              ContentType: "text/plain",
              Body:        []byte(body),
      })
    ```
    
    - Direct
    메시지는 바인딩 키가 메시지의 라우팅 키와 정확히 일치하는 큐로 전달.
    
    ```go
    //======Producer======//
    // 채널 재정의
    err = ch.ExchangeDeclare(
      "exchagneName",   // name
      "direct", // type
      true,     // durable
      false,    // auto-deleted
      false,    // internal
      false,    // no-wait
      nil,      // arguments
    )
    // 정의한 exchange의 이름을 사용함으로 써. producer가 메시지를 exchange에게 전달
    err = ch.PublishWithContext(ctx,
      "exchagneName", // exchange 
      "routingKey",     // routing key를 지정함.
      false,  // mandatory
      false,  // immediate
      amqp.Publishing{
              ContentType: "text/plain",
              Body:        []byte(body),
      })
      //======Producer======//
      err = ch.QueueBind(
      q.Name, // queue name
      "routingKey", // routingKey인 메세지만 Exchange로 부터 수신 받음.
      "logs", // exchange
      false,
      nil,
    )
    ```
    
    - Topic
    Direct와 논리와 비슷함. 라우팅 키와 함께 보낸 메시지는 일치하는 바인딩 키로 바인딩된 모든 Queue에 전달 됨.
    
    <reg 규칙>
    1. 점으로 구분된 단어 목록
    2. * (별표)는 정확히 하나의 단어를 대체. e.g. *.Orange → one.Orange, two.Orange
    3. # (해시)는 0개 이상의 단어를 대체. e.g. #.Orange → one.Orange, one.two.Orange
    - Headers
    라우팅 키를 사용하지 않고 메시지의 Headers 테이블과 비교.
    더 자세한건 채팅AI ㄱㄱ…