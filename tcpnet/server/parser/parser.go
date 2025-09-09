package parser

import (
	"net"

	tcpmd "github.com/Jaeun-Choi98/modules/tcpnet/server/model"
)

type Parser interface {
	Parse(conn net.Conn) (tcpmd.ParseMsg, error)
}
