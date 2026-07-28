package client

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"time"
)

type ClientBase struct {
	conn *net.UDPConn

	ip   string
	port string

	handleConnectFunc func(conn *net.UDPConn)

	wg     sync.WaitGroup
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.RWMutex

	isConnected bool
	readTimeout time.Duration
	heartbeat   time.Duration
}

func NewClientBase(ctx context.Context, readTimeout, heartbeat time.Duration) (*ClientBase, error) {
	ctx, cancel := context.WithCancel(ctx)
	return &ClientBase{
		readTimeout: readTimeout,
		heartbeat:   heartbeat,
		ctx:         ctx,
		cancel:      cancel,
	}, nil
}

func (c *ClientBase) SetConnectionState(state bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.isConnected = state
}

func (c *ClientBase) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.isConnected
}

func (c *ClientBase) SetIpAndPort(ip, port string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ip = ip
	c.port = port
}

func (c *ClientBase) SetHandleConnectFunc(f func(conn *net.UDPConn)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handleConnectFunc = f
}

func (c *ClientBase) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ip == "" || c.port == "" {
		return fmt.Errorf("need ip and port. call SetIpAndPort")
	}

	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%s", c.ip, c.port))
	if err != nil {
		return err
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		log.Println(err)
		return err
	}

	if c.conn != nil {
		c.conn.Close()
	}
	c.conn = conn
	c.isConnected = true
	return nil
}

func (c *ClientBase) Start() error {
	if c.handleConnectFunc == nil {
		return fmt.Errorf("handle connect func is nil")
	}

	if err := c.Connect(); err != nil {
		return err
	}

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.handleConnectFunc(c.conn)
	}()
	return nil
}

func (c *ClientBase) SendMessage(msg []byte) error {
	c.mu.RLock()
	conn := c.conn
	c.mu.RUnlock()

	if conn == nil {
		return fmt.Errorf("not connected")
	}

	_, err := conn.Write(msg)
	return err
}

func (c *ClientBase) Shutdown() error {
	c.cancel()
	c.mu.Lock()
	c.isConnected = false
	var err error
	if c.conn != nil {
		err = c.conn.Close()
	}
	c.mu.Unlock()
	c.wg.Wait()
	return err
}

// pingData: 헬스체크용으로 서버에 주기적으로 전송할 데이터. nil이면 전송 생략.
func (c *ClientBase) StartUDPClientHeartbeat(pingData []byte) {
	c.wg.Add(1)
	go func() {
		heartbeat := time.NewTicker(c.heartbeat)
		defer func() {
			heartbeat.Stop()
			c.wg.Done()
		}()

		for {
			select {
			case <-heartbeat.C:
				if !c.IsConnected() {
					log.Println("[UDP Client Heartbeat] Not connected, attempting to reconnect...")
					if err := c.Connect(); err != nil {
						log.Printf("[UDP Client Heartbeat] Failed to reconnect: %v", err)
					}
					continue
				}
				if len(pingData) > 0 {
					if err := c.SendMessage(pingData); err != nil {
						log.Printf("[UDP Client Heartbeat] Failed to send ping: %v", err)
						c.SetConnectionState(false)
					}
				}
			case <-c.ctx.Done():
				log.Println("[UDP Client Heartbeat] goroutine terminated")
				return
			}
		}
	}()
}
