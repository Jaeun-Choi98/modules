package server

import (
	"context"
	"fmt"
	"log"
	"net"
	"runtime/debug"
	"sync"
	"time"
)

type ServerBase struct {
	listener net.Listener
	ip       string
	port     string

	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.RWMutex
	wg     sync.WaitGroup

	isListening bool
	heartbeat   time.Duration

	handleConnectFunc func(conn net.Conn)
	panicHandler      func(conn net.Conn, r any, stack []byte)

	// handleConnectFunc가 실행 중인 연결. Shutdown 시 모두 닫아 블로킹된 Read를 해제함.
	conns map[net.Conn]struct{}

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
		conns:         make(map[net.Conn]struct{}),
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
	if !state && s.listener != nil {
		s.listener.Close()
		s.listener = nil
	}
}

func (s *ServerBase) SetIpAndPort(ip, port string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ip = ip
	s.port = port
}

// 연결된 클라이언트를 어떻게 관리할 것인지 구현해야 함.
func (s *ServerBase) SetHandleConnectFunc(f func(conn net.Conn)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handleConnectFunc = f
}

// handleConnectFunc에서 panic이 발생했을 때 호출됨. nil이면 로그만 남김.
// 호출 이후 conn은 서버가 닫음.
func (s *ServerBase) SetPanicHandler(f func(conn net.Conn, r any, stack []byte)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.panicHandler = f
}

func (s *ServerBase) Listening() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.port == "" {
		return fmt.Errorf("need ip and port, call SetIpAndPort.")
	}

	// Shutdown 이후 heartbeat 등이 listener를 다시 여는 것을 방지
	if err := s.ctx.Err(); err != nil {
		return err
	}

	if s.listener == nil {
		listener, err := net.Listen("tcp", fmt.Sprintf(`%s:%s`, s.ip, s.port))
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

	s.mu.RLock()
	handleConnectFunc := s.handleConnectFunc
	s.mu.RUnlock()

	if handleConnectFunc == nil {
		return fmt.Errorf("HandleConnectFunc is nil.")
	}

	if !s.addGoroutine() {
		return s.ctx.Err()
	}
	return s.WaitForAccept()
}

// Shutdown과 경합하지 않도록 락 안에서 ctx를 확인한 뒤 wg에 등록함.
// Shutdown이 시작된 이후에는 false를 반환 (wg.Wait 이후 wg.Add 방지).
func (s *ServerBase) addGoroutine() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil {
		return false
	}
	s.wg.Add(1)
	return true
}

// 연결을 추적 대상에 등록. Shutdown이 시작된 이후라면 conn을 닫고 false 반환.
func (s *ServerBase) trackConn(conn net.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil {
		conn.Close()
		return false
	}
	s.conns[conn] = struct{}{}
	s.wg.Add(1)
	return true
}

func (s *ServerBase) untrackConn(conn net.Conn) {
	s.mu.Lock()
	delete(s.conns, conn)
	s.mu.Unlock()
	s.wg.Done()
}

func (s *ServerBase) WaitForAccept() error {
	defer s.wg.Done()

	for {
		s.mu.RLock()
		listener := s.listener
		isListening := s.isListening
		handleConnectFunc := s.handleConnectFunc
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

		conn, err := listener.Accept()
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

		if !s.trackConn(conn) {
			return nil
		}
		go s.serveConn(handleConnectFunc, conn)
	}
}

// 연결 고루틴의 panic은 Start를 호출한 고루틴의 recover로 잡히지 않으므로 여기서 처리.
// handleConnectFunc가 반환되면 추적에서 제외됨 (conn을 닫는 것은 기존과 같이 핸들러 책임).
func (s *ServerBase) serveConn(f func(conn net.Conn), conn net.Conn) {
	defer s.untrackConn(conn)
	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			s.mu.RLock()
			panicHandler := s.panicHandler
			s.mu.RUnlock()

			if panicHandler != nil {
				safeCall(func() { panicHandler(conn, r, stack) })
			} else {
				log.Printf("[TCP Server] panic in handle connection (%s): %v\n%s", conn.RemoteAddr(), r, stack)
			}
			conn.Close()
		}
	}()
	f(conn)
}

// panic 핸들러 자체의 panic으로 프로세스가 종료되지 않도록 보호.
func safeCall(f func()) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[TCP Server] panic in panic handler: %v\n%s", r, debug.Stack())
		}
	}()
	f()
}

// 종료 순서:
//  1. ctx 취소 → 이후 새 연결 등록, 고루틴 추가, listener 재생성 차단
//  2. listener 닫기 → Accept 루프 종료
//  3. 추적 중인 conn 모두 닫기 → 핸들러의 블로킹된 Read 해제
//  4. Accept 루프, heartbeat, 모든 연결 핸들러가 반환될 때까지 대기
//
// 핸들러가 conn 외의 이유(외부 채널 등)로 블로킹되어 있으면 반환될 때까지 기다림.
func (s *ServerBase) Shutdown() {
	s.mu.Lock()
	s.cancel()
	s.isListening = false
	if s.listener != nil {
		s.listener.Close()
		s.listener = nil
	}
	for conn := range s.conns {
		conn.Close()
	}
	s.mu.Unlock()

	s.wg.Wait()
}

// 현재 handleConnectFunc가 실행 중인 연결 수.
func (s *ServerBase) ActiveConnCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.conns)
}

// 개별적으로 TCP 헬스체크를 관리하려 할 때 사용. 진행했던 프로젝트에서는 모니터링 객체에서 DB와 함께 관리되어짐.
func (s *ServerBase) StartTCPServerHeartbeat() {
	if !s.addGoroutine() {
		return
	}
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
	addr := fmt.Sprintf(`%s:%s`, s.ip, s.port)
	s.mu.RUnlock()

	if listener == nil || !isListening {
		return false
	}

	testConn, err := net.DialTimeout("tcp", addr, 300*time.Millisecond)
	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			s.mu.Lock()
			s.timeOutCnt++
			exceeded := s.timeOutCnt > s.maxTimeOutCnt
			if exceeded {
				s.timeOutCnt = 0
			}
			s.mu.Unlock()

			if exceeded {
				s.SetListeningState(false)
			}
			return false
		}
		s.SetListeningState(false)
		return false
	}
	testConn.Close()

	s.mu.Lock()
	s.timeOutCnt = 0
	s.mu.Unlock()
	return true
}
