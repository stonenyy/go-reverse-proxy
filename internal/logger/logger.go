package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var Log *zap.Logger

// Init 初始化日志系统
func Init(logDir string) error {
	if envDir := os.Getenv("LOG_DIR"); envDir != "" {
		logDir = envDir
	}
	if logDir == "" {
		logDir = "/var/log/go-reverse-proxy"
	}

	if !filepath.IsAbs(logDir) {
		cwd, _ := os.Getwd()
		logDir = filepath.Join(cwd, logDir)
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

	// 配置 Zap
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	encoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder

	// 日志文件路径 (按天)
	filePath := filepath.Join(logDir, time.Now().Format("2006-01-02")+".log")
	file, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("无法打开日志文件: %v", err)
	}

	core := zapcore.NewTee(
		zapcore.NewCore(zapcore.NewJSONEncoder(encoderConfig), zapcore.AddSync(os.Stdout), zap.InfoLevel),
		zapcore.NewCore(zapcore.NewJSONEncoder(encoderConfig), zapcore.AddSync(file), zap.InfoLevel),
	)

	Log = zap.New(core, zap.AddCaller())
	return nil
}

func Close() {
	if Log != nil {
		_ = Log.Sync()
	}
}
