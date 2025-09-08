package client

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"
)

type ClientBase struct {
	conn net.Conn

	ip   string
	port string

	rxAndParsingFunc func(conn net.Conn) (any, error)
	parsingErrCnt    int

	handlePacket func(any) error

	wg     sync.WaitGroup
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.RWMutex

	isConnected    bool
	connectTimeout time.Duration
	heartbeat      time.Duration
}

func NewClientBase(ctx context.Context, connectTimeout, heartbeat time.Duration) (*ClientBase, error) {
	ctx, cancel := context.WithCancel(ctx)
	return &ClientBase{
		//reader:      bufio.NewReader(os.Stdin),
		heartbeat:      heartbeat,
		connectTimeout: connectTimeout,
		ctx:            ctx,
		cancel:         cancel,
	}, nil
}

func (c *ClientBase) SetConnectionState(state bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.isConnected = state
}

func (c *ClientBase) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.Unlock()
	return c.isConnected
}

func (c *ClientBase) SetIpAndPort(ip, port string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ip = ip
	c.port = port
}

func (c *ClientBase) SetRxAndParsingFunc(f func(conn net.Conn) (any, error)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rxAndParsingFunc = f
}

func (c *ClientBase) SetHandlePacket(f func(any) error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handlePacket = f
}

func (c *ClientBase) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ip == "" || c.port == "" {
		return fmt.Errorf("need ip and port. call SetIpAndPort")
	}

	conn, err := net.DialTimeout("tcp", fmt.Sprintf(`%s:%s`, c.ip, c.port), c.connectTimeout)
	if err != nil {
		log.Println(err)
		return err
	}

	c.conn = conn
	c.isConnected = true
	return nil
}

func (c *ClientBase) Start() error {
	if c.rxAndParsingFunc == nil {
		return fmt.Errorf("parsing func is nil")
	}

	if c.handlePacket == nil {
		return fmt.Errorf("handle packet func is nil")
	}

	// 초기 연결
	if err := c.Connect(); err != nil {
		return err
	}

	c.wg.Add(1)
	return c.WaitForReceiveMessage()
}

func (c *ClientBase) WaitForReceiveMessage() error {
	defer c.wg.Done()
	for {

		select {
		case <-c.ctx.Done():
			return nil
		default:
		}

		if !c.IsConnected() {
			time.Sleep(1 * time.Second)
			continue
		}

		parsedPacket, err := c.rxAndParsingFunc(c.conn)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			// if EOF, normally exit
			if err == io.EOF || c.parsingErrCnt > 4 {
				c.SetConnectionState(false)
				return err
			}
			c.parsingErrCnt++
			continue
		}

		if err := c.handlePacket(parsedPacket); err != nil {
			log.Printf("failed to handlePacket. error: %+v", err)
		}

	}
}

func (c *ClientBase) SendMessage(msg []byte, timeout time.Duration) error {
	if timeout == 0 {
		timeout = 5 * time.Second
	}

	c.conn.SetWriteDeadline(time.Now().Add(timeout))
	defer c.conn.SetWriteDeadline(time.Time{})

	written := 0
	for written < len(msg) {
		n, err := c.conn.Write(msg[written:])
		if err != nil {
			return err
		}
		written += n
	}
	return nil
}

func (c *ClientBase) Shutdown() error {
	c.cancel()

	c.isConnected = false
	if c.conn != nil {
		return c.conn.Close()
	}

	c.wg.Wait()
	return nil
}

func (c *ClientBase) StartTCPClientHeartbeat() {
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
				connected := c.IsConnected() && c.CheckConnection()
				if !connected {
					log.Println("[TCP Client Heartbeat] Connection is closed, attempting to reconnect...")
					if err := c.Connect(); err != nil {
						log.Printf("[TCP Client Heartbeat] Failed to reconnect:\n\t%v", err)
					}
				}
			case <-c.ctx.Done():
				log.Println("[TCP Client Heartbeat] TCP Client heartbeat goroutine is terminated")
				return
			}
		}
	}()
}

// check the connection
func (c *ClientBase) CheckConnection() bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return false
	}

	// TCP 연결 상태 확인을 위한 더미 데이터 전송
	c.conn.SetWriteDeadline(time.Now().Add(1 * time.Second))
	defer c.conn.SetWriteDeadline(time.Time{})

	_, err := c.conn.Write([]byte{})
	if err != nil {
		c.isConnected = false
		return false
	}

	return true
}
