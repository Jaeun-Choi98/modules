package handler

import (
	"errors"
	"log"
	"sync"

	tcpmd "github.com/Jaeun-Choi98/modules/tcpnet/server/model"
)

var (
	errNotExistsHandlerType = errors.New("not exsits registered handler about packet.GetPacketId")
	ErrNilHandler           = errors.New("nil handler")
)

type TypeHandlerInterface interface {
	handle(parseMsg tcpmd.ParseMsg, replyCh map[any]chan tcpmd.Reply) error
}

type TypeHandlerFunc func(parseMsg tcpmd.ParseMsg, replyCh map[any]chan tcpmd.Reply) error

func (f TypeHandlerFunc) handle(parseMsg tcpmd.ParseMsg, replyCh map[any]chan tcpmd.Reply) error {
	return f(parseMsg, replyCh)
}

type HandlerManagerInterface interface {
	HandleMessage(parseMsg tcpmd.ParseMsg, replyCh map[any]chan tcpmd.Reply) error
	RegisterHandle(packetId any, handle TypeHandlerFunc)
	RegisterHandler(packetId any, handler TypeHandlerInterface)
}

type MsgType interface {
	byte | int8 | int16 | uint16 | int | int32 | uint32 | int64 | uint64 | float32 | float64 | string
}

type HandlerManager[T MsgType] struct {
	handlers map[T]TypeHandlerInterface
	mu       sync.RWMutex
}

func New[T MsgType]() *HandlerManager[T] {
	return &HandlerManager[T]{
		handlers: make(map[T]TypeHandlerInterface),
	}
}

func (h *HandlerManager[T]) HandleMessage(parseMsg tcpmd.ParseMsg, replyCh map[any]chan tcpmd.Reply) error {

	h.mu.RLock()
	handler, exists := h.handlers[parseMsg.GetPacketId().(T)]
	h.mu.RUnlock()

	if !exists {
		return errNotExistsHandlerType
	}

	if handler == nil {
		return ErrNilHandler
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Println("panic in handling packet")
			}
		}()
		if err := handler.handle(parseMsg, replyCh); err != nil {
			log.Printf("error in handling packet: %+v", err)
		}
	}()
	return nil
}

func (h *HandlerManager[T]) RegisterHandle(packetId any, handle TypeHandlerFunc) {
	h.handlers[packetId.(T)] = handle
}

func (h *HandlerManager[T]) RegisterHandler(packetId any, handler TypeHandlerInterface) {
	h.handlers[packetId.(T)] = handler
}
