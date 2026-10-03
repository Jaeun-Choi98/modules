package logger

import (
	"errors"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"time"
)

// cleanupLoop 는 기동 직후 한 번, 이후 cleanupInterval 마다 오래된 날짜 디렉터리를 지운다.
func (l *Logger) cleanupLoop() {
	defer close(l.done)
	t := time.NewTicker(cleanupInterval)
	defer t.Stop()
	for {
		l.cleanupOnce()
		select {
		case <-l.stop:
			return
		case <-t.C:
		}
	}
}

func (l *Logger) cleanupOnce() {
	defer func() {
		if r := recover(); r != nil {
			l.Errorf("[logger] cleanup panic recovered: %v\n%s", r, debug.Stack())
		}
	}()
	n, err := removeOldDays(l.opts.Dir, l.now(), l.opts.MaxAgeDays)
	if n > 0 {
		l.Infof("[logger] cleanup: %d day directories removed", n)
	}
	if err != nil {
		l.Warnf("[logger] cleanup: %v", err)
	}
}

// removeOldDays 는 <root>/YYYY/MM/D 구조의 디렉터리만 보고, 오늘에서 keepDays 일 전보다 오래된
// 날짜 디렉터리를 통째로 지운다. 이 구조가 아닌 파일·디렉터리는 건드리지 않는다.
// 비게 된 월/연 디렉터리도 지운다. 지운 날짜 디렉터리 수를 돌려준다.
func removeOldDays(root string, now time.Time, keepDays int) (int, error) {
	loc := now.Location()
	cutoff := time.Date(now.Year(), now.Month(), now.Day()-keepDays, 0, 0, 0, 0, loc)

	years, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}

	var errs []error
	removed := 0
	for _, ye := range years {
		y, ok := dirNum(ye, 1, 9999)
		if !ok {
			continue
		}
		yDir := filepath.Join(root, ye.Name())
		months, err := os.ReadDir(yDir)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, me := range months {
			m, ok := dirNum(me, 1, 12)
			if !ok {
				continue
			}
			mDir := filepath.Join(yDir, me.Name())
			days, err := os.ReadDir(mDir)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			for _, de := range days {
				d, ok := dirNum(de, 1, 31)
				if !ok {
					continue
				}
				date := time.Date(y, time.Month(m), d, 0, 0, 0, 0, loc)
				if date.Day() != d || !date.Before(cutoff) { // 2월 31일 같은 이름은 무시
					continue
				}
				if err := os.RemoveAll(filepath.Join(mDir, de.Name())); err != nil {
					errs = append(errs, err)
					continue
				}
				removed++
			}
			removeIfEmpty(mDir)
		}
		removeIfEmpty(yDir)
	}
	return removed, errors.Join(errs...)
}

func dirNum(e os.DirEntry, min, max int) (int, bool) {
	if !e.IsDir() {
		return 0, false
	}
	name := e.Name()
	for _, c := range name {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(name)
	if err != nil || n < min || n > max {
		return 0, false
	}
	return n, true
}

func removeIfEmpty(dir string) {
	if ents, err := os.ReadDir(dir); err == nil && len(ents) == 0 {
		os.Remove(dir)
	}
}
