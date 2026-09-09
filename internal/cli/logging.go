package cli

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/spf13/pflag"
	logger "github.com/yousysadmin/go-logger"
)

// logOptions are the logging flags. They are persistent on the root
// command, so every subcommand logs the same way.
type logOptions struct {
	level  string
	format string
	file   string
}

// addLogFlags registers the logging flags on fs.
func addLogFlags(fs *pflag.FlagSet, o *logOptions) {
	fs.StringVar(&o.level, "log-level", logger.LevelInfo, "trace, debug, info, warn or error")
	fs.StringVar(&o.format, "log-format", logger.FormatText, "text or json")
	fs.StringVar(&o.file, "log-file", "", "also append JSON log lines to this file")
}

// consoleTimeFormat is the clock on a terminal line. The date is noise
// for a tool that runs in a shell for the afternoon, and the file sink
// keeps the full timestamp anyway.
const consoleTimeFormat = "15:04:05.000"

// setupLogging builds the process logger from the flags and installs it
// as the slog default. The console sink is stderr, colored when stderr is
// a terminal. The optional file sink is JSON at the same level.
func setupLogging(o logOptions) (*slog.Logger, error) {
	level := strings.ToLower(strings.TrimSpace(o.level))
	if _, err := logger.ParseLevel(level); err != nil {
		return nil, fmt.Errorf("--log-level: %q is not one of trace, debug, info, warn, error", o.level)
	}

	format := strings.ToLower(strings.TrimSpace(o.format))
	if format != logger.FormatText && format != logger.FormatJSON {
		return nil, fmt.Errorf("--log-format: %q is not text or json", o.format)
	}

	sinks := []logger.Sink{{
		Level:      level,
		Output:     logger.OutputStderr,
		Format:     format,
		Color:      true,
		TimeFormat: consoleTimeFormat,
	}}

	if o.file != "" {
		sinks = append(sinks, logger.JSONSink(level, expandHome(o.file)))
	}

	log, err := logger.Setup(sinks...)
	if err != nil {
		return nil, fmt.Errorf("logging: %w", err)
	}

	return log, nil
}
