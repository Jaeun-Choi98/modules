package shell

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type ScriptResult struct {
	ExitCode int
	Stdout   []string
	Stderr   []string
	Duration time.Duration
	Error    error
}

type ScriptExecutor struct {
	scriptsDir string
}

func NewScriptExecutor(dir string) *ScriptExecutor {
	os.MkdirAll(dir, 0755)
	return &ScriptExecutor{scriptsDir: dir}
}

func (se *ScriptExecutor) Execute(ctx context.Context, shellName, scriptName, content string, autoDelete bool) (*ScriptResult, error) {
	start := time.Now()
	result := &ScriptResult{
		Stdout: make([]string, 0),
		Stderr: make([]string, 0),
	}

	scriptPath := filepath.Join(se.scriptsDir, scriptName)
	if err := os.WriteFile(scriptPath, []byte(content), 0755); err != nil {
		result.Error = err
		return result, err
	}
	defer func() {
		if autoDelete {
			os.Remove(scriptPath)
		}

	}()

	// 명령 생성
	cmd := exec.CommandContext(ctx, shellName, scriptPath)

	// stdout 파이프
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		result.Error = err
		return result, err
	}

	// stderr 파이프
	stderr, err := cmd.StderrPipe()
	if err != nil {
		result.Error = err
		return result, err
	}

	// 실행
	if err := cmd.Start(); err != nil {
		result.Error = err
		return result, err
	}

	// stdout 읽기
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			result.Stdout = append(result.Stdout, scanner.Text())
		}
	}()

	// stderr 읽기
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			result.Stderr = append(result.Stderr, scanner.Text())
		}
	}()

	// 완료 대기
	err = cmd.Wait()
	result.Duration = time.Since(start)
	result.ExitCode = cmd.ProcessState.ExitCode()
	result.Error = err

	return result, nil
}
