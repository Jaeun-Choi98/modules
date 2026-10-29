// Package udpnet은 비연결형 UDP 소켓 위에서 송수신, 응답 주소 변환, 핑퐁 기반 생존 확인을 제공합니다.
//
// UDP에는 연결이 없으므로 연결 상태 플래그나 재연결 개념을 두지 않습니다.
// 소켓은 Start에서 한 번 열고 Shutdown에서 닫으며, 상대의 수신 가능 여부는 Prober로 확인합니다.
//
// 지원하는 포트 구성:
//   - 송신 소켓과 수신 소켓 분리: Config.SendBindAddr
//   - 상대의 수신 포트가 송신 포트와 다름: 핸들러에서 SendTo의 목적지 포트를 지정
//   - 멀티캐스트 수신: Config.MulticastGroup / 브로드캐스트 송신: SendTo(255.255.255.255:port 등)
package udpnet

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"runtime/debug"
	"sync"
	"time"
)

var (
	ErrNotStarted     = errors.New("udpnet: endpoint not started")
	ErrEndpointClosed = errors.New("udpnet: endpoint closed")
	ErrAlreadyStarted = errors.New("udpnet: endpoint already started")
	ErrNilHandler     = errors.New("udpnet: packet handler is nil")
)

const defaultReadBufferSize = 65535

type Config struct {
	// 수신 소켓 바인드 주소. 비우거나 포트가 0이면 임의 포트.
	LocalAddr string

	// 송신 전용 소켓을 바인드할 로컬 주소 (= 송신 패킷의 출발 주소). 목적지가 아님.
	// 비우면 수신 소켓으로 송신하므로 출발 포트가 LocalAddr 포트와 같음.
	// 송신 소켓으로 들어오는 패킷은 읽지 않음.
	SendBindAddr string

	// 설정하면 LocalAddr의 포트로 이 멀티캐스트 그룹에 가입해 수신함 (예: "239.0.0.1").
	MulticastGroup string
	// 멀티캐스트 그룹에 가입할 네트워크 인터페이스 이름. 비우면 시스템 기본.
	MulticastInterface string

	// 수신 버퍼 크기. 0이면 65535.
	ReadBufferSize int
}

type Endpoint struct {
	cfg Config

	ctx    context.Context
	cancel context.CancelFunc

	mu           sync.RWMutex
	started      bool
	recvConn     *net.UDPConn
	sendConn     *net.UDPConn // 분리하지 않으면 recvConn과 같음
	handler      func(data []byte, src *net.UDPAddr)
	panicHandler func(src *net.UDPAddr, data []byte, r any, stack []byte)
	probes       []*Prober

	wg sync.WaitGroup // readLoop, probe

	// 패킷 핸들러 고루틴. readLoop만 Add하므로 readLoop 종료(wg.Wait) 이후에는 증가하지 않음.
	handlerWg sync.WaitGroup
}

func NewEndpoint(ctx context.Context, cfg Config) (*Endpoint, error) {
	e := &Endpoint{cfg: cfg}

	if cfg.MulticastGroup != "" {
		ip := net.ParseIP(cfg.MulticastGroup)
		if ip == nil || !ip.IsMulticast() {
			return nil, fmt.Errorf("udpnet: invalid multicast group %q", cfg.MulticastGroup)
		}
		laddr, err := net.ResolveUDPAddr("udp", cfg.LocalAddr)
		if err != nil || laddr.Port == 0 {
			return nil, fmt.Errorf("udpnet: multicast needs LocalAddr with port, got %q", cfg.LocalAddr)
		}
	}

	if e.cfg.ReadBufferSize <= 0 {
		e.cfg.ReadBufferSize = defaultReadBufferSize
	}

	e.ctx, e.cancel = context.WithCancel(ctx)
	return e, nil
}

// 수신한 패킷을 처리할 함수. 패킷마다 별도 고루틴에서 호출됨.
func (e *Endpoint) SetHandler(f func(data []byte, src *net.UDPAddr)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.handler = f
}

// 패킷 핸들러에서 panic이 발생했을 때 호출됨. nil이면 로그만 남김.
func (e *Endpoint) SetPanicHandler(f func(src *net.UDPAddr, data []byte, r any, stack []byte)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.panicHandler = f
}

func (e *Endpoint) Start() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.ctx.Err() != nil {
		return ErrEndpointClosed
	}
	if e.started {
		return ErrAlreadyStarted
	}
	if e.handler == nil {
		return ErrNilHandler
	}

	recv, err := e.listenRecv()
	if err != nil {
		return err
	}
	send := recv
	if e.cfg.SendBindAddr != "" {
		send, err = listenUDP(e.cfg.SendBindAddr)
		if err != nil {
			recv.Close()
			return fmt.Errorf("udpnet: listen send socket: %w", err)
		}
	}

	e.recvConn, e.sendConn = recv, send
	e.started = true

	e.wg.Add(1)
	go e.readLoop(recv)
	return nil
}

