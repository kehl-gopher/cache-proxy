package utils

import (
	js "encoding/json"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"sync"

	"github.com/sirupsen/logrus"
	"gopkg.in/natefinch/lumberjack.v2"
)

var logsOutputFile = "logs/cache-proxy.log"

type LogsLevel int

const (
	InfoLevel LogsLevel = iota + 1
	DebugLevel
	WarnLevel
	ErrorLevel
	FatalLevel
)

type handler string

var (
	text handler = "text"
	json handler = "json"
)

type Logs struct {
	mu   *sync.RWMutex
	logs *logrus.Logger
}

type BracketFormater struct{}

func (b *BracketFormater) Format(entry *logrus.Entry) ([]byte, error) {
	timestamp := entry.Time.Format("2006-01-02 15:04:05")
	level := entry.Level
	msg := entry.Message

	var jsonData []byte
	var err error
	if len(entry.Data) > 0 {
		jsonData, err = js.Marshal(entry.Data)

		if err != nil {
			return nil, err
		}
	} else {
		jsonData = []byte{}
	}
	if len(jsonData) > 0 {
		jsd := fmt.Sprintf("[%s] - [%s]: %s %s\n", timestamp, level, msg, string(jsonData))
		return []byte(jsd), nil
	}
	log := fmt.Sprintf("[%s] - [%s]: %s\n", timestamp, level, msg)
	return []byte(log), nil
}
func NewLogs() *Logs {
	logs := logrus.New()

	logs.SetFormatter(&BracketFormater{})
	logger := &lumberjack.Logger{
		Filename:   logsOutputFile,
		MaxSize:    5, // megabytes
		MaxBackups: 3,
		MaxAge:     28,
		Compress:   true,
	}

	multiWriter := io.MultiWriter(os.Stdout, logger)
	logrus.SetOutput(multiWriter)

	return &Logs{
		logs: logs,
		mu:   &sync.RWMutex{},
	}
}

func (l *Logs) Debug(message ...interface{}) {
	l.logs.Debug(message...)
}

func (l *Logs) Info(message ...interface{}) {
	l.logs.Info(message...)
}

func (l *Logs) Warn(message ...interface{}) {
	l.logs.Warn(message...)
}

func (l *Logs) Error(message string, err error, args ...interface{}) {
	l.logs.WithFields(logrus.Fields{
		"error":      err,
		"message":    message,
		"stacktrace": string(debug.Stack()),
	}).Error(args...)
}

func PrintLogs(logs *Logs, levels LogsLevel, message ...interface{}) {
	logs.mu.Lock()
	defer logs.mu.Unlock()

	switch levels {
	case 1:
		logs.Info(message...)
	case 2:
		logs.Debug(message...)
	case 3:
		logs.Warn(message...)
	case 4:
		logs.Error(message[0].(string), message[1].(error), message[2:]...)
	default:
		logs.logs.Fatal(message...)
	}
}
