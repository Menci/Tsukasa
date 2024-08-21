package main

import (
	"fmt"
	"log"
	"os"
)

type LogLevel int

const (
	Error   LogLevel = 0
	Info    LogLevel = 1
	Verbose LogLevel = 2
)

type Logger struct {
	Printf   func(format string, v ...interface{})
	LogLevel LogLevel
}

func (l *Logger) Fatalf(format string, v ...interface{}) {
	l.Printf(format, v...)
	os.Exit(1)
}

func (l *Logger) Logf(logLevel LogLevel, format string, v ...interface{}) {
	if l.LogLevel >= logLevel {
		l.Printf(format, v...)
	}
}

func (l *Logger) Errorf(format string, v ...interface{}) {
	l.Logf(Error, format, v...)
}

func (l *Logger) Infof(format string, v ...interface{}) {
	l.Logf(Info, format, v...)
}

func (l *Logger) Verbosef(format string, v ...interface{}) {
	l.Logf(Verbose, format, v...)
}

func CreateLogger(prefix string, logLevel LogLevel) *Logger {
	return &Logger{
		Printf:   log.New(os.Stderr, fmt.Sprintf("[%s] ", prefix), log.LstdFlags).Printf,
		LogLevel: logLevel,
	}
}
