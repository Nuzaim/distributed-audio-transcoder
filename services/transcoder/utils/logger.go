package utils

import (
  "os"
  "log/slog"
)

func NewLogger() *slog.Logger {
  level := slog.LevelInfo
  opts := &slog.HandlerOptions{
    Level: level,
  }
  var handler slog.Handler
  handler = slog.NewTextHandler(os.Stdout, opts)

  return slog.New(handler)
}
