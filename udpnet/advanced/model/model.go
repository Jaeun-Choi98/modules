package udpmd

import (
	"context"
	"errors"
	"net"
	"sync"
)

var (
	ErrContextClosed    = errors.New("closed session context")
	ErrNoPendingRequest = errors.New("no pending request for key")
	ErrResponseBusy     = errors.New("response channel is full or has no receiver")
)

type Response interface {
	GetPayload() any
	GetErr() error
}

type GenericResponse[T any] struct {
	Payload T
	Err     error
}

func (r *GenericResponse[T]) GetPayload() any { return r.Payload }
func (r *GenericResponse[T]) GetErr() error   { return r.Err }

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

type PendingRequests struct {
	mu       sync.RWMutex
	channels map[any]chan Response
}

func (m *PendingRequests) Get(key any) (chan Response, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ch, exists := m.channels[key]
	return ch, exists
}

func (m *PendingRequests) Register(key any, ch chan Response) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.channels[key] = ch
}

// 대기 중인 요청에 응답을 전달함. Cancel/CancelAll과 동시에 호출되어도 안전함.
// Get으로 얻은 채널에 직접 보내면 Cancel과 경합 시 panic이 발생할 수 있으므로 Resolve 사용을 권장.
// 수신자가 없거나 버퍼가 가득 찬 경우 블로킹하지 않고 ErrResponseBusy를 반환함.
func (m *PendingRequests) Resolve(key any, resp Response) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ch, exists := m.channels[key]
	if !exists {
		return ErrNoPendingRequest
	}
	select {
	case ch <- resp:
		return nil
	default:
		return ErrResponseBusy
	}
}

func (m *PendingRequests) Remove(key any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.channels, key)
}

func (m *PendingRequests) Cancel(key any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ch, exists := m.channels[key]; exists {
		close(ch)
		delete(m.channels, key)
	}
}

func (m *PendingRequests) CancelAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, ch := range m.channels {
		close(ch)
		delete(m.channels, key)
	}
}

// 송신 인터페이스. udpnet.Endpoint가 구현하며, 송신 소켓 분리(SendBindAddr)가 적용됨.
type Sender interface {
	SendTo(data []byte, addr *net.UDPAddr) error
}

// SessionContext는 *net.UDPAddr 기준으로 관리되는 per-peer 세션.
// sender와 addr을 함께 보유해 핸들러에서 응답 전송 가능.
type SessionContext struct {
	context  context.Context
	sender   Sender
	addr     *net.UDPAddr
	parseMsg chan ParseMsg
	pending  *PendingRequests

	// parseMsg 채널은 닫지 않고 done으로 종료를 알림 (send on closed channel 방지)
	mu     sync.RWMutex
	closed bool
	done   chan struct{}
}

func NewSessionContext(ctx context.Context, sender Sender, addr *net.UDPAddr, msgChannelSize int) *SessionContext {
	return &SessionContext{
		context:  ctx,
		sender:   sender,
		addr:     addr,
		parseMsg: make(chan ParseMsg, msgChannelSize),
		done:     make(chan struct{}),
		pending: &PendingRequests{
			channels: make(map[any]chan Response),
		},
	}
}

func (s *SessionContext) GetContext() context.Context { return s.context }
func (s *SessionContext) GetAddr() *net.UDPAddr       { return s.addr }

func (s *SessionContext) GetParsedMsg() (ParseMsg, bool) {
	select {
	case msg := <-s.parseMsg:
		return msg, true
	default:
		return nil, false
	}
}

func (s *SessionContext) SetParsedMsg(msg ParseMsg) error {
	select {
	case <-s.done:
		return ErrContextClosed
	default:
	}

	select {
	case s.parseMsg <- msg:
		return nil
	case <-s.done:
		return ErrContextClosed
	case <-s.context.Done():
		return s.context.Err()
	}
}

func (s *SessionContext) GetPending() *PendingRequests {
	return s.pending
}

func (s *SessionContext) NewHandleContext(msg ParseMsg) *HandleContext {
	return &HandleContext{
		context:  s.context,
		sender:   s.sender,
		addr:     s.addr,
		parseMsg: msg,
		pending:  s.pending,
	}
}

func (s *SessionContext) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	close(s.done)
	s.mu.Unlock()

	s.pending.CancelAll()
	return nil
}

type HandleContext struct {
	context  context.Context
	sender   Sender
	addr     *net.UDPAddr
	parseMsg ParseMsg
	pending  *PendingRequests
}

func (c *HandleContext) GetContext() context.Context  { return c.context }
func (c *HandleContext) GetParseMsg() ParseMsg        { return c.parseMsg }
func (c *HandleContext) GetPending() *PendingRequests { return c.pending }
func (c *HandleContext) GetAddr() *net.UDPAddr        { return c.addr }

// 이 패킷의 출발지 주소(GetAddr)로 전송.
func (c *HandleContext) Send(data []byte) error {
	return c.SendTo(data, c.addr)
}

// 임의 주소로 전송. 상대가 송신 포트와 다른 포트에서 수신할 때 사용.
func (c *HandleContext) SendTo(data []byte, addr *net.UDPAddr) error {
	if c.sender == nil {
		return errors.New("nil sender")
	}
	return c.sender.SendTo(data, addr)
}
