package parser

import (
	"net"

	"github.com/Jaeun-Choi98/modules/tcpnet/server/model"
)

type Parser interface {
	Parse(conn net.Conn) (model.ParseMsg, error)
}
