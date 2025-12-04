package handler

import (
	"log"
	"sync"

	tcpmd "github.com/Jaeun-Choi98/modules/tcpnet/server/model"
)

type HandlerInterface interface {
	handle(c *tcpmd.ReqContext) error
}

type HandlerFunc func(c *tcpmd.ReqContext) error

func (f HandlerFunc) handle(c *tcpmd.ReqContext) error {
	return f(c)
}

type ManagerInterface interface {
	HandleMessage(c *tcpmd.ReqContext, packetId any) error
	RegisterHandle(packetId any, handle HandlerFunc)
	RegisterHandler(packetId any, handler HandlerInterface)
}

type Manager[T MsgType] struct {
	handlers map[T]HandlerInterface
	mu       sync.RWMutex
}

func NewV1[T MsgType]() *Manager[T] {
	return &Manager[T]{
		handlers: make(map[T]HandlerInterface),
	}
}

func (h *Manager[T]) HandleMessage(c *tcpmd.ReqContext, packetId any) error {

	h.mu.RLock()
	handler, exists := h.handlers[packetId.(T)]
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
		if err := handler.handle(c); err != nil {
			log.Printf("error in handling packet: %+v", err)
		}
	}()
	return nil
}

func (h *Manager[T]) RegisterHandle(packetId any, handle HandlerFunc) {
	h.handlers[packetId.(T)] = handle
}

func (h *Manager[T]) RegisterHandler(packetId any, handler HandlerInterface) {
	h.handlers[packetId.(T)] = handler
}
