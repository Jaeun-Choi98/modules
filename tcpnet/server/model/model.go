package model

type Reply interface {
	GetPayload() any
	GetError() error
}

type ReplyCode interface {
	GetReplyCode() any
}

type GenericReplyCode[T any] struct {
	Code T
}

func NewReplyCode[T any](code T) *GenericReplyCode[T] {
	return &GenericReplyCode[T]{
		Code: code,
	}
}

func (r *GenericReplyCode[T]) GetReplyCode() any {
	return r.Code
}

type GenericReply[T any] struct {
	Payload T
	Err     error
}

func (r *GenericReply[T]) GetPayload() any {
	return r.Payload
}

func (r *GenericReply[T]) GetErr() error {
	return r.Err
}

type ParseMsg interface {
	GetClientId() uint32
	GetPacketId() any
	GetPacket() any
}

type GenericParseMsg[T, K any] struct {
	ClientId uint32
	packetId T
	packet   K
}

func (p *GenericParseMsg[T, K]) AddPacket(packetId T, packet K) *GenericParseMsg[T, K] {
	p.packet = packet
	p.packetId = packetId
	return p
}

func (p *GenericParseMsg[T, K]) GetClientId() uint32 {
	return p.ClientId
}

func (p *GenericParseMsg[T, K]) GetPacketId() any {
	return p.packetId
}

func (p *GenericParseMsg[T, K]) GetPacket() any {
	return p.packet
}
