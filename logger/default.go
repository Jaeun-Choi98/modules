package logger

import "sync/atomic"

// 패키지 기본 로거. 등록 전에는 nil 이고, 이때 패키지 함수는 표준 log 로 대체 출력한다.
var std atomic.Pointer[Logger]

// Init 은 New 로 로거를 만들어 기본 로거로 등록한다.
func Init(opts Options) (*Logger, error) {
	l, err := New(opts)
	if err != nil {
		return nil, err
	}
	SetDefault(l)
	return l, nil
}

// SetDefault 는 기본 로거를 바꾼다. 이전 로거는 닫지 않는다.
func SetDefault(l *Logger) { std.Store(l) }

// Default 는 기본 로거를 돌려준다. 등록 전이면 nil 이다 (nil 이어도 메서드 호출은 안전하다).
func Default() *Logger { return std.Load() }

// Close 는 기본 로거를 닫는다.
func Close() error { return std.Load().Close() }

func SetLevel(lv Level)     { std.Load().SetLevel(lv) }
func GetLevel() Level       { return std.Load().Level() }
func Enabled(lv Level) bool { return std.Load().Enabled(lv) }
func SetDump(on bool)       { std.Load().SetDump(on) }
func IsDumpEnabled() bool   { return std.Load().DumpEnabled() }

// 아래 함수들은 호출 깊이를 맞추려고 메서드가 아니라 output/dumpFrame 을 바로 부른다.

func Debugf(format string, v ...any) { std.Load().output(DEBUG, false, format, v) }
func Debugln(v ...any)               { std.Load().output(DEBUG, true, "", v) }
func Infof(format string, v ...any)  { std.Load().output(INFO, false, format, v) }
func Infoln(v ...any)                { std.Load().output(INFO, true, "", v) }
func Warnf(format string, v ...any)  { std.Load().output(WARN, false, format, v) }
func Warnln(v ...any)                { std.Load().output(WARN, true, "", v) }
func Errorf(format string, v ...any) { std.Load().output(ERROR, false, format, v) }
func Errorln(v ...any)               { std.Load().output(ERROR, true, "", v) }

func Dump(link string, dir Direction, peer any, opCode byte, frame []byte) {
	std.Load().dumpFrame(link, dir, peer, true, opCode, frame)
}

func DumpRaw(link string, dir Direction, peer any, frame []byte) {
	std.Load().dumpFrame(link, dir, peer, false, 0, frame)
}
