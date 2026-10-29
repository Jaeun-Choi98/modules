package tcpmd

import (
	"context"
	"errors"
	"sync"
)

var (
	ErrContextClosed    = errors.New("closed conn context")
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

func (r *GenericResponse[T]) GetPayload() any {
	return r.Payload
}

func (r *GenericResponse[T]) GetErr() error {
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

// 채널을 닫지 않고 맵에서 정리
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

type ConnContext struct {
	context  context.Context
	parseMsg chan ParseMsg
	pending  *PendingRequests

	// parseMsg 채널은 닫지 않고 done으로 종료를 알림 (send on closed channel 방지)
	mu     sync.RWMutex
	closed bool
	done   chan struct{}
}

func NewConnContext(ctx context.Context, msgChannelSize int) *ConnContext {
	return &ConnContext{
		context:  ctx,
		parseMsg: make(chan ParseMsg, msgChannelSize),
		done:     make(chan struct{}),
		pending: &PendingRequests{
			channels: make(map[any]chan Response),
		},
	}
}

func (c *ConnContext) GetContext() context.Context {
	return c.context
}

func (c *ConnContext) GetParsedMsg() (ParseMsg, bool) {
	select {
	case msg := <-c.parseMsg:
		return msg, true
	default:
		return nil, false
	}
}

func (c *ConnContext) SetParsedMsg(msg ParseMsg) error {
	select {
	case <-c.done:
		return ErrContextClosed
	default:
	}

	select {
	case c.parseMsg <- msg:
		return nil
	case <-c.done:
		return ErrContextClosed
	case <-c.context.Done():
		return c.context.Err()
	}
}

func (c *ConnContext) GetPending() *PendingRequests {
	return c.pending
}

func (c *ConnContext) NewHandleContext(msg ParseMsg) *HandleContext {
	return &HandleContext{
		context:  c.context,
		parseMsg: msg,
		pending:  c.pending,
	}
}

func (c *ConnContext) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	close(c.done)
	c.mu.Unlock()

	c.pending.CancelAll()
	return nil
}

type HandleContext struct {
	context  context.Context
	parseMsg ParseMsg
	pending  *PendingRequests
}

func (c *HandleContext) GetContext() context.Context {
	return c.context
}

func (c *HandleContext) GetParseMsg() ParseMsg {
	return c.parseMsg
}

func (c *HandleContext) GetPending() *PendingRequests {
	return c.pending
}
