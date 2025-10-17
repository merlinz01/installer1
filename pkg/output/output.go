package output

import (
	"fmt"
	"os"
)

var outputFile *os.File = os.Stderr
var debugLevel DebugLevel = LevelNone
var colorizeOutput bool = true

type DebugLevel int

const (
	LevelNone DebugLevel = iota
	LevelError
	LevelWarn
	LevelInfo
	LevelDebug
)

func SetOutputFile(f *os.File) {
	if f == nil {
		f = os.Stderr
	}
	outputFile = f
	colorizeOutput = false
}

func SetDebugLevel(level DebugLevel) {
	debugLevel = level
}

func Output(level DebugLevel, msg string, args ...any) {
	if level > debugLevel {
		return
	}
	prefix := ""
	switch level {
	case LevelError:
		prefix = "ERROR: "
	case LevelWarn:
		prefix = "WARN: "
	case LevelInfo:
		prefix = "INFO: "
	case LevelDebug:
		prefix = "DEBUG: "
	}
	if colorizeOutput {
		switch level {
		case LevelError:
			prefix = "\033[31m" + prefix + "\033[0m" // Red
		case LevelWarn:
			prefix = "\033[33m" + prefix + "\033[0m" // Yellow
		case LevelInfo:
			prefix = "\033[32m" + prefix + "\033[0m" // Green
		case LevelDebug:
			prefix = "\033[34m" + prefix + "\033[0m" // Blue
		}
	}
	_, err := fmt.Fprintf(outputFile, prefix+msg+"\n", args...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to write to log file, using stderr: %v\n", err)
		outputFile = os.Stderr
		colorizeOutput = false
	}
}

func Debug(msg string, args ...any) {
	Output(LevelDebug, msg, args...)
}

func Info(msg string, args ...any) {
	Output(LevelInfo, msg, args...)
}

func Warn(msg string, args ...any) {
	Output(LevelWarn, msg, args...)
}

func Error(msg string, args ...any) {
	Output(LevelError, msg, args...)
}
