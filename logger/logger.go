// Package logger 는 날짜별 디렉터리(<Dir>/YYYY/MM/D/)에 두 개의 파일을 남기는 로거다.
//
//	app.log  - 애플리케이션 로그. 레벨(DEBUG < INFO < WARN < ERROR)로 거른다.
//	dump.log - TCP/UDP/Serial 프레임 hex 덤프. 레벨과 별개로 켜고 끈다.
//
// 날짜가 바뀌면 다음 쓰기부터 새 날짜 디렉터리의 파일로 넘어간다 (자정 직후 첫 줄부터 정확히 분리).
// 보존 기간이 지난 날짜 디렉터리는 백그라운드 고루틴이 1시간 주기로 지운다.
//
// 사용 방식은 두 가지다.
//
//	l, err := logger.New(opts)    // 인스턴스를 직접 들고 쓰기
//	l.Infof("...")
//
//	logger.Init(opts)             // 패키지 기본 로거로 등록하고
//	logger.Infof("...")           // 어디서든 패키지 함수로 쓰기
//
// 기본 로거가 등록되기 전(설정 파일을 읽는 동안 등)의 패키지 함수 호출은
// 표준 log(표준에러)로 나가며 INFO 이상만 남는다. 덤프는 버려진다.
package logger

import (
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Level 은 로그 레벨이다. 0 값이 INFO 라서 Options{} 의 기본 레벨은 INFO 다.
type Level int32

const (
	DEBUG Level = iota - 1
	INFO
	WARN
	ERROR
)

var levelNames = [...]string{"DEBUG", "INFO", "WARN", "ERROR"}

func (lv Level) String() string {
	if lv < DEBUG || lv > ERROR {
		return fmt.Sprintf("Level(%d)", int32(lv))
	}
	return levelNames[lv-DEBUG]
}

// ParseLevel 은 "debug", "INFO" 같은 문자열을 레벨로 바꾼다. 대소문자와 앞뒤 공백은 무시한다.
func ParseLevel(s string) (Level, error) {
	s = strings.TrimSpace(s)
	for i, name := range levelNames {
		if strings.EqualFold(s, name) {
			return DEBUG + Level(i), nil
		}
	}
	return INFO, fmt.Errorf("logger: unknown level %q", s)
}

const (
	DefaultDir        = "log"
	DefaultMaxAgeDays = 3
	defaultAppName    = "app.log"
	defaultDumpName   = "dump.log"
	cleanupInterval   = time.Hour
)

// Options 는 로거 설정이다. 0 값 필드는 기본값을 쓴다.
type Options struct {
	Dir    string // 로그 루트 디렉터리. 기본 "log"
	Prefix string // 각 줄 맨 앞에 붙는 문자열
	Level  Level  // 기본 INFO
	Dump   bool   // dump.log 기록 여부

	// MaxAgeDays 는 오늘을 빼고 며칠 전까지의 날짜 디렉터리를 남길지 정한다.
	// 0 이면 DefaultMaxAgeDays(3), 음수면 자동 정리를 하지 않는다.
	MaxAgeDays int

	// Console 이 nil 이 아니면 app.log 내용을 여기에도 쓴다 (예: os.Stderr).
	// 덤프는 콘솔로 나가지 않는다.
	Console io.Writer

	// Caller 가 true 면 호출 위치(file.go:123)를 줄에 남긴다.
	Caller bool

	AppFileName  string // 기본 "app.log"
	DumpFileName string // 기본 "dump.log"
}

func (o *Options) setDefaults() {
	if o.Dir == "" {
		o.Dir = DefaultDir
	}
	if o.MaxAgeDays == 0 {
		o.MaxAgeDays = DefaultMaxAgeDays
	}
	if o.AppFileName == "" {
		o.AppFileName = defaultAppName
	}
	if o.DumpFileName == "" {
		o.DumpFileName = defaultDumpName
	}
}

// Logger 는 여러 고루틴에서 동시에 써도 안전하다.
// 모든 메서드는 nil 수신자에서도 동작한다 (표준 log 로 대체 출력).
type Logger struct {
	opts  Options
	now   func() time.Time
	level atomic.Int32
	dump  atomic.Bool

	appFile  *dailyFile
	dumpFile *dailyFile
	app      *log.Logger
	dumper   *log.Logger

	stop      chan struct{}
	done      chan struct{}
	closeOnce sync.Once
	closeErr  error
}

// New 는 로거를 만들고 app.log 를 바로 연다 (디렉터리 권한 문제를 기동 시점에 잡기 위해).
// dump.log 는 첫 덤프가 기록될 때 만든다.
// MaxAgeDays >= 0 이면 정리 고루틴을 띄운다. 다 쓰면 Close 를 부른다.
func New(opts Options) (*Logger, error) {
	return newLogger(opts, time.Now)
}

func newLogger(opts Options, now func() time.Time) (*Logger, error) {
	opts.setDefaults()

	l := &Logger{
		opts:     opts,
		now:      now,
		appFile:  &dailyFile{root: opts.Dir, name: opts.AppFileName, now: now},
		dumpFile: &dailyFile{root: opts.Dir, name: opts.DumpFileName, now: now},
	}
	l.level.Store(int32(opts.Level))
	l.dump.Store(opts.Dump)

	if err := l.appFile.open(); err != nil {
		return nil, err
	}

	flags := log.Ldate | log.Ltime | log.Lmicroseconds
	if opts.Caller {
		flags |= log.Lshortfile
	}

	var appW io.Writer = l.appFile
	if opts.Console != nil {
		appW = &tee{primary: l.appFile, secondary: opts.Console}
	}
	l.app = log.New(appW, opts.Prefix, flags)
	l.dumper = log.New(l.dumpFile, opts.Prefix, flags)

	if opts.MaxAgeDays >= 0 {
		l.stop = make(chan struct{})
		l.done = make(chan struct{})
		go l.cleanupLoop()
	}
	return l, nil
}

// tee 는 primary(파일) 결과를 돌려주고 secondary(콘솔) 오류는 무시한다.
type tee struct {
	primary   io.Writer
	secondary io.Writer
}

func (t *tee) Write(p []byte) (int, error) {
	t.secondary.Write(p)
	return t.primary.Write(p)
}

// Close 는 정리 고루틴을 멈추고 파일을 닫는다. 여러 번 불러도 된다.
// 닫은 뒤의 파일 쓰기는 버려진다 (Console 출력은 계속된다).
func (l *Logger) Close() error {
	if l == nil {
		return nil
	}
	l.closeOnce.Do(func() {
		if l.stop != nil {
			close(l.stop)
			<-l.done
		}
		l.closeErr = errors.Join(l.appFile.Close(), l.dumpFile.Close())
	})
	return l.closeErr
}

// ---- 레벨 / 덤프 설정 (실행 중 변경 가능) ----

func (l *Logger) SetLevel(lv Level) {
	if l != nil {
		l.level.Store(int32(lv))
	}
}

// Level 은 현재 레벨을 돌려준다. nil 이면 INFO.
func (l *Logger) Level() Level {
	if l == nil {
		return INFO
	}
	return Level(l.level.Load())
}

// Enabled 는 lv 레벨 로그가 기록되는지 알려준다.
// 인자를 만드는 비용이 큰 로그 앞에서 쓴다.
func (l *Logger) Enabled(lv Level) bool { return lv >= l.Level() }

func (l *Logger) SetDump(on bool) {
	if l != nil {
		l.dump.Store(on)
	}
}

// DumpEnabled 는 덤프가 켜져 있는지 알려준다. nil 이면 false.
func (l *Logger) DumpEnabled() bool { return l != nil && l.dump.Load() }

// ---- 애플리케이션 로그 ----

// output 의 호출 깊이는 사용자 → Infof(메서드든 패키지 함수든) → output → Output 으로 고정이다.
// 그래서 패키지 함수는 메서드를 거치지 말고 output 을 바로 불러야 한다.
func (l *Logger) output(lv Level, ln bool, format string, args []any) {
	if !l.Enabled(lv) {
		return
	}
	var msg string
	if ln {
		msg = fmt.Sprintln(args...)
	} else {
		msg = fmt.Sprintf(format, args...)
	}
	line := "> [" + padLevel(lv) + "] " + strings.TrimSuffix(msg, "\n")
	if l == nil {
		log.Output(3, line)
		return
	}
	l.app.Output(3, line)
}

func padLevel(lv Level) string {
	s := lv.String()
	if len(s) < 5 {
		s += strings.Repeat(" ", 5-len(s))
	}
	return s
}

func (l *Logger) Debugf(format string, v ...any) { l.output(DEBUG, false, format, v) }
func (l *Logger) Debugln(v ...any)               { l.output(DEBUG, true, "", v) }
func (l *Logger) Infof(format string, v ...any)  { l.output(INFO, false, format, v) }
func (l *Logger) Infoln(v ...any)                { l.output(INFO, true, "", v) }
func (l *Logger) Warnf(format string, v ...any)  { l.output(WARN, false, format, v) }
func (l *Logger) Warnln(v ...any)                { l.output(WARN, true, "", v) }
func (l *Logger) Errorf(format string, v ...any) { l.output(ERROR, false, format, v) }
func (l *Logger) Errorln(v ...any)               { l.output(ERROR, true, "", v) }

// Writer 는 쓰기 한 번을 lv 레벨 로그 한 줄로 남기는 io.Writer 를 돌려준다.
// gin.DefaultWriter, http.Server.ErrorLog 처럼 io.Writer / *log.Logger 를 받는 곳에 연결할 때 쓴다.
func (l *Logger) Writer(lv Level) io.Writer { return levelWriter{l: l, lv: lv} }

// StdLogger 는 Writer(lv) 로 쓰는 *log.Logger 를 돌려준다.
func (l *Logger) StdLogger(lv Level) *log.Logger { return log.New(l.Writer(lv), "", 0) }

type levelWriter struct {
	l  *Logger
	lv Level
}

func (w levelWriter) Write(p []byte) (int, error) {
	w.l.output(w.lv, false, "%s", []any{p})
	return len(p), nil
}