func (e *Endpoint) listenRecv() (*net.UDPConn, error) {
	if e.cfg.MulticastGroup == "" {
		conn, err := listenUDP(e.cfg.LocalAddr)
		if err != nil {
			return nil, fmt.Errorf("udpnet: listen recv socket: %w", err)
		}
		return conn, nil
	}

	laddr, _ := net.ResolveUDPAddr("udp", e.cfg.LocalAddr) // NewEndpoint에서 검증됨
	var iface *net.Interface
	if e.cfg.MulticastInterface != "" {
		var err error
		if iface, err = net.InterfaceByName(e.cfg.MulticastInterface); err != nil {
			return nil, fmt.Errorf("udpnet: multicast interface: %w", err)
		}
	}
	group := &net.UDPAddr{IP: net.ParseIP(e.cfg.MulticastGroup), Port: laddr.Port}
	conn, err := net.ListenMulticastUDP("udp", iface, group)
	if err != nil {
		return nil, fmt.Errorf("udpnet: join multicast group: %w", err)
	}
	return conn, nil
}

func listenUDP(addr string) (*net.UDPConn, error) {
	var laddr *net.UDPAddr
	if addr != "" {
		var err error
		if laddr, err = net.ResolveUDPAddr("udp", addr); err != nil {
			return nil, err
		}
	}
	return net.ListenUDP("udp", laddr)
}

func (e *Endpoint) readLoop(conn *net.UDPConn) {
	defer e.wg.Done()

	buf := make([]byte, e.cfg.ReadBufferSize)
	for {
		n, src, err := conn.ReadFromUDP(buf)
		if e.ctx.Err() != nil {
			return
		}
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			// UDP 소켓은 상대 때문에 끊기지 않으므로 소켓을 다시 열지 않고 계속 수신함.
			// (예: Windows에서는 ICMP port unreachable이 ReadFrom 에러로 올라옴)
			log.Printf("[udpnet] read error: %v", err)
			select {
			case <-e.ctx.Done():
				return
			case <-time.After(10 * time.Millisecond):
			}
			continue
		}

		data := make([]byte, n)
		copy(data, buf[:n])

		if e.observe(data, src) {
			continue // Probe가 소비한 pong
		}

		e.mu.RLock()
		handler := e.handler
		e.mu.RUnlock()

		e.handlerWg.Add(1)
		go e.serve(handler, data, src)
	}
}

// 패킷 고루틴의 panic은 Start를 호출한 고루틴의 recover로 잡히지 않으므로 여기서 처리.
func (e *Endpoint) serve(f func(data []byte, src *net.UDPAddr), data []byte, src *net.UDPAddr) {
	defer e.handlerWg.Done()
	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			e.mu.RLock()
			panicHandler := e.panicHandler
			e.mu.RUnlock()

			if panicHandler != nil {
				safeCall(func() { panicHandler(src, data, r, stack) })
			} else {
				log.Printf("[udpnet] panic in handle packet (%s): %v\n%s", src, r, stack)
			}
		}
	}()
	f(data, src)
}

// 사용자 콜백의 panic으로 프로세스가 종료되지 않도록 보호.
func safeCall(f func()) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[udpnet] panic in callback: %v\n%s", r, debug.Stack())
		}
	}()
	f()
}

// addr로 전송. 출발 주소는 송신 소켓(SendBindAddr, 없으면 LocalAddr)으로 정해짐. 브로드캐스트·멀티캐스트 주소도 가능.
func (e *Endpoint) SendTo(data []byte, addr *net.UDPAddr) error {
	e.mu.RLock()
	conn := e.sendConn
	e.mu.RUnlock()

	if conn == nil {
		if e.ctx.Err() != nil {
			return ErrEndpointClosed
		}
		return ErrNotStarted
	}
	_, err := conn.WriteToUDP(data, addr)
	return err
}

// 수신 소켓의 로컬 주소. Start 전이나 Shutdown 후에는 nil.
func (e *Endpoint) LocalAddr() *net.UDPAddr {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.recvConn == nil {
		return nil
	}
	return e.recvConn.LocalAddr().(*net.UDPAddr)
}

// 송신 소켓의 로컬 주소. 분리하지 않았다면 LocalAddr와 같음.
func (e *Endpoint) SendLocalAddr() *net.UDPAddr {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.sendConn == nil {
		return nil
	}
	return e.sendConn.LocalAddr().(*net.UDPAddr)
}

// 종료 순서:
//  1. ctx 취소 → 이후 Start, Probe 추가 차단
//  2. 읽기 대기 해제 → readLoop가 새 패킷을 받지 않고 종료
//  3. readLoop, probe 종료 대기
//  4. 처리 중인 패킷 핸들러 종료 대기 (소켓이 열려 있어 응답 전송 가능)
//  5. 소켓 닫기
//
// 핸들러가 블로킹되어 있으면 반환될 때까지 기다림.
func (e *Endpoint) Shutdown() {
	e.mu.Lock()
	e.cancel()
	recv := e.recvConn
	e.mu.Unlock()

	if recv != nil {
		recv.SetReadDeadline(time.Now())
	}

	e.wg.Wait()
	e.handlerWg.Wait()

	e.mu.Lock()
	if e.sendConn != nil && e.sendConn != e.recvConn {
		e.sendConn.Close()
	}
	if e.recvConn != nil {
		e.recvConn.Close()
	}
	e.recvConn, e.sendConn = nil, nil
	e.mu.Unlock()
}
