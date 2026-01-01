package logger

import (
	"os"

	"github.com/sirupsen/logrus"
)

var Log *logrus.Logger

func Init(level, format string) {
	Log = logrus.New()
	Log.SetOutput(os.Stdout)

	// Set log level
	logLevel, err := logrus.ParseLevel(level)
	if err != nil {
		logLevel = logrus.DebugLevel
	}
	Log.SetLevel(logLevel)

	// Set formatter
	if format == "json" {
		Log.SetFormatter(&logrus.JSONFormatter{
			TimestampFormat: "2006-01-02 15:04:05",
		})
	} else {
		Log.SetFormatter(&logrus.TextFormatter{
			FullTimestamp:   true,
			TimestampFormat: "2006-01-02 15:04:05",
		})
	}
}

func GetLogger() *logrus.Logger {
	if Log == nil {
		Init("debug", "text")
	}
	return Log
}

// SetLevel changes the log level at runtime.
func SetLevel(level string) error {
	l, err := logrus.ParseLevel(level)
	if err != nil {
		return err
	}
	GetLogger().SetLevel(l)
	return nil
}
