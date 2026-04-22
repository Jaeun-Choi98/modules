package serialparser

import (
	serialmd "github.com/Jaeun-Choi98/modules/serialport/advanced/model"
	"go.bug.st/serial"
)

type Parser interface {
	Parse(port serial.Port) (serialmd.ParseMsg, error)
}
