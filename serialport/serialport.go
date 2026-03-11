package serialport

import (
	"context"
	"fmt"
	"sync"

	"go.bug.st/serial"
)

type SerialBase struct {
	port serial.Port

	portName   string
	serialMode *serial.Mode

	handleConnectFunc func(port serial.Port)

	wg     sync.WaitGroup
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.RWMutex
}

func NewSerialBase(ctx context.Context) *SerialBase {
	nctx, ncancel := context.WithCancel(ctx)

	return &SerialBase{
		ctx:    nctx,
		cancel: ncancel,
	}
}

func (s *SerialBase) Shutdown() error {
	s.cancel()
	s.wg.Wait()
	return s.port.Close()
}

func (s *SerialBase) SetPortAndMode(portName string, mode *serial.Mode) {
	s.mu.Lock()
	s.portName = portName
	s.serialMode = mode
	s.mu.Unlock()
}

func (s *SerialBase) SetHandleConnectFunc(f func(port serial.Port)) {
	s.mu.Lock()
	s.handleConnectFunc = f
	s.mu.Unlock()
}

func (s *SerialBase) Connect() error {
	p, err := serial.Open(s.portName, s.serialMode)
	if err != nil {
		return fmt.Errorf("failed to open port, error: %v", err)
	}
	s.port = p
	return nil
}

func (s *SerialBase) Start() error {
	if s.handleConnectFunc == nil {
		return fmt.Errorf("handle connection func is nil")
	}

	if err := s.Connect(); err != nil {
		return err
	}

	s.handleConnectFunc(s.port)

	return nil
}

func (s *SerialBase) WriteAll(data []byte) error {
	written := 0
	for written < len(data) {
		n, err := s.port.Write(data[written:])
		if err != nil {
			return err
		}
		written += n
	}
	return nil
}
