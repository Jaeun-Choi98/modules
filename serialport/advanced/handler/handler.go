package serialhdr

import (
	"errors"
	"log"
	"sync"

	serialmd "github.com/Jaeun-Choi98/modules/serialport/advanced/model"
)

var (
	errNotExistsHandlerType = errors.New("not exsits registered handler about packet.GetPacketId")
	errNilHandler           = errors.New("nil handler")
	errNotExistsMsg         = errors.New("not exists parsed massage")
)

type MsgType interface {
	byte | int8 | int16 | uint16 | int | int32 | uint32 | int64 | uint64 | float32 | float64 | string
}

type HandlerInterface interface {
	handle(c *serialmd.HandleContext) error
}

type HandlerFunc func(c *serialmd.HandleContext) error

func (f HandlerFunc) handle(c *serialmd.HandleContext) error {
	return f(c)
}

type ManagerInterface interface {
	HandleMessage(c *serialmd.ConnContext) error
	HandleMessageSync(c *serialmd.ConnContext) error
	RegisterHandle(packetId any, handle HandlerFunc)
	RegisterHandler(packetId any, handler HandlerInterface)
}

type Manager[T MsgType] struct {
	handlers map[T]HandlerInterface
	mu       sync.RWMutex
}

func NewHandlerManager[T MsgType]() *Manager[T] {
	return &Manager[T]{
		handlers: make(map[T]HandlerInterface),
	}
}

func (h *Manager[T]) HandleMessage(c *serialmd.ConnContext) error {

	msg, ok := c.GetParsedMsg()
	if !ok {
		return errNotExistsMsg
	}

	h.mu.RLock()
	handler, exists := h.handlers[msg.GetPacketId().(T)]
	h.mu.RUnlock()

	if !exists {
		return errNotExistsHandlerType
	}

	if handler == nil {
		return errNilHandler
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Println("panic in handling packet")
			}
		}()
		if err := handler.handle(c.NewHandleContext(msg)); err != nil {
			log.Printf("error in handling packet: %+v", err)
		}
	}()
	return nil
}

func (h *Manager[T]) HandleMessageSync(c *serialmd.ConnContext) error {
	defer func() {
		if r := recover(); r != nil {
			log.Println("panic in handling packet")
		}
	}()

	msg, ok := c.GetParsedMsg()
	if !ok {
		return errNotExistsMsg
	}

	h.mu.RLock()
	handler, exists := h.handlers[msg.GetPacketId().(T)]
	h.mu.RUnlock()

	if !exists {
		return errNotExistsHandlerType
	}

	if handler == nil {
		return errNilHandler
	}

	if err := handler.handle(c.NewHandleContext(msg)); err != nil {
		log.Printf("error in handling packet: %+v", err)
	}

	return nil
}

func (h *Manager[T]) RegisterHandle(packetId any, handle HandlerFunc) {
	h.handlers[packetId.(T)] = handle
}

func (h *Manager[T]) RegisterHandler(packetId any, handler HandlerInterface) {
	h.handlers[packetId.(T)] = handler
}
