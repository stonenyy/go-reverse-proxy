package logger

import (
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

// Init 初始化日志系统，按天轮换日志文件并写入 logs 目录。
func Init(logDir string) error {
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return err
	}

	writer, err := newDailyFileWriter(logDir)
	if err != nil {
		return err
	}

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
