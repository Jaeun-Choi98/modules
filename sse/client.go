package sse

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

type ClientKey interface {
	byte | int8 | int16 | uint16 | int | int32 | uint32 | int64 | uint64 | float32 | float64 | string
}

type SSEClient[T UserIdKey, K ClientKey] struct {
	ClientId K
	UserId   T
	Writer   http.ResponseWriter
	Flusher  http.Flusher
	Ctx      context.Context
	Cancel   context.CancelFunc
}

func NewSSEClient[T UserIdKey, K ClientKey](clientId K, userId T, ctx *gin.Context) (*SSEClient[T, K], error) {
	flusher, ok := ctx.Writer.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("streaming not supported")
	}
	clientCtx, cancel := context.WithCancel(ctx.Request.Context())
	return &SSEClient[T, K]{
		ClientId: clientId,
		UserId:   userId,
		Writer:   ctx.Writer,
		Flusher:  flusher,
		Ctx:      clientCtx,
		Cancel:   cancel,
	}, nil
}

// SendEvent는 SSE 클라이언트에게 이벤트를 전송
func (c *SSEClient[T, K]) SendMessage(evt Event) error {
	// 컨텍스트가 취소되었는지 확인
	select {
	case <-c.Ctx.Done():
		err := fmt.Errorf("client connection closed")
		return err
	default:
		// SSE 형식으로 이벤트 전송
		event, data, id, retry := evt.GetEvent(), evt.GetData(), evt.GetId(), evt.GetRetry()
		if event != nil {
			fmt.Fprintf(c.Writer, "event: %v\n", event)
		}
		if data != nil {
			fmt.Fprintf(c.Writer, "data: %v\n", data)
		}
		if id != nil {
			fmt.Fprintf(c.Writer, "id: %v\n", id)
		}
		if retry != nil {
			fmt.Fprintf(c.Writer, "retry: %v\n", retry)
		}
		fmt.Fprint(c.Writer, "\n")
		c.Flusher.Flush()
		return nil
	}
}

// Close는 클라이언트 연결을 종료
func (c *SSEClient[T, K]) Close() {
	c.Cancel()
}
