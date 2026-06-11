package log

import "log/slog"

func Info(msg string) {
	slog.Info(msg)
}
