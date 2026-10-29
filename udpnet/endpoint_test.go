package udpnet

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	udpmd "github.com/Jaeun-Choi98/modules/udpnet/advanced/model"
)

var _ udpmd.Sender = (*Endpoint)(nil)

type packet struct {
	data string
	src  *net.UDPAddr
}

func newTestEndpoint(t *testing.T, cfg Config, handler func(ep *Endpoint, data []byte, src *net.UDPAddr)) *Endpoint {
	t.Helper()
	if cfg.LocalAddr == "" {
		cfg.LocalAddr = "127.0.0.1:0"
	}
	ep, err := NewEndpoint(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	ep.SetHandler(func(data []byte, src *net.UDPAddr) { handler(ep, data, src) })
	if err := ep.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ep.Shutdown)
	return ep
}

func recv[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for value")
		panic("unreachable")
	}
}

// 송신·수신 소켓을 분리하고, 상대가 송신 포트가 아닌 고정 포트로 응답하는 구성
func TestSeparateSendSocketAndDifferentReplyPort(t *testing.T) {
	aRecv := make(chan packet, 1)
	a := newTestEndpoint(t, Config{SendBindAddr: "127.0.0.1:0"}, func(_ *Endpoint, data []byte, src *net.UDPAddr) {
		aRecv <- packet{string(data), src}
	})

	bRecv := make(chan packet, 1)
	b := newTestEndpoint(t, Config{
		SendBindAddr: "127.0.0.1:0",
	}, func(ep *Endpoint, data []byte, src *net.UDPAddr) {
		bRecv <- packet{string(data), src}
		// A는 송신 포트와 다른 포트에서 수신하므로 출발지 IP + A의 수신 포트로 응답
		ep.SendTo(append([]byte("re:"), data...), &net.UDPAddr{IP: src.IP, Port: a.LocalAddr().Port})
	})

	if a.LocalAddr().Port == a.SendLocalAddr().Port {
		t.Fatal("send socket was not separated")
	}

	if err := a.SendTo([]byte("hi"), b.LocalAddr()); err != nil {
		t.Fatal(err)
	}

	got := recv(t, bRecv)
	if got.src.Port != a.SendLocalAddr().Port {
		t.Fatalf("B saw src port %d, want A send port %d", got.src.Port, a.SendLocalAddr().Port)
	}

	reply := recv(t, aRecv)
	if reply.data != "re:hi" {
		t.Fatalf("reply = %q, want re:hi", reply.data)
	}
	if reply.src.Port != b.SendLocalAddr().Port {
		t.Fatalf("A saw src port %d, want B send port %d", reply.src.Port, b.SendLocalAddr().Port)
	}
}

func TestProbeReachability(t *testing.T) {
	// 장비 역할: PING에 PONG으로 응답. 송신 소켓이 분리되어 있어 pong의 출발 포트가 ping 대상 포트와 다름.
	b := newTestEndpoint(t, Config{SendBindAddr: "127.0.0.1:0"}, func(ep *Endpoint, data []byte, src *net.UDPAddr) {
		if string(data) == "PING" {
			ep.SendTo([]byte("PONG"), src)
		}
	})

	var handled atomic.Int32
	a := newTestEndpoint(t, Config{}, func(*Endpoint, []byte, *net.UDPAddr) {
		handled.Add(1)
	})

	changes := make(chan bool, 4)
	p, err := a.StartProbe(Probe{
		Target:   b.LocalAddr().String(),
		Interval: 20 * time.Millisecond,
		Timeout:  100 * time.Millisecond,
		Ping:     []byte("PING"),
		IsPong:   func(data []byte, _ *net.UDPAddr) bool { return string(data) == "PONG" },
		OnChange: func(reachable bool) { changes <- reachable },
	})
	if err != nil {
		t.Fatal(err)
	}

	if !recv(t, changes) || !p.Reachable() {
		t.Fatal("want reachable after pong")
	}
	if handled.Load() != 0 {
		t.Fatalf("pong delivered to handler %d times, want 0", handled.Load())
	}

	b.Shutdown()
	if recv(t, changes) {
		t.Fatal("want unreachable after peer stopped")
	}
	if p.Reachable() {
		t.Fatal("Reachable() = true after peer stopped")
	}
}

