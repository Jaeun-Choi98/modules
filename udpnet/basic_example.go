package udpnet

import (
	"context"
	"log"
	"net"
	"time"

	"github.com/Jaeun-Choi98/modules/udpnet/basic/client"
)

type CustomClient struct {
	BaseClient  *client.ClientBase
	readTimeout time.Duration

	ctx    context.Context
	cancel context.CancelFunc
}

func NewCustomClient(parentCtx context.Context, readTimeout, heartbeat time.Duration) *CustomClient {
	c, _ := client.NewClientBase(parentCtx, readTimeout, heartbeat)
	nctx, ncancel := context.WithCancel(parentCtx)
	c.SetIpAndPort("localhost", "5000")

	customClient := &CustomClient{
		BaseClient:  c,
		readTimeout: readTimeout,
		ctx:         nctx,
		cancel:      ncancel,
	}
	customClient.implHandleConnection()
	return customClient
}

func (c *CustomClient) implHandleConnection() {
	c.BaseClient.SetHandleConnectFunc(func(conn *net.UDPConn) {
		buf := make([]byte, 65535)
		for {
			select {
			case <-c.ctx.Done():
				return
			default:
			}

			conn.SetReadDeadline(time.Now().Add(c.readTimeout))

			// DialUDP로 연결된 소켓은 Read로 수신 가능
			n, err := conn.Read(buf)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue
				}
				c.BaseClient.SetConnectionState(false)
				return
			}

			// =============== handler space =============== //
			log.Printf("received: %s", buf[:n])
			// =============== handler space =============== //
		}
	})
}

func (c *CustomClient) Start() error {
	return c.BaseClient.Start()
}

func (c *CustomClient) Shutdown() {
	c.cancel()
	c.BaseClient.Shutdown()
}

func (c *CustomClient) StartUDPClientHeartbeat(pingData []byte) {
	c.BaseClient.StartUDPClientHeartbeat(pingData)
}
