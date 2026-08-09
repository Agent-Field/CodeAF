// Package logshim is the port-side stand-in for swe-pro's @/core/util/log.
// The TS logger writes structured lines to a log file/stderr; none of that
// output is model-visible or parity-relevant, so the shim keeps the call
// shape (Create with service tags, leveled methods) and discards the output.
// If run-log parity ever becomes interesting, this is the one place to wire
// a real sink.
package logshim

type Logger struct {
	Tags map[string]any
}

func Create(tags map[string]any) *Logger { return &Logger{Tags: tags} }

var Default = Create(map[string]any{"service": "default"})

func (l *Logger) Debug(msg string, extra ...map[string]any) {}
func (l *Logger) Info(msg string, extra ...map[string]any)  {}
func (l *Logger) Warn(msg string, extra ...map[string]any)  {}
func (l *Logger) Error(msg string, extra ...map[string]any) {}
