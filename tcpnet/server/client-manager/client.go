package client

import (
	"context"
	"net"
	"sync"
	"time"
)

type Client interface {
	Close() error
	ProcessingData()

	SetConn(conn net.Conn)
	SetAuth(b bool)
	SetClientId(id uint32)
	GetClientId() uint32
}

type DefaultClient struct {
	ClientId uint32

	Conn net.Conn

	SeqNum uint16

	IsAuth bool

	// 구현을 위해 parser, handler, replyCh 객체 필요. <- 추가 구현

	Ctx    context.Context
	Cancel context.CancelFunc

	mu sync.Mutex
}

func NewDefaultClient(clientId uint32) *DefaultClient {
	ctx, cancel := context.WithCancel(context.Background())

	return &DefaultClient{
		ClientId: clientId,
		Ctx:      ctx,
		Cancel:   cancel,
	}
}

func (d *DefaultClient) SetConn(conn net.Conn) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.Conn = conn
}

func (d *DefaultClient) SetAuth(b bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.IsAuth = b
}

func (d *DefaultClient) SetClientId(id uint32) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.ClientId = id
}

func (d *DefaultClient) SetSequenceNum(seq uint16) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.SeqNum = seq
}

func (d *DefaultClient) GetSequenceNum() uint16 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.SeqNum
}

func (d *DefaultClient) GetClientId() uint32 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.ClientId
}

// default timeout: 5 sec
func (d *DefaultClient) SendMessage(msg []byte, timeout time.Duration) error {
	if timeout == 0 {
		timeout = 5 * time.Second
	}

	d.Conn.SetWriteDeadline(time.Now().Add(timeout))
	defer d.Conn.SetWriteDeadline(time.Time{})

	written := 0
	for written < len(msg) {
		n, err := d.Conn.Write(msg[written:])
		if err != nil {
			return err
		}
		written += n
	}
	return nil
}
