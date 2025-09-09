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
	handle(packet model.Packet, replyCh map[model.ReplyCode]chan model.Reply) error
}

type TypeHandlerFunc func(packet model.Packet, replyCh map[model.ReplyCode]chan model.Reply) error

func (f TypeHandlerFunc) handle(packet model.Packet, replyCh map[model.ReplyCode]chan model.Reply) error {
	return f(packet, replyCh)
}

type HandlerManagerInterface interface {
	HandleMessage(packet model.Packet, replyCh map[model.ReplyCode]chan model.Reply) error
	RegisterHandle(packetId any, handle TypeHandlerFunc)
	RegisterHandler(packetId any, handler TypeHandlerInterface)
}

type HandlerManager struct {
	handlers map[any]TypeHandlerInterface
	mu       sync.RWMutex
}

func (h *HandlerManager) HandleMessage(packet model.Packet, replyCh map[model.ReplyCode]chan model.Reply) error {

	h.mu.RLock()
	handler, exists := h.handlers[packet.GetPacketId()]
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
		if err := handler.handle(packet, replyCh); err != nil {
			log.Printf("error in handling packet: %+v", err)
		}
	}()
	return nil
}
