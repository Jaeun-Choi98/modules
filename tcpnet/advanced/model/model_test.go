package tcpmd_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	tcpmd "github.com/Jaeun-Choi98/modules/tcpnet/advanced/model"
)

func TestSetParsedMsgConcurrentClose(t *testing.T) {
	for i := 0; i < 100; i++ {
		ctx := context.Background()
		c := tcpmd.NewConnContext(ctx, 4)

		var wg sync.WaitGroup
		for j := 0; j < 8; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for k := 0; k < 10; k++ {
					_ = c.SetParsedMsg((&tcpmd.GenericParseMsg[uint16, string]{}).AddPacket(1, "payload"))
				}
			}()
		}
		c.Close()
		wg.Wait()

		if err := c.SetParsedMsg((&tcpmd.GenericParseMsg[uint16, string]{}).AddPacket(1, "payload")); !errors.Is(err, tcpmd.ErrContextClosed) {
			t.Fatalf("SetParsedMsg after Close = %v, want ErrContextClosed", err)
		}
	}
}

func TestSetParsedMsgContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	c := tcpmd.NewConnContext(ctx, 1)
	defer c.Close()

	if err := c.SetParsedMsg((&tcpmd.GenericParseMsg[uint16, string]{}).AddPacket(1, "payload")); err != nil {
		t.Fatalf("first SetParsedMsg = %v", err)
	}
	cancel()
	// 버퍼가 가득 찬 상태에서 ctx가 취소되면 조용히 버리지 않고 에러를 반환해야 함
	if err := c.SetParsedMsg((&tcpmd.GenericParseMsg[uint16, string]{}).AddPacket(2, "payload")); !errors.Is(err, context.Canceled) {
		t.Fatalf("SetParsedMsg after cancel = %v, want context.Canceled", err)
	}
}

func TestGetParsedMsgDrainsAfterClose(t *testing.T) {
	c := tcpmd.NewConnContext(context.Background(), 4)
	if err := c.SetParsedMsg((&tcpmd.GenericParseMsg[uint16, string]{}).AddPacket(7, "payload")); err != nil {
		t.Fatal(err)
	}
	c.Close()

	got, ok := c.GetParsedMsg()
	if !ok || got.GetPacketId().(uint16) != 7 {
		t.Fatalf("GetParsedMsg = %v, %v; want buffered msg", got, ok)
	}
	if _, ok := c.GetParsedMsg(); ok {
		t.Fatal("GetParsedMsg on empty closed context returned ok")
	}
}

func TestResolveConcurrentCancelAll(t *testing.T) {
	for i := 0; i < 100; i++ {
		c := tcpmd.NewConnContext(context.Background(), 4)
		m := c.GetPending()
		m.Register("k", make(chan tcpmd.Response, 1))

		var wg sync.WaitGroup
		for j := 0; j < 8; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = m.Resolve("k", &tcpmd.GenericResponse[int]{Payload: 1})
			}()
		}
		c.Close()
		wg.Wait()

		if err := m.Resolve("k", &tcpmd.GenericResponse[int]{}); !errors.Is(err, tcpmd.ErrNoPendingRequest) {
			t.Fatalf("Resolve after Close = %v, want ErrNoPendingRequest", err)
		}
	}
}

func TestResolveBusy(t *testing.T) {
	c := tcpmd.NewConnContext(context.Background(), 4)
	defer c.Close()
	m := c.GetPending()
	ch := make(chan tcpmd.Response, 1)
	m.Register("k", ch)

	if err := m.Resolve("k", &tcpmd.GenericResponse[int]{Payload: 1}); err != nil {
		t.Fatal(err)
	}
	if err := m.Resolve("k", &tcpmd.GenericResponse[int]{Payload: 2}); !errors.Is(err, tcpmd.ErrResponseBusy) {
		t.Fatalf("Resolve to full channel = %v, want ErrResponseBusy", err)
	}
	if r := <-ch; r.GetPayload().(int) != 1 {
		t.Fatalf("payload = %v, want 1", r.GetPayload())
	}
}
