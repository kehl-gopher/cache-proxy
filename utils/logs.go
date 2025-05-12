package utils

import (
	js "encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"github.com/sirupsen/logrus"
	"gopkg.in/natefinch/lumberjack.v2"
)

var logsOutputFile = "logs/cache-proxy.log"

type handler string

var (
	text handler = "text"
	json handler = "json"
)

type Logs struct {
	level int
	Msg   interface{}
	mu    *sync.RWMutex
	logs  *logrus.Logger
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
	logs.SetOutput(&lumberjack.Logger{
		Filename:   logsOutputFile,
		MaxSize:    5, // megabytes
		MaxBackups: 3,
		MaxAge:     28,
		Compress:   true,
	})

	return &Logs{
		logs: logs,
		mu:   &sync.RWMutex{},
	}
}

func PrintLogs(level slog.Level, mu *sync.Mutex, l *Logs) {
}
