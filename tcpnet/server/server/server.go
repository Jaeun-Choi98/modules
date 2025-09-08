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
	listener net.Listener
	Ip       string
	Port     string

	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.RWMutex
	wg     sync.WaitGroup

	isListening bool
	heartbeat   time.Duration

	HandleConnectFunc func(conn net.Conn)

	timeOutCnt    int
	maxTimeOutCnt int
}

func NewServerBase(ctx context.Context, heartbeat time.Duration, maxTimeOutCnt int) (*ServerBase, error) {
	ctxWithCancel, cancel := context.WithCancel(ctx)
	return &ServerBase{
		ctx:           ctxWithCancel,
		cancel:        cancel,
		heartbeat:     heartbeat,
		maxTimeOutCnt: maxTimeOutCnt,
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
	s.listener = nil
}

func (s *ServerBase) SetIpAndPort(ip, port string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Ip = ip
	s.Port = port
}

// 연결된 클라이언트를 어떻게 관리할 것인지 구현해야 함.
func (s *ServerBase) SetHandleConnectFunc(f func(conn net.Conn)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.HandleConnectFunc = f
}

func (s *ServerBase) Listening() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.Ip == "" || s.Port == "" {
		return fmt.Errorf("need ip and port, call SetIpAndPort.")
	}

	if s.listener == nil {
		listener, err := net.Listen("tcp", fmt.Sprintf("%s:%s", s.Ip, s.Port))
		if err != nil {
			return err
		}
		s.listener = listener
	}
	s.isListening = true
	return nil
}

func (s *ServerBase) Start() error {
	//wg.Add(1)
	//go t.SendMessageRoutine()

	if err := s.Listening(); err != nil {
		return err
	}

	if s.HandleConnectFunc == nil {
		return fmt.Errorf("HandleConnectFunc is nil.")
	}

	s.wg.Add(1)
	return s.WaitForAccept()
}

func (s *ServerBase) WaitForAccept() error {
	defer s.wg.Done()

	for {
		s.mu.RLock()
		listener := s.listener
		isListening := s.isListening
		s.mu.RUnlock()

		select {
		case <-s.ctx.Done():
			return nil
		default:
		}

		if listener == nil || !isListening {
			time.Sleep(1 * time.Second)
			continue
		}

		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.ctx.Done():
				return nil
			default:
				if s.IsListening() {
					s.SetListeningState(false)
				}
			}
			continue
		}
		s.wg.Add(1)
		go s.HandleConnectFunc(conn)
	}
}

func (s *ServerBase) Shutdown() {
	s.mu.Lock()
	s.cancel()
	if s.listener != nil {
		s.listener.Close()
	}

	s.mu.Unlock()
	s.wg.Wait()
}

// 개별적으로 TCP 헬스체크를 관리하려 할 때 사용. 진행했던 프로젝트에서는 모니터링 객체에서 DB와 함께 관리되어짐.
func (s *ServerBase) StartTCPServerHeartbeat() {
	s.wg.Add(1)
	go func() {
		heartbeat := time.NewTicker(s.heartbeat)
		healthCheck := time.NewTicker(s.heartbeat * 3)

		defer func() {
			heartbeat.Stop()
			healthCheck.Stop()
			s.wg.Done()
		}()

		for {
			select {
			case <-heartbeat.C:
				if !s.IsListening() {
					if err := s.Listening(); err != nil {
						log.Printf("[TCP Server Heartbeat] Failed to reconnect:\n\t%v", err)
					}
				}
			case <-healthCheck.C:
				if s.IsListening() && !s.CheckConnection() {
					log.Println("[TCP Server Heartbeat] Connection is closed, attempting to reconnect...")
					if err := s.Listening(); err != nil {
						log.Printf("[TCP Server Heartbeat] Failed to reconnect:\n\t%v", err)
					}
				}
			case <-s.ctx.Done():
				log.Println("[TCP Server Heartbeat] TCP Client heartbeat goroutine terminated")
				return
			}
		}
	}()
}

func (s *ServerBase) CheckConnection() bool {
	s.mu.RLock()
	listener := s.listener
	isListening := s.isListening
	s.mu.RUnlock()

	if listener == nil || !isListening {
		return false
	}

	testConn, err := net.DialTimeout("tcp", "localhost:5000", 300*time.Millisecond)
	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			if s.timeOutCnt >= s.maxTimeOutCnt {
				s.timeOutCnt = 0
				listener.Close()
				s.SetListeningState(false)
				return false
			}
			s.timeOutCnt++
			return false
		} else {
			s.SetListeningState(false)
			return false
		}
	}
	testConn.Close()

	return true
}
