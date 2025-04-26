package log

import (
	"os"

	"github.com/rs/zerolog"
)

var (
	// L is the shared logger (use log.L.Info().Msg("hi"))
	L zerolog.Logger
)

func init() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	L = zerolog.New(os.Stdout).With().Timestamp().Logger()
}
