package handler

import (
	"errors"
	"fmt"
	"log"
	"runtime/debug"
	"sync"

	udpmd "github.com/Jaeun-Choi98/modules/udpnet/advanced/model"
)

var (
	errNotExistsHandlerType = errors.New("not exists registered handler about packet.GetPacketId")
	errNilHandler           = errors.New("nil handler")
	errNotExistsMsg         = errors.New("not exists parsed message")
	errInvalidPacketIdType  = errors.New("invalid packet id type")
)

type MsgType interface {
	byte | int8 | int16 | uint16 | int | int32 | uint32 | int64 | uint64 | float32 | float64 | string
}

type HandlerInterface interface {
	handle(c *udpmd.HandleContext) error
}

type HandlerFunc func(c *udpmd.HandleContext) error

func (f HandlerFunc) handle(c *udpmd.HandleContext) error {
	return f(c)
}

type ManagerInterface interface {
	HandleMessage(c *udpmd.SessionContext) error
	HandleMessageSync(c *udpmd.SessionContext) error
	RegisterHandle(packetId any, handle HandlerFunc)
	RegisterHandler(packetId any, handler HandlerInterface)
}

type Manager[T MsgType] struct {
	handlers map[T]HandlerInterface
	mu       sync.RWMutex
}

func New[T MsgType]() *Manager[T] {
	return &Manager[T]{
		handlers: make(map[T]HandlerInterface),
	}
}

func (h *Manager[T]) lookup(msg udpmd.ParseMsg) (HandlerInterface, error) {
	packetId, ok := msg.GetPacketId().(T)
	if !ok {
		var zero T
		return nil, fmt.Errorf("%w: got %T, want %T", errInvalidPacketIdType, msg.GetPacketId(), zero)
	}

	h.mu.RLock()
	handler, exists := h.handlers[packetId]
	h.mu.RUnlock()

	if !exists {
		return nil, errNotExistsHandlerType
	}
	if handler == nil {
		return nil, errNilHandler
	}
	return handler, nil
}

func (h *Manager[T]) HandleMessage(c *udpmd.SessionContext) error {
	msg, ok := c.GetParsedMsg()
	if !ok {
		return errNotExistsMsg
	}

	handler, err := h.lookup(msg)
	if err != nil {
		return err
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("panic in handling packet: %v\n%s", r, debug.Stack())
			}
		}()
		if err := handler.handle(c.NewHandleContext(msg)); err != nil {
			log.Printf("error in handling packet: %+v", err)
		}
	}()
	return nil
}

// 핸들러에서 panic이 발생하면 error로 변환해 반환함.
func (h *Manager[T]) HandleMessageSync(c *udpmd.SessionContext) (rstErr error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("panic in handling packet: %v\n%s", r, debug.Stack())
			rstErr = fmt.Errorf("panic in handling packet: %v", r)
		}
	}()

	msg, ok := c.GetParsedMsg()
	if !ok {
		return errNotExistsMsg
	}

	handler, err := h.lookup(msg)
	if err != nil {
		return err
	}

	if err := handler.handle(c.NewHandleContext(msg)); err != nil {
		log.Printf("error in handling packet: %+v", err)
	}
	return nil
}

// packetId 타입이 Manager의 T와 다르면 등록 시점에 panic (http.HandleFunc와 같은 프로그래머 오류 처리).
func (h *Manager[T]) RegisterHandle(packetId any, handle HandlerFunc) {
	h.register(packetId, handle)
}

func (h *Manager[T]) RegisterHandler(packetId any, handler HandlerInterface) {
	h.register(packetId, handler)
}

func (h *Manager[T]) register(packetId any, handler HandlerInterface) {
	id, ok := packetId.(T)
	if !ok {
		var zero T
		panic(fmt.Sprintf("handler: invalid packet id type %T, want %T", packetId, zero))
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	h.handlers[id] = handler
}