// pong이 아니어도 대상에서 온 패킷은 생존 신호이며, 트래픽이 있는 동안 ping을 생략함
func TestProbeCountsAnyTrafficAndSkipsPing(t *testing.T) {
	var pings atomic.Int32
	b := newTestEndpoint(t, Config{}, func(_ *Endpoint, data []byte, _ *net.UDPAddr) {
		if string(data) == "PING" {
			pings.Add(1)
		}
	})

	var handled atomic.Int32
	a := newTestEndpoint(t, Config{}, func(*Endpoint, []byte, *net.UDPAddr) {
		handled.Add(1)
	})

	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-time.After(5 * time.Millisecond):
				b.SendTo([]byte("data"), a.LocalAddr())
			}
		}
	}()

	changes := make(chan bool, 4)
	p, err := a.StartProbe(Probe{
		Target:   b.LocalAddr().String(),
		Interval: 50 * time.Millisecond,
		Ping:     []byte("PING"),
		OnChange: func(reachable bool) { changes <- reachable },
	})
	if err != nil {
		t.Fatal(err)
	}
	if !recv(t, changes) {
		t.Fatal("want reachable from data traffic")
	}

	time.Sleep(300 * time.Millisecond)
	if !p.Reachable() {
		t.Fatal("want reachable while traffic flows")
	}
	// 시작 직후 1회 외에는 ping이 나가지 않아야 함
	if n := pings.Load(); n > 1 {
		t.Fatalf("pings sent during traffic = %d, want <= 1", n)
	}
	if handled.Load() == 0 {
		t.Fatal("data packets should reach handler")
	}
}

