package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type dailyFileWriter struct {
	mu          sync.Mutex
	dir         string
	currentDate string
	file        *os.File
}

var currentWriter *dailyFileWriter

// Init 初始化日志系统：
// - 相对路径基于可执行文件所在目录进行解析
// - 如果无法创建该目录，会依次回退到 /var/log/go-reverse-proxy 或临时目录
// - 日志同时写入 stdout 和文件，按天轮换
func Init(logDir string) error {
	if logDir == "" {
		logDir = "logs"
	}

	// 如果是相对路径，基于可执行文件目录
	if !filepath.IsAbs(logDir) {
		if exe, err := os.Executable(); err == nil {
			base := filepath.Dir(exe)
			logDir = filepath.Join(base, logDir)
		} else if cwd, err := os.Getwd(); err == nil {
			logDir = filepath.Join(cwd, logDir)
		}
	}

	// 尝试创建目录，若失败则尝试回退位置
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		alt := "/var/log/go-reverse-proxy"
		if err2 := os.MkdirAll(alt, 0o755); err2 == nil {
			logDir = alt
		} else {
			alt2 := filepath.Join(os.TempDir(), "go-reverse-proxy-logs")
			if err3 := os.MkdirAll(alt2, 0o755); err3 == nil {
				logDir = alt2
			} else {
				return fmt.Errorf("无法创建日志目录: %v", err)
			}
		}
	}

	writer, err := newDailyFileWriter(logDir)
	if err != nil {
		return err
	}

	log.Printf("日志写入目录: %s", logDir)

	log.SetOutput(io.MultiWriter(os.Stdout, writer))
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.SetPrefix("")
	currentWriter = writer

	return nil
}

// Close 关闭当前日志文件。
func Close() error {
	if currentWriter == nil {
		return nil
	}
	return currentWriter.Close()
}

func newDailyFileWriter(logDir string) (*dailyFileWriter, error) {
	w := &dailyFileWriter{dir: logDir}
	if err := w.rotate(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *dailyFileWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.needsRotate() {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}
	return w.file.Write(p)
}

func (w *dailyFileWriter) needsRotate() bool {
	return w.file == nil || time.Now().Format("2006-01-02") != w.currentDate
}

func (w *dailyFileWriter) rotate() error {
	if w.file != nil {
		_ = w.file.Close()
	}

	date := time.Now().Format("2006-01-02")
	path := filepath.Join(w.dir, date+".log")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}

	w.file = file
	w.currentDate = date
	return nil
}

func (w *dailyFileWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	return w.file.Close()
}
