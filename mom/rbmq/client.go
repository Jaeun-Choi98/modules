package rbmq

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// ------------------Channel Pool------------------//
type ChannelPool struct {
	client      *Client
	channels    chan *amqp.Channel
	maxSize     int
	currentSize int
	mu          sync.Mutex
	closed      bool
}

func NewChannelPool(client *Client, maxSize int) (*ChannelPool, error) {
	if maxSize <= 0 {
		maxSize = 10
	}

	pool := &ChannelPool{
		client:   client,
		channels: make(chan *amqp.Channel, maxSize),
		maxSize:  maxSize,
	}

	// 초기 채널 생성
	/*
		for i := 0; i < maxSize/2; i++ {
			ch, err := pool.createChannel()
			if err != nil {
				return nil, err
			}
			pool.channels <- ch
			pool.currentSize++
		}
	*/

	return pool, nil
}

func (p *ChannelPool) createChannel() (*amqp.Channel, error) {
	p.client.mu.RLock()
	conn := p.client.conn
	p.client.mu.RUnlock()

	if conn == nil {
		return nil, fmt.Errorf("nil connection")
	}

	ch, err := conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("failed to create channel: %w", err)
	}

	return ch, nil
}

func (p *ChannelPool) Get() (*amqp.Channel, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, fmt.Errorf("closed channel pool")
	}
	p.mu.Unlock()

	select {
	case ch := <-p.channels:
		if ch != nil && !ch.IsClosed() {
			return ch, nil
		}
		p.mu.Lock()
		p.currentSize--
		p.mu.Unlock()
		return p.createAndIncrementSize()

	default:
		return p.createAndIncrementSize()
	}
}

func (p *ChannelPool) createAndIncrementSize() (*amqp.Channel, error) {
	p.mu.Lock()
	if p.currentSize >= p.maxSize {
		p.mu.Unlock()
		// 풀이 가득 찬 경우 대기
		select {
		case ch := <-p.channels:
			if ch != nil && !ch.IsClosed() {
				return ch, nil
			}
			p.mu.Lock()
			p.currentSize--
			p.mu.Unlock()
			return p.createChannel()
		case <-time.After(5 * time.Second):
			return nil, fmt.Errorf("timeout waiting for channel")
		}
	}
	p.currentSize++
	p.mu.Unlock()

	return p.createChannel()
}

func (p *ChannelPool) Put(ch *amqp.Channel) error {
	if ch == nil {
		return fmt.Errorf("cannot put nil channel")
	}

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		ch.Close()
		return fmt.Errorf("closed pool")
	}
	p.mu.Unlock()

	// 채널이 닫혀있으면 반환하지 않고 폐기
	if ch.IsClosed() {
		p.mu.Lock()
		p.currentSize--
		p.mu.Unlock()
		return nil
	}

	select {
	case p.channels <- ch:
		return nil
	default:
		// 풀이 가득 차면 채널 닫기
		ch.Close()
		p.mu.Lock()
		p.currentSize--
		p.mu.Unlock()
		return nil
	}
}

func (p *ChannelPool) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	p.mu.Unlock()

	close(p.channels)

	for ch := range p.channels {
		if ch != nil && !ch.IsClosed() {
			ch.Close()
		}
	}

	log.Println("closed channel pool")
	return nil
}

func (p *ChannelPool) Size() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.currentSize
}

func (p *ChannelPool) Available() int {
	return len(p.channels)
}

// ------------------Client------------------ //
type Client struct {
	url         string
	conn        *amqp.Connection
	channelPool *ChannelPool
	poolSize    int

	ctx    context.Context
	cancel context.CancelFunc

	mu sync.RWMutex
}

func NewClient(url string, channelPoolSize int) (*Client, error) {
	ctx, cancel := context.WithCancel(context.Background())

	client := &Client{
		url:      url,
		poolSize: channelPoolSize,
		ctx:      ctx,
		cancel:   cancel,
	}

	if err := client.connect(); err != nil {
		cancel()
		return nil, err
	}

	pool, err := NewChannelPool(client, channelPoolSize)
	if err != nil {
		client.Close()
		cancel()
		return nil, fmt.Errorf("failed to create channel pool: %w", err)
	}
	client.channelPool = pool

	return client, nil
}

func (c *Client) connect() error {
	conn, err := amqp.Dial(c.url)
	if err != nil {
		log.Println("failed to connect rabbitmq server:", err)
		return err
	}

	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()

	log.Println("connected to rabbitmq server")
	return nil
}

func (c *Client) reconnect() error {
	c.mu.Lock()

	if c.channelPool != nil {
		c.channelPool.Close()
	}

	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}

	c.mu.Unlock()

	for i := 0; i < 5; i++ {
		if err := c.connect(); err != nil {
			log.Printf("reconnect attempt %d failed: %v", i+1, err)
			time.Sleep(time.Second * time.Duration(i+1))
			continue
		}

		// 새로운 풀 생성
		pool, err := NewChannelPool(c, c.poolSize)
		if err != nil {
			log.Printf("failed to recreate channel pool: %v", err)
			continue
		}
		c.channelPool = pool
		return nil
	}

	return fmt.Errorf("failed to reconnect after 5 attempts")
}

func (c *Client) Close() error {
	c.cancel()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.channelPool != nil {
		c.channelPool.Close()
	}

	if c.conn != nil {
		if err := c.conn.Close(); err != nil {
			log.Println("error closing connection:", err)
		}
		c.conn = nil
	}

	log.Println("closed rabbitmq client connection")
	return nil
}

// GetChannelFromPool 풀에서 채널 가져오기
func (c *Client) GetChannelFromPool() (*amqp.Channel, error) {
	if c.channelPool == nil {
		return nil, fmt.Errorf("channel pool is not initialized")
	}
	return c.channelPool.Get()
}

// ReturnChannelToPool 채널을 풀에 반환
func (c *Client) ReturnChannelToPool(ch *amqp.Channel) error {
	if c.channelPool == nil {
		return fmt.Errorf("channel pool is not initialized")
	}
	return c.channelPool.Put(ch)
}

// GetConnection Connection 반환 (ChannelPool에서 사용)
// func (c *Client) GetConnection() *amqp.Connection {
// 	c.mu.RLock()
// 	defer c.mu.RUnlock()
// 	return c.conn
// }

func (c *Client) PoolStats() (total int, available int) {
	if c.channelPool == nil {
		return 0, 0
	}
	return c.channelPool.Size(), c.channelPool.Available()
}

func (c *Client) StartHealthCheckAndRecover() {
	for {

		blockingCh := make(chan amqp.Blocking, 1)
		closeCh := make(chan *amqp.Error, 1)

		c.mu.RLock()
		conn := c.conn
		c.mu.RUnlock()

		// connection이 없으면 재연결 시도
		if conn == nil {
			log.Println("no connection, trying to reconnect...")
			if err := c.reconnect(); err != nil {
				log.Println("reconnect failed:", err)
				time.Sleep(5 * time.Second)
			}
			continue
		}

		conn.NotifyBlocked(blockingCh)
		conn.NotifyClose(closeCh)

		select {
		case <-c.ctx.Done():
			log.Println("health check stopped")
			return

		case blocking := <-blockingCh:
			log.Printf("connection blocked: %v, trying to reconnect...", blocking.Reason)
			if err := c.reconnect(); err != nil {
				log.Println("reconnect failed:", err)
			}

		case closeErr := <-closeCh:
			log.Printf("connection closed: %v, trying to reconnect...", closeErr)
			if err := c.reconnect(); err != nil {
				log.Println("reconnect failed:", err)
			}
		}
	}
}
