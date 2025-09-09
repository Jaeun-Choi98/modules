package handler

import (
	"errors"
	"log"
	"sync"

	"github.com/Jaeun-Choi98/modules/tcpnet/server/model"
)

var (
	errNotExistsHandlerType = errors.New("not exsits registered handler about packet.GetPacketId")
)

type TypeHandlerInterface interface {
	handle(parseMsg model.ParseMsg, replyCh map[model.ReplyCode]chan model.Reply) error
}

type TypeHandlerFunc func(parseMsg model.ParseMsg, replyCh map[model.ReplyCode]chan model.Reply) error

func (f TypeHandlerFunc) handle(parseMsg model.ParseMsg, replyCh map[model.ReplyCode]chan model.Reply) error {
	return f(parseMsg, replyCh)
}

type HandlerManagerInterface interface {
	HandleMessage(parseMsg model.ParseMsg, replyCh map[model.ReplyCode]chan model.Reply) error
	RegisterHandle(packetId any, handle TypeHandlerFunc)
	RegisterHandler(packetId any, handler TypeHandlerInterface)
}

type HandlerManager struct {
	handlers map[any]TypeHandlerInterface
	mu       sync.RWMutex
}

func (h *HandlerManager) HandleMessage(parseMsg model.ParseMsg, replyCh map[model.ReplyCode]chan model.Reply) error {

	h.mu.RLock()
	handler, exists := h.handlers[parseMsg.GetPacketId()]
	h.mu.RUnlock()

	if !exists {
		return errNotExistsHandlerType
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

func (h *HandlerManager) RegisterHandle(packetId any, handle TypeHandlerFunc) {
	h.handlers[packetId] = handle
}

func (h *HandlerManager) RegisterHandler(packetId any, handler TypeHandlerInterface) {
	h.handlers[packetId] = handler
}
