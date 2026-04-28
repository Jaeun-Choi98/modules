package eventbus

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type Event interface {
	GetEventId() uint32
}

// HandlerFunc는 이벤트를 처리하고 응답값과 에러를 반환.
// Publish에서는 반환값이 무시되고, Request에서는 응답으로 수집됨.
type HandlerFunc func(Event) (any, error)

// DroppedEventFunc는 Publish 중 핸들러가 에러를 반환했을 때 호출되는 콜백.
type DroppedEventFunc func(topic string, event Event, err error)

type subscription struct {
	id      uint64
	handler HandlerFunc
}

type EventBus struct {
	subscribers    map[string][]*subscription
	droppedHandler DroppedEventFunc

	mu           sync.RWMutex
	subIdCounter uint64

	ctx    context.Context
	cancel context.CancelFunc
}

func NewEventBus(pctx context.Context) *EventBus {
	ctx, cancel := context.WithCancel(pctx)
	return &EventBus{
		subscribers: make(map[string][]*subscription),
		ctx:         ctx,
		cancel:      cancel,
	}
}

// SetDroppedEventHandler는 Publish 중 핸들러 에러 발생 시 호출될 콜백을 등록.
func (b *EventBus) SetDroppedEventHandler(f DroppedEventFunc) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.droppedHandler = f
}

// Subscribe는 핸들러를 등록하고 unsubscribe 함수를 반환.
// 반환된 func()를 호출하면 구독이 해제됨.
func (b *EventBus) Subscribe(topic string, handler HandlerFunc) func() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.subIdCounter++
	id := b.subIdCounter
	b.subscribers[topic] = append(b.subscribers[topic], &subscription{id: id, handler: handler})

	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		subs := b.subscribers[topic]
		for i, s := range subs {
			if s.id == id {
				b.subscribers[topic] = append(subs[:i], subs[i+1:]...)
				return
			}
		}
	}
}

// Publish는 이벤트를 비동기로 발행 (fire-and-forget).
// 핸들러가 에러를 반환하면 DroppedEventHandler가 호출됨.
func (b *EventBus) Publish(topic string, event Event) {
	handlers, dropped := b.copyHandlers(topic)
	for _, h := range handlers {
		go func(handler HandlerFunc) {
			defer func() { recover() }()
			if _, err := handler(event); err != nil && dropped != nil {
				dropped(topic, event, err)
			}
		}(h)
	}
}

// PublishSync는 모든 핸들러가 완료될 때까지 대기하고 에러 목록을 반환.
// 핸들러들은 병렬로 실행됨.
func (b *EventBus) PublishSync(topic string, event Event) []error {
	handlers, _ := b.copyHandlers(topic)

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)

	for _, h := range handlers {
		wg.Add(1)
		go func(handler HandlerFunc) {
			var err error
			defer func() {
				if r := recover(); r != nil {
					err = fmt.Errorf("panic: %v", r)
				}
				if err != nil {
					mu.Lock()
					errs = append(errs, err)
					mu.Unlock()
				}
				wg.Done()
			}()
			_, err = handler(event)
		}(h)
	}

	wg.Wait()
	return errs
}

// Request는 이벤트를 발행하고 timeout 내에 모든 핸들러의 응답을 수집해서 반환.
// 핸들러들은 병렬로 실행되며, timeout 초과 시 수집된 결과까지만 반환.
func (b *EventBus) Request(topic string, event Event, timeout time.Duration) ([]any, []error) {
	handlers, _ := b.copyHandlers(topic)

	type result struct {
		reply any
		err   error
	}

	resultCh := make(chan result, len(handlers))

	var wg sync.WaitGroup
	for _, h := range handlers {
		wg.Add(1)
		go func(handler HandlerFunc) {
			var r result
			func() {
				defer func() {
					if rec := recover(); rec != nil {
						r.err = fmt.Errorf("panic: %v", rec)
					}
				}()
				r.reply, r.err = handler(event)
			}()
			resultCh <- r
			wg.Done()
		}(h)
	}

	go func() {
		wg.Wait()
		close(resultCh)
	}()

	ctx, cancel := context.WithTimeout(b.ctx, timeout)
	defer cancel()

	replies := make([]any, 0, len(handlers))
	errs := make([]error, 0)

	for {
		select {
		case r, ok := <-resultCh:
			if !ok {
				return replies, errs
			}
			if r.err != nil {
				errs = append(errs, r.err)
			} else {
				replies = append(replies, r.reply)
			}
		case <-ctx.Done():
			return replies, append(errs, fmt.Errorf("request timeout after %v", timeout))
		}
	}
}

func (b *EventBus) Close() {
	b.cancel()
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subscribers = make(map[string][]*subscription)
}

func (b *EventBus) copyHandlers(topic string) ([]HandlerFunc, DroppedEventFunc) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	subs := b.subscribers[topic]
	handlers := make([]HandlerFunc, len(subs))
	for i, sub := range subs {
		handlers[i] = sub.handler
	}
	return handlers, b.droppedHandler
}
