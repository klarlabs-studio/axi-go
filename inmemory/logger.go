package inmemory

import (
	"fmt"
	"log"
	"os"
	"strings"

	"go.klarlabs.de/axi/domain"
)

// Compile-time interface check.
var _ domain.Logger = (*StdLogger)(nil)

// StdLogger implements domain.Logger using the stdlib log package.
type StdLogger struct {
	logger *log.Logger
	level  LogLevel
}

// LogLevel controls the minimum severity for output.
type LogLevel int

const (
	LevelDebug LogLevel = iota
	LevelInfo
	LevelWarn
	LevelError
)

// NewStdLogger creates a StdLogger that writes to stderr.
func NewStdLogger(level LogLevel) *StdLogger {
	return &StdLogger{
		logger: log.New(os.Stderr, "", log.LstdFlags),
		level:  level,
	}
}

func (l *StdLogger) Debug(msg string, fields ...domain.Field) {
	if l.level <= LevelDebug {
		l.log("DEBUG", msg, fields)
	}
}

func (l *StdLogger) Info(msg string, fields ...domain.Field) {
	if l.level <= LevelInfo {
		l.log("INFO", msg, fields)
	}
}

func (l *StdLogger) Warn(msg string, fields ...domain.Field) {
	if l.level <= LevelWarn {
		l.log("WARN", msg, fields)
	}
}

func (l *StdLogger) Error(msg string, fields ...domain.Field) {
	if l.level <= LevelError {
		l.log("ERROR", msg, fields)
	}
}

func (l *StdLogger) log(level, msg string, fields []domain.Field) {
	if len(fields) == 0 {
		l.logger.Printf("[%s] %s", level, msg)
		return
	}
	pairs := make([]string, len(fields))
	for i, f := range fields {
		pairs[i] = fmt.Sprintf("%s=%v", f.Key, f.Value)
	}
	l.logger.Printf("[%s] %s %s", level, msg, strings.Join(pairs, " "))
}
