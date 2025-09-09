package serializer

import "encoding/binary"

type BinaryWriter struct {
	buffer []byte
	order  binary.ByteOrder
}

func NewBinaryWriter(order binary.ByteOrder) *BinaryWriter {
	return &BinaryWriter{
		buffer: make([]byte, 0, 256),
		order:  order,
	}
}

func (w *BinaryWriter) WriteByteOne(value byte) *BinaryWriter {
	w.buffer = append(w.buffer, value)
	return w
}

func (w *BinaryWriter) WriteBytes(values []byte) *BinaryWriter {
	w.buffer = append(w.buffer, values...)
	return w
}

func (w *BinaryWriter) WriteUint16(value uint16) *BinaryWriter {
	bytes := make([]byte, 2)
	w.order.PutUint16(bytes, value)
	w.buffer = append(w.buffer, bytes...)
	return w
}

func (w *BinaryWriter) WriteUint32(value uint32) *BinaryWriter {
	bytes := make([]byte, 4)
	w.order.PutUint32(bytes, value)
	w.buffer = append(w.buffer, bytes...)
	return w
}

func (w *BinaryWriter) WriteFixedBytes(data []byte, size int) *BinaryWriter {
	fixed := make([]byte, size)
	copy(fixed, data)
	w.buffer = append(w.buffer, fixed...)
	return w
}

func (w *BinaryWriter) Bytes() []byte {
	return w.buffer
}

func (w *BinaryWriter) Reset() {
	w.buffer = w.buffer[:0]
}
