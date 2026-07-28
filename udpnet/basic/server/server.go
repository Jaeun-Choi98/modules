package server

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"time"
)

type ServerBase struct {
	conn *net.UDPConn
	ip   string
	port string

	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.RWMutex
	wg     sync.WaitGroup

	isListening      bool
	heartbeat        time.Duration
	handlePacketFunc func(conn *net.UDPConn, data []byte, addr *net.UDPAddr)
}

func NewServerBase(ctx context.Context, heartbeat time.Duration) (*ServerBase, error) {
	ctxWithCancel, cancel := context.WithCancel(ctx)
	return &ServerBase{
		ctx:       ctxWithCancel,
		cancel:    cancel,
		heartbeat: heartbeat,
	}, nil
}

func (s *ServerBase) IsListening() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isListening
}

func (s *ServerBase) SetListeningState(state bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.isListening = state
	if !state && s.conn != nil {
		s.conn.Close()
		s.conn = nil
	}
}

func (s *ServerBase) SetIpAndPort(ip, port string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ip = ip
	s.port = port
}

// 수신된 패킷을 어떻게 처리할 것인지 구현해야 함. conn을 통해 응답 전송 가능.
func (s *ServerBase) SetHandlePacketFunc(f func(conn *net.UDPConn, data []byte, addr *net.UDPAddr)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlePacketFunc = f
}

func (s *ServerBase) Listening() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.port == "" {
		return fmt.Errorf("need ip and port, call SetIpAndPort")
	}

	if s.conn == nil {
		addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%s", s.ip, s.port))
		if err != nil {
			return err
		}
		conn, err := net.ListenUDP("udp", addr)
		if err != nil {
			return err
		}
		s.conn = conn
	}
	s.isListening = true
	return nil
}

func (s *ServerBase) Start() error {
	if err := s.Listening(); err != nil {
		return err
	}

	if s.handlePacketFunc == nil {
		return fmt.Errorf("HandlePacketFunc is nil")
	}

	s.wg.Add(1)
	go s.readLoop()
	return nil
}

func (s *ServerBase) readLoop() {
	defer s.wg.Done()

	buf := make([]byte, 65535)
	for {
		select {
		case <-s.ctx.Done():
			return
		default:
		}

		s.mu.RLock()
		conn := s.conn
		s.mu.RUnlock()

		if conn == nil {
			time.Sleep(1 * time.Second)
			continue
		}

		conn.SetReadDeadline(time.Now().Add(s.heartbeat))
		n, addr, err := conn.ReadFromUDP(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			select {
			case <-s.ctx.Done():
				return
			default:
				s.SetListeningState(false)
			}
			continue
		}

		data := make([]byte, n)
		copy(data, buf[:n])

		s.mu.RLock()
		f := s.handlePacketFunc
		c := s.conn
		s.mu.RUnlock()

		go f(c, data, addr)
	}
}

func (s *ServerBase) Shutdown() {
	s.mu.Lock()
	s.cancel()
	if s.conn != nil {
		s.conn.Close()
	}
	s.mu.Unlock()
	s.wg.Wait()
}

func (s *ServerBase) StartUDPServerHeartbeat() {
	s.wg.Add(1)
	go func() {
		heartbeat := time.NewTicker(s.heartbeat)
		defer func() {
			heartbeat.Stop()
			s.wg.Done()
		}()

		for {
			select {
			case <-heartbeat.C:
				if !s.IsListening() {
					if err := s.Listening(); err != nil {
						log.Printf("[UDP Server Heartbeat] Failed to rebind: %v", err)
					}
				}
			case <-s.ctx.Done():
				log.Println("[UDP Server Heartbeat] goroutine terminated")
				return
			}
		}
	}()
}
