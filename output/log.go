package output

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

const timestampLayout = "2006-01-02 15:04:05.000"

var writeMu sync.Mutex

func Infof(format string, args ...interface{}) {
	writeWithLevel(os.Stdout, "INFO", "\033[32m", format, args...)
}

func Warnf(format string, args ...interface{}) {
	writeWithLevel(os.Stdout, "WARN", "\033[33m", format, args...)
}

func Errorf(format string, args ...interface{}) {
	writeWithLevel(os.Stderr, "ERROR", "\033[31m", format, args...)
}

func writeWithLevel(writer io.Writer, level, color, format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	message = strings.TrimRight(message, "\n")
	if message == "" {
		message = "-"
	}

	lines := strings.Split(message, "\n")
	writeMu.Lock()
	defer writeMu.Unlock()

	reset := "\033[0m"
	for _, line := range lines {
		fmt.Fprintf(writer, "%s %s%5s%s %s\n", time.Now().Format(timestampLayout), color, level, reset, line)
	}
}
