package log

import (
	"fmt"
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

var (
	_logger *zap.Logger
)

func InitLog(path string, verbose bool) {
	level := zapcore.InfoLevel
	if verbose {
		level = zapcore.DebugLevel
	}
	leveler := zap.LevelEnablerFunc(func(lvl zapcore.Level) bool {
		return lvl >= level
	})
	var cores []zapcore.Core
	// 文本
	fencoder := zapcore.NewJSONEncoder(zap.NewDevelopmentEncoderConfig())
	fsyncer := zapcore.AddSync(&lumberjack.Logger{
		Filename:   path,
		MaxSize:    500, // megabytes
		MaxBackups: 20,
		MaxAge:     28,    //days
		Compress:   false, // disabled by default
		LocalTime:  true,
	})
	cores = append(cores, zapcore.NewCore(fencoder, fsyncer, leveler))

	if verbose {
		// 命令行
		cencoder := zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig())
		csyncer := zapcore.AddSync(os.Stdout)
		cores = append(cores, zapcore.NewCore(cencoder, csyncer, leveler))
	}
	core := zapcore.NewTee(cores...)
	_logger = zap.New(core, zap.AddCallerSkip(1), zap.AddCaller())
}

func With(field ...zapcore.Field) *zap.Logger {
	return _logger.With(field...)
}

func Debug(msg string, field ...zapcore.Field) {
	_logger.Debug(msg, field...)
}

func Info(msg string, field ...zapcore.Field) {
	_logger.Info(msg, field...)
}

func Warn(msg string, field ...zapcore.Field) {
	_logger.Warn(msg, field...)
}

func Error(msg string, field ...zapcore.Field) {
	_logger.Error(msg, field...)
}

func Fatal(msg string, field ...zapcore.Field) {
	_logger.Fatal(msg, field...)
}

func Sync() {
	if err := _logger.Sync(); err != nil {
		fmt.Fprintf(os.Stderr, "logger sync err: %v\n", err)
	}
}

type LogArrayStringWraper struct {
	V []string
}

func (w LogArrayStringWraper) MarshalLogArray(enc zapcore.ArrayEncoder) error {
	if len(w.V) > 0 {
		for _, i := range w.V {
			enc.AppendString(i)
		}
	}
	return nil
}

func ArrayString(name string, values []string) zap.Field {
	return zap.Array(name, LogArrayStringWraper{V: values})
}