// Shutdown은 처리 중인 핸들러를 기다리고, 그동안 응답 전송이 가능해야 함
func TestShutdownWaitsInFlightHandlers(t *testing.T) {
	peerRecv := make(chan string, 1)
	peer := newTestEndpoint(t, Config{}, func(_ *Endpoint, data []byte, _ *net.UDPAddr) {
		peerRecv <- string(data)
	})

	started := make(chan struct{})
	var finished atomic.Bool
	ep, err := NewEndpoint(context.Background(), Config{LocalAddr: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	ep.SetHandler(func(data []byte, src *net.UDPAddr) {
		close(started)
		time.Sleep(200 * time.Millisecond)
		ep.SendTo(data, src)
		finished.Store(true)
	})
	if err := ep.Start(); err != nil {
		t.Fatal(err)
	}

	peer.SendTo([]byte("hello"), ep.LocalAddr())
	<-started

	begin := time.Now()
	ep.Shutdown()

	if !finished.Load() {
		t.Fatal("Shutdown returned before in-flight handler finished")
	}
	if got := recv(t, peerRecv); got != "hello" {
		t.Fatalf("reply during Shutdown = %q", got)
	}
	if elapsed := time.Since(begin); elapsed > time.Second {
		t.Fatalf("Shutdown took %v", elapsed)
	}
}

func TestHandlerPanicRecovered(t *testing.T) {
	hooked := make(chan string, 1)
	echoed := make(chan string, 1)

	peer := newTestEndpoint(t, Config{}, func(_ *Endpoint, data []byte, _ *net.UDPAddr) {
		echoed <- string(data)
	})

	ep, _ := NewEndpoint(context.Background(), Config{LocalAddr: "127.0.0.1:0"})
	ep.SetPanicHandler(func(src *net.UDPAddr, data []byte, r any, stack []byte) {
		if src == nil || len(stack) == 0 {
			t.Error("hook got nil src or empty stack")
		}
		hooked <- string(data)
		panic("hook boom") // 훅 자체의 panic도 보호되어야 함
	})
	ep.SetHandler(func(data []byte, src *net.UDPAddr) {
		if string(data) == "boom" {
			panic("boom")
		}
		ep.SendTo(data, src)
	})
	if err := ep.Start(); err != nil {
		t.Fatal(err)
	}
	defer ep.Shutdown()

	peer.SendTo([]byte("boom"), ep.LocalAddr())
	if got := recv(t, hooked); got != "boom" {
		t.Fatalf("hook data = %q", got)
	}
	peer.SendTo([]byte("hello"), ep.LocalAddr())
	if got := recv(t, echoed); got != "hello" {
		t.Fatalf("echo after panic = %q", got)
	}
}

func TestLifecycleErrors(t *testing.T) {
	ep, err := NewEndpoint(context.Background(), Config{LocalAddr: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}

	if err := ep.SendTo([]byte("x"), &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9}); !errors.Is(err, ErrNotStarted) {
		t.Fatalf("SendTo before Start = %v", err)
	}
	if _, err := ep.StartProbe(Probe{Target: "127.0.0.1:9", Interval: time.Second}); !errors.Is(err, ErrNotStarted) {
		t.Fatalf("StartProbe before Start = %v", err)
	}
	if err := ep.Start(); !errors.Is(err, ErrNilHandler) {
		t.Fatalf("Start without handler = %v", err)
	}

	ep.SetHandler(func([]byte, *net.UDPAddr) {})
	if err := ep.Start(); err != nil {
		t.Fatal(err)
	}
	if err := ep.Start(); !errors.Is(err, ErrAlreadyStarted) {
		t.Fatalf("second Start = %v", err)
	}
	if _, err := ep.StartProbe(Probe{Interval: time.Second}); err == nil {
		t.Fatal("StartProbe without Target should fail")
	}

	ep.Shutdown()
	ep.Shutdown() // 중복 호출 안전

	if err := ep.Start(); !errors.Is(err, ErrEndpointClosed) {
		t.Fatalf("Start after Shutdown = %v", err)
	}
	if err := ep.SendTo([]byte("x"), &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9}); !errors.Is(err, ErrEndpointClosed) {
		t.Fatalf("SendTo after Shutdown = %v", err)
	}
	if _, err := ep.StartProbe(Probe{Target: "127.0.0.1:9", Interval: time.Second}); !errors.Is(err, ErrEndpointClosed) {
		t.Fatalf("StartProbe after Shutdown = %v", err)
	}
	if ep.LocalAddr() != nil {
		t.Fatal("LocalAddr after Shutdown should be nil")
	}
}

func TestInvalidConfig(t *testing.T) {
	for name, cfg := range map[string]Config{
		"not multicast group":    {LocalAddr: ":5000", MulticastGroup: "10.0.0.1"},
		"multicast without port": {LocalAddr: "", MulticastGroup: "239.0.0.1"},
	} {
		if _, err := NewEndpoint(context.Background(), cfg); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestMulticastReceive(t *testing.T) {
	// 사용 가능한 포트 확보
	tmp, err := net.ListenUDP("udp4", &net.UDPAddr{})
	if err != nil {
		t.Fatal(err)
	}
	port := tmp.LocalAddr().(*net.UDPAddr).Port
	tmp.Close()

	const group = "239.255.43.21"
	got := make(chan string, 1)
	ep, err := NewEndpoint(context.Background(), Config{
		LocalAddr:      net.JoinHostPort("", strconv.Itoa(port)),
		MulticastGroup: group,
	})
	if err != nil {
		t.Fatal(err)
	}
	ep.SetHandler(func(data []byte, _ *net.UDPAddr) {
		select {
		case got <- string(data):
		default:
		}
	})
	if err := ep.Start(); err != nil {
		t.Skipf("multicast not available: %v", err)
	}
	defer ep.Shutdown()

	sender := newTestEndpoint(t, Config{LocalAddr: ":0"}, func(*Endpoint, []byte, *net.UDPAddr) {})
	dst := &net.UDPAddr{IP: net.ParseIP(group), Port: port}

	deadline := time.After(2 * time.Second)
	for {
		if err := sender.SendTo([]byte("hello group"), dst); err != nil && strings.Contains(err.Error(), "unreachable") {
			t.Skipf("multicast route not available: %v", err)
		}
		select {
		case msg := <-got:
			if msg != "hello group" {
				t.Fatalf("multicast data = %q", msg)
			}
			return
		case <-deadline:
			t.Skip("multicast packet not delivered (no multicast route in this environment)")
		case <-time.After(100 * time.Millisecond):
		}
	}
}
