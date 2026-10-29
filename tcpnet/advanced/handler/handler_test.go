package handler

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	tcpmd "github.com/Jaeun-Choi98/modules/tcpnet/advanced/model"
)

func TestConcurrentRegisterAndHandle(t *testing.T) {
	h := New[uint16]()
	c := tcpmd.NewConnContext(context.Background(), 1024)
	defer c.Close()

	var handled atomic.Int32
	h.RegisterHandle(uint16(1), func(*tcpmd.HandleContext) error {
		handled.Add(1)
		return nil
	})

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func(id uint16) {
			defer wg.Done()
			h.RegisterHandle(id, func(*tcpmd.HandleContext) error { return nil })
		}(uint16(100 + i))
		go func() {
			defer wg.Done()
			_ = c.SetParsedMsg((&tcpmd.GenericParseMsg[uint16, string]{}).AddPacket(1, "payload"))
			_ = h.HandleMessageSync(c)
		}()
	}
	wg.Wait()

	if handled.Load() == 0 {
		t.Fatal("handler was never called")
	}
}

func TestHandleMessageInvalidPacketIdType(t *testing.T) {
	h := New[uint16]()
	h.RegisterHandle(uint16(1), func(*tcpmd.HandleContext) error { return nil })

	for name, handle := range map[string]func(*tcpmd.ConnContext) error{
		"async": h.HandleMessage,
		"sync":  h.HandleMessageSync,
	} {
		c := tcpmd.NewConnContext(context.Background(), 4)
		// Manager[uint16]에 int packetId 메시지가 들어와도 panic 없이 에러를 반환해야 함
		_ = c.SetParsedMsg((&tcpmd.GenericParseMsg[int, string]{}).AddPacket(1, "payload"))
		if err := handle(c); err == nil {
			t.Fatalf("%s: want error for mismatched packet id type", name)
		}
		c.Close()
	}
}

func TestHandleMessageSyncPanicReturnsError(t *testing.T) {
	h := New[uint16]()
	h.RegisterHandle(uint16(1), func(*tcpmd.HandleContext) error { panic("boom") })

	c := tcpmd.NewConnContext(context.Background(), 4)
	defer c.Close()
	_ = c.SetParsedMsg((&tcpmd.GenericParseMsg[uint16, string]{}).AddPacket(1, "payload"))

	if err := h.HandleMessageSync(c); err == nil {
		t.Fatal("want error from panicking handler")
	}
}

func TestRegisterInvalidPacketIdTypePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("want panic for mismatched packet id type")
		}
	}()
	New[uint16]().RegisterHandle(1, func(*tcpmd.HandleContext) error { return nil })
}
