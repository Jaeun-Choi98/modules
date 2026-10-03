package logger

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeClock struct{ t atomic.Pointer[time.Time] }

func newClock(t time.Time) *fakeClock { c := &fakeClock{}; c.set(t); return c }
func (c *fakeClock) set(t time.Time)  { c.t.Store(&t) }
func (c *fakeClock) now() time.Time   { return *c.t.Load() }

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestLevelParseAndString(t *testing.T) {
	for _, lv := range []Level{DEBUG, INFO, WARN, ERROR} {
		got, err := ParseLevel(" " + strings.ToLower(lv.String()) + " ")
		if err != nil || got != lv {
			t.Fatalf("ParseLevel(%v) = %v, %v", lv, got, err)
		}
	}
	if _, err := ParseLevel("trace"); err == nil {
		t.Fatal("want error")
	}
	if Level(0) != INFO {
		t.Fatal("zero level must be INFO")
	}
}

func TestLevelFilterAndRotation(t *testing.T) {
	dir := t.TempDir()
	clk := newClock(time.Date(2026, 10, 3, 23, 59, 59, 0, time.Local))
	var console bytes.Buffer
	l, err := newLogger(Options{Dir: dir, MaxAgeDays: -1, Console: &console}, clk.now)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	l.Debugf("hidden")
	l.Infof("day1 %d", 1)
	clk.set(time.Date(2026, 10, 4, 0, 0, 0, 1, time.Local))
	l.Warnln("day2", 2)

	d1 := read(t, filepath.Join(dir, "2026", "10", "3", "app.log"))
	d2 := read(t, filepath.Join(dir, "2026", "10", "4", "app.log"))
	if strings.Contains(d1, "hidden") || !strings.Contains(d1, "> [INFO ] day1 1") {
		t.Fatalf("day1: %q", d1)
	}
	if !strings.Contains(d2, "> [WARN ] day2 2") || strings.Contains(d2, "day1") {
		t.Fatalf("day2: %q", d2)
	}
	if !strings.Contains(console.String(), "day1") || !strings.Contains(console.String(), "day2") {
		t.Fatalf("console: %q", console.String())
	}

	l.SetLevel(DEBUG)
	l.Debugf("now visible")
	if !strings.Contains(read(t, filepath.Join(dir, "2026", "10", "4", "app.log")), "now visible") {
		t.Fatal("SetLevel not applied")
	}
}

func TestDump(t *testing.T) {
	dir := t.TempDir()
	clk := newClock(time.Date(2026, 9, 13, 20, 16, 0, 0, time.Local))
	l, err := newLogger(Options{Dir: dir, MaxAgeDays: -1}, clk.now)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	dumpPath := filepath.Join(dir, "2026", "09", "13", "dump.log")
	l.Dump("TCP", Tx, 5001, 0x21, []byte{1, 2})
	if _, err := os.Stat(dumpPath); !os.IsNotExist(err) {
		t.Fatal("dump.log must not be created while dump is off")
	}

	l.SetDump(true)
	frame := make([]byte, 18)
	for i := range frame {
		frame[i] = byte(0xF0 + i)
	}
	l.Dump("TCP", Tx, 5001, 0x21, frame)
	l.DumpRaw("SERIAL", Rx, "COM3", nil)

	got := read(t, dumpPath)
	want := []string{
		"[TCP] [Tx] peer=5001 op=0x21 len=18\n" +
			"  0000: F0 F1 F2 F3 F4 F5 F6 F7 F8 F9 FA FB FC FD FE FF\n" +
			"  0010: 00 01\n",
		"[SERIAL] [Rx] peer=COM3 len=0 (body omitted)\n",
	}
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Fatalf("dump missing %q in\n%s", w, got)
		}
	}
}

func TestCallerDepth(t *testing.T) {
	dir := t.TempDir()
	l, err := New(Options{Dir: dir, MaxAgeDays: -1, Caller: true})
	if err != nil {
		t.Fatal(err)
	}
	SetDefault(l)
	defer func() { SetDefault(nil); l.Close() }()

	l.Infof("method")
	Infof("pkgfunc")
	l.SetDump(true)
	Dump("UDP", Rx, "x", 1, []byte{1})

	now := time.Now()
	app := read(t, filepath.Join(dayDir(dir, now), "app.log"))
	dump := read(t, filepath.Join(dayDir(dir, now), "dump.log"))
	for _, s := range []string{app, dump} {
		for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
			if strings.HasPrefix(line, "  ") {
				continue
			}
			if !strings.Contains(line, "logger_test.go:") {
				t.Fatalf("caller should be the test file: %q", line)
			}
		}
	}
}

func TestNilDefaultIsSafe(t *testing.T) {
	SetDefault(nil)
	SetLevel(DEBUG)
	SetDump(true)
	Infof("goes to std log")
	Dump("TCP", Rx, 1, 1, []byte{1})
	if GetLevel() != INFO || IsDumpEnabled() {
		t.Fatal("nil default must report INFO / dump off")
	}
	if err := Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCloseIdempotentAndDropsAfterClose(t *testing.T) {
	l, err := New(Options{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	l.Close()
	l.Close()
	l.Infof("dropped") // panic 나면 안 된다
}

func TestRemoveOldDays(t *testing.T) {
	root := t.TempDir()
	mk := func(p ...string) {
		path := filepath.Join(append([]string{root}, p...)...)
		os.MkdirAll(path, 0o755)
		os.WriteFile(filepath.Join(path, "app.log"), []byte("x"), 0o644)
	}
	mk("2026", "09", "29") // 지울 대상
	mk("2026", "09", "30") // 경계: 남는다 (오늘 10/3, 3일 보존)
	mk("2026", "10", "3")
	mk("2025", "12", "31") // 지울 대상 → 2025 디렉터리까지 비워져서 삭제
	mk("2026", "09", "notaday")
	mk("2026", "02", "31") // 존재할 수 없는 날짜 → 무시
	os.WriteFile(filepath.Join(root, "keep.txt"), []byte("x"), 0o644)

	now := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	n, err := removeOldDays(root, now, 3)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("removed %d, want 2", n)
	}
	gone := []string{"2026/09/29", "2025"}
	kept := []string{"2026/09/30", "2026/10/3", "2026/09/notaday", "2026/02/31", "keep.txt"}
	for _, p := range gone {
		if _, err := os.Stat(filepath.Join(root, p)); !os.IsNotExist(err) {
			t.Errorf("%s should be removed", p)
		}
	}
	for _, p := range kept {
		if _, err := os.Stat(filepath.Join(root, p)); err != nil {
			t.Errorf("%s should be kept: %v", p, err)
		}
	}
}

func TestConcurrentWritesDuringRotation(t *testing.T) {
	dir := t.TempDir()
	clk := newClock(time.Date(2026, 10, 3, 23, 59, 0, 0, time.Local))
	l, err := newLogger(Options{Dir: dir, MaxAgeDays: -1, Dump: true}, clk.now)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				l.Infof("g%d i%d", g, i)
				l.Dump("TCP", Rx, g, 1, []byte{byte(i)})
				if i == 100 && g == 0 {
					clk.set(time.Date(2026, 10, 4, 0, 0, 0, 0, time.Local))
				}
				if i == 150 && g == 1 {
					l.SetLevel(WARN)
					l.SetLevel(INFO)
				}
			}
		}()
	}
	wg.Wait()
}
