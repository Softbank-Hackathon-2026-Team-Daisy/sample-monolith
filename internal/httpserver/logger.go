package httpserver

import (
	"io"
	"log/slog"
	"time"
)

// NewLogger returns a JSON logger writing to w. The time field is named
// "timestamp" and formatted as RFC 3339 in UTC.
func NewLogger(w io.Writer, level slog.Leveler) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) == 0 && a.Key == slog.TimeKey {
				a.Key = "timestamp"
				a.Value = slog.StringValue(a.Value.Time().UTC().Format(time.RFC3339Nano))
			}
			return a
		},
	}))
}
