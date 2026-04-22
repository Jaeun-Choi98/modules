package parser

import (
	"net"

	tcpmd "github.com/Jaeun-Choi98/modules/tcpnet/advanced/model"
)

type Parser interface {
	Parse(conn net.Conn) (tcpmd.ParseMsg, error)
}
