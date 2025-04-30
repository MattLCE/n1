package log

import (
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"
)

var (
	// Logger is the global logger instance
	Logger zerolog.Logger
)

// init initializes the global logger
func init() {
	// Set up the global logger with defaults
	Logger = zerolog.New(os.Stderr).With().Timestamp().Logger()

	// Set the global time field format
	zerolog.TimeFieldFormat = time.RFC3339

	// Set the default level to info
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
}

// SetOutput sets the output destination for the global logger
func SetOutput(w io.Writer) {
	Logger = Logger.Output(w)
}

// SetLevel sets the minimum level for the global logger
func SetLevel(level zerolog.Level) {
	zerolog.SetGlobalLevel(level)
}

// EnableConsoleOutput configures the logger to use a more human-friendly console format
func EnableConsoleOutput() {
	consoleWriter := zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}
	Logger = Logger.Output(consoleWriter)
}

// Debug logs a message at debug level
func Debug() *zerolog.Event {
	return Logger.Debug()
}

// Info logs a message at info level
func Info() *zerolog.Event {
	return Logger.Info()
}

// Warn logs a message at warn level
func Warn() *zerolog.Event {
	return Logger.Warn()
}

// Error logs a message at error level
func Error() *zerolog.Event {
	return Logger.Error()
}

// Fatal logs a message at fatal level and then calls os.Exit(1)
func Fatal() *zerolog.Event {
	return Logger.Fatal()
}

// Panic logs a message at panic level and then panics
func Panic() *zerolog.Event {
	return Logger.Panic()
}
