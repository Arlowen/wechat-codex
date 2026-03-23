package output

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

const timestampLayout = "2006-01-02 15:04:05"

var writeMu sync.Mutex

func Infof(format string, args ...interface{}) {
	writeWithLevel(os.Stdout, "INFO", format, args...)
}

func Warnf(format string, args ...interface{}) {
	writeWithLevel(os.Stdout, "WARN", format, args...)
}

func Errorf(format string, args ...interface{}) {
	writeWithLevel(os.Stderr, "ERROR", format, args...)
}

func OKf(format string, args ...interface{}) {
	writeWithLevel(os.Stdout, "OK", format, args...)
}

func writeWithLevel(writer io.Writer, level, format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	message = strings.TrimRight(message, "\n")
	if message == "" {
		message = "-"
	}

	lines := strings.Split(message, "\n")
	writeMu.Lock()
	defer writeMu.Unlock()

	for _, line := range lines {
		fmt.Fprintf(writer, "%s %s %s\n", time.Now().Format(timestampLayout), level, line)
	}
}
