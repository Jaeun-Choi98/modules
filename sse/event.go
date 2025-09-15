package sse

type Event interface {
	GetEvent() any
	GetData() any
	GetId() any
	GetRetry() any
}
