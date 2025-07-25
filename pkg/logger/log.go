package logger

import (
	"fmt"
	"runtime"

	"github.com/sirupsen/logrus"
	prefixed "github.com/x-cray/logrus-prefixed-formatter"

	"github.com/autobrr/tqm/pkg/formatting"
)

var (
	prefixLen       = 15
	loggingFilePath string
)

/* Public */

func Init(logLevel int, logFilePath string) error {
	var useLevel logrus.Level

	// determine logging level
	switch logLevel {
	case 0:
		useLevel = logrus.InfoLevel
	case 1:
		useLevel = logrus.DebugLevel
	default:
		useLevel = logrus.TraceLevel
	}

	// set rotating file hook
	fileLogFormatter := &prefixed.TextFormatter{}
	fileLogFormatter.FullTimestamp = true
	fileLogFormatter.QuoteEmptyFields = true
	fileLogFormatter.DisableColors = true
	fileLogFormatter.ForceFormatting = true

	rotateFileHook, err := NewRotateFileHook(RotateFileConfig{
		Filename:   logFilePath,
		MaxSize:    5,
		MaxBackups: 10,
		MaxAge:     90,
		Level:      useLevel,
		Formatter:  fileLogFormatter,
	})

	if err != nil {
		logrus.WithError(err).Errorf("Failed initializing rotating file log to %q", logFilePath)
		return fmt.Errorf("rotating file hook: %w", err)
	}
	logrus.AddHook(rotateFileHook)

	// set console formatter
	logFormatter := &prefixed.TextFormatter{}
	logFormatter.FullTimestamp = true
	logFormatter.QuoteEmptyFields = true
	logFormatter.ForceFormatting = true

	if runtime.GOOS == "windows" {
		// disable colors on windows
		logFormatter.DisableColors = true
	}

	logrus.SetFormatter(logFormatter)

	// set logging level
	logrus.SetLevel(useLevel)

	// set globals
	loggingFilePath = logFilePath

	return nil
}

func ShowUsing() {
	log := GetLogger("log")

	log.Infof("Using %s = %s", formatting.LeftJust("LOG_LEVEL", " ", 10),
		logrus.GetLevel().String())
	log.Infof("Using %s = %q", formatting.LeftJust("LOG", " ", 10), loggingFilePath)
}

func GetLogger(prefix string) *logrus.Entry {
	if len(prefix) > prefixLen {
		prefixLen = len(prefix)
	}

	return logrus.WithFields(logrus.Fields{"prefix": formatting.LeftJust(prefix, " ", prefixLen)})
}
