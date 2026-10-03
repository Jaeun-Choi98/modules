package logger

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var errRetryLater = errors.New("logger: file open failed recently, retrying later")

// dailyFile 은 쓰기마다 날짜를 보고, 날짜가 바뀌었으면 새 날짜 디렉터리의 파일로 갈아 끼우는 writer 다.
// 파일 열기에 실패하면 이전 파일에 계속 쓰고 1분 뒤에 다시 시도한다.
type dailyFile struct {
	root string
	name string
	now  func() time.Time

	mu      sync.Mutex
	f       *os.File
	day     int // f 의 날짜 (yyyymmdd)
	retryAt time.Time
	closed  bool
}

func dayKey(t time.Time) int {
	y, m, d := t.Date()
	return y*10000 + int(m)*100 + d
}

// dayDir 은 <root>/YYYY/MM/D 다. 기존 운영 디렉터리와 호환되도록 일(day)은 0 을 채우지 않는다.
func dayDir(root string, t time.Time) string {
	return filepath.Join(root,
		fmt.Sprintf("%d", t.Year()),
		fmt.Sprintf("%02d", int(t.Month())),
		fmt.Sprintf("%d", t.Day()),
	)
}

func (d *dailyFile) Write(p []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return len(p), nil
	}
	now := d.now()
	if k := dayKey(now); d.f == nil || k != d.day {
		if err := d.rotateLocked(now, false); err != nil && d.f == nil {
			return 0, err
		}
	}
	return d.f.Write(p)
}

// open 은 지금 날짜의 파일을 바로 연다 (재시도 대기 없이).
func (d *dailyFile) open() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.rotateLocked(d.now(), true)
}

func (d *dailyFile) rotateLocked(now time.Time, force bool) error {
	if !force && now.Before(d.retryAt) {
		return errRetryLater
	}
	dir := dayDir(d.root, now)
	f, err := func() (*os.File, error) {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
		return os.OpenFile(filepath.Join(dir, d.name), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	}()
	if err != nil {
		d.retryAt = now.Add(time.Minute)
		fmt.Fprintf(os.Stderr, "logger: open %s: %v\n", filepath.Join(dir, d.name), err)
		return err
	}
	if d.f != nil {
		d.f.Close()
	}
	d.f, d.day, d.retryAt = f, dayKey(now), time.Time{}
	return nil
}

func (d *dailyFile) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = true
	if d.f == nil {
		return nil
	}
	err := d.f.Close()
	d.f = nil
	return err
}
