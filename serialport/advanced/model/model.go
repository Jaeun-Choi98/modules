package serialmd

import (
	"context"
	"fmt"
	"sync"
)

type Reply interface {
	GetPayload() any
	GetErr() error
}

type GenericReply[T any] struct {
	Payload T
	Err     error
}

func (r *GenericReply[T]) GetPayload() any {
	return r.Payload
}

func (r *GenericReply[T]) GetErr() error {
	return r.Err
}

type ParseMsg interface {
	SetClientId(clientId uint32)
	GetClientId() uint32
	GetPacketId() any
	GetPacket() any
}

type GenericParseMsg[T, K any] struct {
	ClientId uint32
	packetId T
	packet   K
}

func (p *GenericParseMsg[T, K]) AddPacket(packetId T, packet K) *GenericParseMsg[T, K] {
	p.packet = packet
	p.packetId = packetId
	return p
}

func (p *GenericParseMsg[T, K]) SetClientId(clientId uint32) {
	p.ClientId = clientId
}

func (p *GenericParseMsg[T, K]) GetClientId() uint32 {
	return p.ClientId
}

func (p *GenericParseMsg[T, K]) GetPacketId() any {
	return p.packetId
}

func (p *GenericParseMsg[T, K]) GetPacket() any {
	return p.packet
}

type ReplyChannelManager struct {
	mu       sync.RWMutex
	channels map[any]chan Reply
}

func (m *ReplyChannelManager) Get(key any) (chan Reply, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	reply, exists := m.channels[key]
	return reply, exists
}

func (m *ReplyChannelManager) Set(key any, ch chan Reply) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.channels[key] = ch
}

// 채널을 닫지 않고 맵에서 정리
func (m *ReplyChannelManager) Del(key any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.channels, key)
}

func (m *ReplyChannelManager) Close(key any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ch, exists := m.channels[key]; exists {
		close(ch)
		delete(m.channels, key)
	}
}

func (m *ReplyChannelManager) CloseAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for key, ch := range m.channels {
		close(ch)
		delete(m.channels, key)
	}
}

type ConnContext struct {
	context      context.Context
	parseMsg     chan ParseMsg
	replyManager *ReplyChannelManager

	// for parseMsg
	mu     sync.RWMutex
	closed bool
}

func NewConnContext(ctx context.Context, msgChannelSize int) *ConnContext {
	return &ConnContext{
		context:  ctx,
		parseMsg: make(chan ParseMsg, msgChannelSize),
		replyManager: &ReplyChannelManager{
			channels: make(map[any]chan Reply),
		},
	}
}

func (c *ConnContext) GetContext() context.Context {
	return c.context
}

func (c *ConnContext) GetParsedMsg() (ParseMsg, bool) {
	select {
	case msg, ok := <-c.parseMsg:
		if !ok {
			return nil, false
		}
		return msg, true
	default:
		return nil, false
	}
}

func (c *ConnContext) SetParsedMsg(msg ParseMsg) error {
	c.mu.RLock()
	ch := c.parseMsg
	c.mu.RUnlock()

	if ch == nil {
		return fmt.Errorf("cancelled client context")
	}

	select {
	case c.parseMsg <- msg:
	case <-c.context.Done():
		return nil
	}

	return nil
}

func (c *ConnContext) GetReplyChannel() *ReplyChannelManager {
	return c.replyManager
}

func (c *ConnContext) NewHandleContext(msg ParseMsg) *HandleContext {
	return &HandleContext{
		context:      c.context,
		parseMsg:     msg,
		replyManager: c.replyManager,
	}
}

func (c *ConnContext) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	parseChan := c.parseMsg
	c.parseMsg = nil
	c.mu.Unlock()

	c.replyManager.CloseAll()
	close(parseChan)
	return nil
}

type HandleContext struct {
	context      context.Context
	parseMsg     ParseMsg
	replyManager *ReplyChannelManager
}

func (c *HandleContext) GetContext() context.Context {
	return c.context
}

func (c *HandleContext) GetParseMsg() ParseMsg {
	return c.parseMsg
}

func (c *HandleContext) GetReplyChannel() *ReplyChannelManager {
	return c.replyManager
}
