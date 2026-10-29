package server

import (
	"context"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestStartAcceptShutdown(t *testing.T) {
	s, _ := NewServerBase(context.Background(), 10*time.Millisecond, 3)
	s.SetIpAndPort("127.0.0.1", "0")

	accepted := make(chan struct{}, 1)
	s.SetHandleConnectFunc(func(conn net.Conn) {
		conn.Close()
		accepted <- struct{}{}
	})
	if err := s.Listening(); err != nil {
		t.Fatal(err)
	}
	s.mu.RLock()
	addr := s.listener.Addr().String()
	s.mu.RUnlock()

	startErr := make(chan error, 1)
	go func() { startErr <- s.Start() }()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()

	select {
	case <-accepted:
	case <-time.After(3 * time.Second):
		t.Fatal("connection was not accepted")
	}

	s.Shutdown()
	select {
	case err := <-startErr:
		if err != nil {
			t.Fatalf("Start returned %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Start did not return after Shutdown")
	}
}

// 리스너가 내려갔다 올라오는 동안 Accept 루프가 nil 리스너를 참조하지 않아야 함
func TestAcceptLoopWithListenerReset(t *testing.T) {
	s, _ := NewServerBase(context.Background(), 10*time.Millisecond, 3)
	s.SetIpAndPort("127.0.0.1", "0")
	s.SetHandleConnectFunc(func(conn net.Conn) { conn.Close() })

	startErr := make(chan error, 1)
	go func() { startErr <- s.Start() }()

	for i := 0; i < 20; i++ {
		time.Sleep(5 * time.Millisecond)
		s.SetListeningState(false)
		if err := s.Listening(); err != nil {
			t.Fatal(err)
		}
	}

	s.Shutdown()
	if err := <-startErr; err != nil {
		t.Fatalf("Start returned %v", err)
	}
}

// 연결 핸들러의 panic이 프로세스를 종료시키지 않고, 훅 호출 후 conn을 닫고, 다음 연결을 계속 받아야 함
func TestHandleConnectPanicRecovered(t *testing.T) {
	for _, tc := range []struct {
		name string
		hook func(hooked chan any) func(net.Conn, any, []byte)
	}{
		{"default log", nil},
		{"hook", func(hooked chan any) func(net.Conn, any, []byte) {
			return func(conn net.Conn, r any, stack []byte) {
				if conn == nil || len(stack) == 0 {
					t.Error("hook got nil conn or empty stack")
				}
				hooked <- r
			}
		}},
		{"panicking hook", func(hooked chan any) func(net.Conn, any, []byte) {
			return func(net.Conn, any, []byte) {
				hooked <- "called"
				panic("hook boom")
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := NewServerBase(context.Background(), 10*time.Millisecond, 3)
			s.SetIpAndPort("127.0.0.1", "0")

			hooked := make(chan any, 1)
			if tc.hook != nil {
				s.SetPanicHandler(tc.hook(hooked))
			}

			var first atomic.Bool
			first.Store(true)
			accepted := make(chan struct{}, 1)
			s.SetHandleConnectFunc(func(conn net.Conn) {
				if first.CompareAndSwap(true, false) {
					panic("boom")
				}
				conn.Close()
				accepted <- struct{}{}
			})
			if err := s.Listening(); err != nil {
				t.Fatal(err)
			}
			s.mu.RLock()
			addr := s.listener.Addr().String()
			s.mu.RUnlock()

			startErr := make(chan error, 1)
			go func() { startErr <- s.Start() }()
			defer func() {
				s.Shutdown()
				<-startErr
			}()

			// 첫 연결: panic 후 서버가 conn을 닫아야 함
			conn, err := net.Dial("tcp", addr)
			if err != nil {
				t.Fatal(err)
			}
			conn.SetReadDeadline(time.Now().Add(3 * time.Second))
			if _, err := conn.Read(make([]byte, 1)); err != io.EOF {
				t.Fatalf("read on panicked conn = %v, want EOF", err)
			}
			conn.Close()

			if tc.hook != nil {
				select {
				case <-hooked:
				case <-time.After(3 * time.Second):
					t.Fatal("panic handler not called")
				}
			}

			// 두 번째 연결은 정상 처리
			conn2, err := net.Dial("tcp", addr)
			if err != nil {
				t.Fatal(err)
			}
			conn2.Close()
			select {
			case <-accepted:
			case <-time.After(3 * time.Second):
				t.Fatal("server stopped accepting after panic")
			}
		})
	}
}

func startTestServer(t *testing.T, handle func(net.Conn)) (*ServerBase, string, chan error) {
	t.Helper()
	s, _ := NewServerBase(context.Background(), 10*time.Millisecond, 3)
	s.SetIpAndPort("127.0.0.1", "0")
	s.SetHandleConnectFunc(handle)
	if err := s.Listening(); err != nil {
		t.Fatal(err)
	}
	s.mu.RLock()
	addr := s.listener.Addr().String()
	s.mu.RUnlock()

	startErr := make(chan error, 1)
	go func() { startErr <- s.Start() }()
	return s, addr, startErr
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// Shutdown은 활성 연결을 닫아 블로킹된 Read를 풀고, 핸들러가 모두 반환된 뒤에 반환되어야 함
func TestShutdownClosesActiveConnsAndWaitsHandlers(t *testing.T) {
	var handlersDone atomic.Int32
	s, addr, startErr := startTestServer(t, func(conn net.Conn) {
		defer conn.Close()
		buf := make([]byte, 64)
		for {
			if _, err := conn.Read(buf); err != nil {
				break
			}
		}
		time.Sleep(100 * time.Millisecond) // 정리 작업 시뮬레이션
		handlersDone.Add(1)
	})

	const n = 3
	clients := make([]net.Conn, n)
	for i := range clients {
		c, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		clients[i] = c
	}
	waitFor(t, func() bool { return s.ActiveConnCount() == n })

	done := make(chan struct{})
	go func() {
		s.Shutdown()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Shutdown did not return")
	}

	if got := handlersDone.Load(); got != n {
		t.Fatalf("handlers finished before Shutdown returned = %d, want %d", got, n)
	}
	if got := s.ActiveConnCount(); got != 0 {
		t.Fatalf("ActiveConnCount after Shutdown = %d, want 0", got)
	}
	for _, c := range clients {
		c.SetReadDeadline(time.Now().Add(time.Second))
		if _, err := c.Read(make([]byte, 1)); err != io.EOF {
			t.Fatalf("client read after Shutdown = %v, want EOF", err)
		}
	}
	if err := <-startErr; err != nil {
		t.Fatalf("Start returned %v", err)
	}
}

// Shutdown 이후에는 listener 재생성, 고루틴 추가가 일어나지 않아야 함
func TestNoRestartAfterShutdown(t *testing.T) {
	s, _, startErr := startTestServer(t, func(conn net.Conn) { conn.Close() })

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.StartTCPServerHeartbeat()
		}()
	}
	s.Shutdown()
	wg.Wait()
	<-startErr

	if err := s.Listening(); err == nil {
		t.Fatal("Listening after Shutdown should fail")
	}
	if err := s.Start(); err == nil {
		t.Fatal("Start after Shutdown should fail")
	}
	s.StartTCPServerHeartbeat() // no-op, panic 없어야 함
	if s.IsListening() {
		t.Fatal("server is listening after Shutdown")
	}
}
