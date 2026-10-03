package logger

import (
	"fmt"
	"strings"
)

// Direction 은 프레임 방향이다.
type Direction string

const (
	Rx Direction = "Rx"
	Tx Direction = "Tx"
)

const dumpBytesPerLine = 16

const hexDigits = "0123456789ABCDEF"

// Dump 는 한 프레임을 dump.log 에 16바이트씩 끊어 기록한다.
//
//	2026/09/13 20:16:00.123456 [TCP] [Tx] peer=5001 op=0x21 len=12
//	  0000: 7E 7E 21 00 05 00 00 13 89 01 8C 7F
//
// link 는 "TCP"/"UDP"/"SERIAL" 같은 링크 이름, peer 는 client id 나 원격 주소처럼 상대를 구분할 값이다.
// frame 이 nil 이면 헤더만 남긴다 (파일 전송처럼 본문이 큰 프레임).
func (l *Logger) Dump(link string, dir Direction, peer any, opCode byte, frame []byte) {
	l.dumpFrame(link, dir, peer, true, opCode, frame)
}

// DumpRaw 는 op 코드 개념이 없는 프로토콜용 Dump 다.
func (l *Logger) DumpRaw(link string, dir Direction, peer any, frame []byte) {
	l.dumpFrame(link, dir, peer, false, 0, frame)
}

// 호출 깊이: 사용자 → Dump/DumpRaw → dumpFrame → Output
func (l *Logger) dumpFrame(link string, dir Direction, peer any, hasOp bool, op byte, frame []byte) {
	if !l.DumpEnabled() {
		return
	}

	var sb strings.Builder
	lines := (len(frame) + dumpBytesPerLine - 1) / dumpBytesPerLine
	sb.Grow(64 + lines*9 + len(frame)*3)

	fmt.Fprintf(&sb, "[%s] [%s] peer=%v", link, dir, peer)
	if hasOp {
		fmt.Fprintf(&sb, " op=0x%02X", op)
	}
	fmt.Fprintf(&sb, " len=%d", len(frame))
	if frame == nil {
		sb.WriteString(" (body omitted)")
	}
	for i, b := range frame {
		if i%dumpBytesPerLine == 0 {
			fmt.Fprintf(&sb, "\n  %04X:", i)
		}
		sb.WriteByte(' ')
		sb.WriteByte(hexDigits[b>>4])
		sb.WriteByte(hexDigits[b&0x0F])
	}
	l.dumper.Output(3, sb.String())
}
