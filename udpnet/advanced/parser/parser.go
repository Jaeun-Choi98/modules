package parser

import (
	"net"

	udpmd "github.com/Jaeun-Choi98/modules/udpnet/advanced/model"
)

type Parser interface {
	Parse(data []byte, addr *net.UDPAddr) (udpmd.ParseMsg, error)
}
