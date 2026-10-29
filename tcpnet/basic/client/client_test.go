package client

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestSendMessageBeforeConnect(t *testing.T) {
	c, _ := NewClientBase(context.Background(), time.Second, time.Second)
	if err := c.SendMessage([]byte("hi"), time.Second); err == nil {
		t.Fatal("want error when not connected")
	}
}

func TestShutdownWaitsHeartbeatAndBlocksReconnect(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			defer conn.Close()
		}
	}()

	host, port, _ := net.SplitHostPort(ln.Addr().String())
	c, _ := NewClientBase(context.Background(), time.Second, 10*time.Millisecond)
	c.SetIpAndPort(host, port)
	if err := c.Connect(); err != nil {
		t.Fatal(err)
	}
	c.StartTCPClientHeartbeat()
	if err := c.SendMessage([]byte("hi"), time.Second); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		c.Shutdown()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Shutdown did not return")
	}

	if err := c.Connect(); err == nil {
		t.Fatal("Connect after Shutdown should fail")
	}
}
