package eventbus

import (
	"context"
	"sync"
	"time"
)

/**
 * EventBus provides a communication mechanism between different parts of the application.
 * It implements a publish-subscribe pattern.
 *
 * -> deprecated this package. use github.com/Jaeun-Choi98/eventbus (same source code).
 */

type Topic interface {
	GetEventType() any
}

type Event interface {
	GetEventId() uint32
}

type EventBus struct {
	subscribers map[any][]chan Event

	processedMsgs   map[uint32]time.Time
	cleanupDuration time.Duration

	mu sync.RWMutex

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func NewEventBus(pctx context.Context, duration time.Duration) *EventBus {
	ctx, cancel := context.WithCancel(pctx)
	eb := &EventBus{
		subscribers:     make(map[any][]chan Event),
		processedMsgs:   make(map[uint32]time.Time),
		cleanupDuration: duration,
		ctx:             ctx,
		cancel:          cancel,
	}

	eb.wg.Add(1)
	go eb.cleanupOldMessages()

	return eb
}

func (b *EventBus) cleanupOldMessages() {

	cleanupTicker := time.NewTicker(b.cleanupDuration)

	defer func() {
		b.wg.Done()
		cleanupTicker.Stop()
	}()

	for {
		select {
		case <-cleanupTicker.C:
			b.mu.Lock()
			cutoff := time.Now().Add(-b.cleanupDuration)
			for id, timestamp := range b.processedMsgs {
				if timestamp.Before(cutoff) {
					delete(b.processedMsgs, id)
				}
			}
			b.mu.Unlock()

		case <-b.ctx.Done():
			return
		}
	}
}

func (b *EventBus) Subscribe(topic Topic, cap int) chan Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan Event, cap)
	b.subscribers[topic.GetEventType()] = append(b.subscribers[topic.GetEventType()], ch)
	return ch
}

func (b *EventBus) Unsubscribe(topic Topic, ch chan Event) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if subs, exsits := b.subscribers[topic.GetEventType()]; exsits {
		for i, sub := range subs {
			if sub == ch {
				close(ch)
				b.subscribers[topic.GetEventType()] = append(subs[:i], subs[i+1:]...)
				break
			}
		}
	}
}

func (b *EventBus) Publish(topic Topic, event Event) {
	b.mu.RLock()
	if _, exists := b.processedMsgs[event.GetEventId()]; exists {
		return
	}
	b.mu.RUnlock()

	b.mu.Lock()
	b.processedMsgs[event.GetEventId()] = time.Now()
	b.mu.Unlock()

	b.mu.RLock()
	if subs, found := b.subscribers[topic.GetEventType()]; found {
		for _, ch := range subs {
			select {
			case ch <- event:
			default:

			}
		}
	}
	b.mu.RUnlock()
}

func (b *EventBus) Close() {
	b.cancel()
	b.wg.Wait()

	b.mu.Lock()
	defer b.mu.Unlock()

	for topicType, subs := range b.subscribers {
		for _, ch := range subs {
			for len(ch) > 0 {
				<-ch
			}
			close(ch)
		}
		delete(b.subscribers, topicType)
	}
}
