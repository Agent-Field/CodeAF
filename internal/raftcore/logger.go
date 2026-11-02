package raftcore

// silentLogger prevents upstream diagnostic formatting from leaking command
// bytes or consulting process time. Invariant failures remain fatal to the
// handle with a constant, payload-free panic; hosts must retain durable stores.
type silentLogger struct{}

func (silentLogger) Debug(...interface{})            {}
func (silentLogger) Debugf(string, ...interface{})   {}
func (silentLogger) Info(...interface{})             {}
func (silentLogger) Infof(string, ...interface{})    {}
func (silentLogger) Warning(...interface{})          {}
func (silentLogger) Warningf(string, ...interface{}) {}
func (silentLogger) Error(...interface{})            {}
func (silentLogger) Errorf(string, ...interface{})   {}
func (silentLogger) Fatal(...interface{})            { panic("raftcore: consensus invariant failed") }
func (silentLogger) Fatalf(string, ...interface{})   { panic("raftcore: consensus invariant failed") }
func (silentLogger) Panic(...interface{})            { panic("raftcore: consensus invariant failed") }
func (silentLogger) Panicf(string, ...interface{})   { panic("raftcore: consensus invariant failed") }
