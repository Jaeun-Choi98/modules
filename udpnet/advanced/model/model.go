package udpmd

import (
	"context"
	"fmt"
	"net"
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

func (r *GenericReply[T]) GetPayload() any { return r.Payload }
func (r *GenericReply[T]) GetErr() error   { return r.Err }

type ParseMsg interface {
	GetAddr() *net.UDPAddr
	GetPacketId() any
	GetPacket() any
}

type GenericParseMsg[T, K any] struct {
	addr     *net.UDPAddr
	packetId T
	packet   K
}

func NewGenericParseMsg[T, K any](addr *net.UDPAddr, packetId T, packet K) *GenericParseMsg[T, K] {
	return &GenericParseMsg[T, K]{
		addr:     addr,
		packetId: packetId,
		packet:   packet,
	}
}

func (p *GenericParseMsg[T, K]) GetAddr() *net.UDPAddr { return p.addr }
func (p *GenericParseMsg[T, K]) GetPacketId() any      { return p.packetId }
func (p *GenericParseMsg[T, K]) GetPacket() any        { return p.packet }

type ReplyChannelManager struct {
	mu       sync.RWMutex
	channels map[any]chan Reply
}

func (m *ReplyChannelManager) Get(key any) (chan Reply, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ch, exists := m.channels[key]
	return ch, exists
}

func (m *ReplyChannelManager) Set(key any, ch chan Reply) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.channels[key] = ch
}

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

// SessionContext는 *net.UDPAddr 기준으로 관리되는 per-client 세션.
// TCP의 ConnContext에 대응하며, conn과 addr을 함께 보유해 핸들러에서 응답 전송 가능.
type SessionContext struct {
	context      context.Context
	conn         *net.UDPConn
	addr         *net.UDPAddr
	parseMsg     chan ParseMsg
	replyManager *ReplyChannelManager
	mu           sync.RWMutex
	closed       bool
}

func NewSessionContext(ctx context.Context, conn *net.UDPConn, addr *net.UDPAddr, msgChannelSize int) *SessionContext {
	return &SessionContext{
		context: ctx,
		conn:    conn,
		addr:    addr,
		parseMsg: make(chan ParseMsg, msgChannelSize),
		replyManager: &ReplyChannelManager{
			channels: make(map[any]chan Reply),
		},
	}
}

func (s *SessionContext) GetContext() context.Context { return s.context }
func (s *SessionContext) GetAddr() *net.UDPAddr       { return s.addr }
func (s *SessionContext) GetConn() *net.UDPConn       { return s.conn }

func (s *SessionContext) GetParsedMsg() (ParseMsg, bool) {
	select {
	case msg, ok := <-s.parseMsg:
		if !ok {
			return nil, false
		}
		return msg, true
	default:
		return nil, false
	}
}

func (s *SessionContext) SetParsedMsg(msg ParseMsg) error {
	s.mu.RLock()
	ch := s.parseMsg
	s.mu.RUnlock()

	if ch == nil {
		return fmt.Errorf("cancelled session context")
	}

	select {
	case s.parseMsg <- msg:
	case <-s.context.Done():
	}
	return nil
}

func (s *SessionContext) GetReplyChannel() *ReplyChannelManager {
	return s.replyManager
}

func (s *SessionContext) NewHandleContext(msg ParseMsg) *HandleContext {
	return &HandleContext{
		context:      s.context,
		conn:         s.conn,
		addr:         s.addr,
		parseMsg:     msg,
		replyManager: s.replyManager,
	}
}

func (s *SessionContext) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	parseChan := s.parseMsg
	s.parseMsg = nil
	s.mu.Unlock()

	s.replyManager.CloseAll()
	close(parseChan)
	return nil
}

type HandleContext struct {
	context      context.Context
	conn         *net.UDPConn
	addr         *net.UDPAddr
	parseMsg     ParseMsg
	replyManager *ReplyChannelManager
}

func (c *HandleContext) GetContext() context.Context        { return c.context }
func (c *HandleContext) GetParseMsg() ParseMsg              { return c.parseMsg }
func (c *HandleContext) GetReplyChannel() *ReplyChannelManager { return c.replyManager }
func (c *HandleContext) GetAddr() *net.UDPAddr              { return c.addr }

func (c *HandleContext) Send(data []byte) error {
	_, err := c.conn.WriteToUDP(data, c.addr)
	return err
}
