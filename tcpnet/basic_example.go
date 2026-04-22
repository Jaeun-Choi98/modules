package tcpnet

import (
	"bufio"
	"context"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/Jaeun-Choi98/modules/tcpnet/basic/client"
)

type CustomClient struct {
	BaseClient   *client.ClientBase
	firstRequest bool

	parsingErrCnt int

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func NewCustomClient(parentCtx context.Context, timeout, reconnect time.Duration) *CustomClient {
	c, _ := client.NewClientBase(parentCtx, timeout, reconnect)
	nctx, ncancel := context.WithCancel(parentCtx)
	c.SetIpAndPort("localhost", "5000")
	customClient := &CustomClient{
		BaseClient:   c,
		firstRequest: true,
		ctx:          nctx,
		cancel:       ncancel,
	}
	customClient.implHandleConnection()
	return customClient
}

func (c *CustomClient) implHandleConnection() {
	c.BaseClient.SetHandleConnectFunc(func(conn net.Conn) {
		defer c.wg.Done()
		for {

			select {
			case <-c.ctx.Done():
				return
			default:
			}

			if !c.BaseClient.IsConnected() {
				time.Sleep(1 * time.Second)
				continue
			}

			conn.SetReadDeadline(time.Now().Add(15 * time.Second))

			// =============== parser space =============== //
			parsedPacket, err := bufio.NewReader(conn).ReadString('\n')
			// =============== parser space =============== //

			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue
				}
				// if EOF, normally exit
				if err == io.EOF || c.parsingErrCnt > 4 {
					c.BaseClient.SetConnectionState(false)
					return
				}
				c.parsingErrCnt++
				continue
			}

			// =============== handler space =============== //
			log.Print(parsedPacket)
			// =============== handler space =============== //
		}
	})
}

func (c *CustomClient) Start() error {
	return c.BaseClient.Start()
}

func (c *CustomClient) Shutdown() {
	c.BaseClient.Shutdown()
}

func (c *CustomClient) StartTCPClientHeartbeat() {
	c.BaseClient.StartTCPClientHeartbeat()
}
