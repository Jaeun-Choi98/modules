
## logger

날짜별 디렉터리 로그(app.log) + 프레임 hex 덤프(dump.log), 보존 기간 자동 정리

```go
l, err := logger.Init(logger.Options{
    Dir:        "log",      // log/YYYY/MM/D/app.log, dump.log
    Level:      lv,         // logger.ParseLevel(cfg.Log.Level)
    Dump:       cfg.Log.Dump,
    MaxAgeDays: 3,          // 오늘 + 지난 3일 보존. 음수면 정리 안 함
    Console:    os.Stderr,  // nil 이면 콘솔 출력 없음
})
if err != nil { ... }
defer logger.Close()

logger.Infof("started")
logger.Dump("TCP", logger.Tx, clientID, op, frame)
logger.SetLevel(logger.DEBUG) // 실행 중 변경 (저장 안 됨)
```

인스턴스를 직접 들고 써도 된다 (`l.Infof`, `l.Dump` …). 한 프로세스에서 로그 디렉터리를 나누고 싶을 때 쓴다

```go
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
```

다른 라이브러리의 로그를 app.log로 모으는 어댑터 리시버도 지원

---
