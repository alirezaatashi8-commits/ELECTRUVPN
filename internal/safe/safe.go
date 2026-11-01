// Package safe contains tiny helpers that keep a panic in one background
// goroutine from taking down the whole panel (and with it the managed
// Xray / sing-box processes). net/http recovers handler panics itself, but
// panics in goroutines started with a bare `go` terminate the process.
package safe

import (
	"context"
	"log/slog"
	"runtime/debug"
	"time"
)

func report(name string, r any) {
	slog.Error("background task panic recovered", "task", name, "panic", r, "stack", string(debug.Stack()))
}

// Run calls fn and converts a panic into a log line.
func Run(name string, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			report(name, r)
		}
	}()
	fn()
}

// Go starts fn in a goroutine with panic recovery.
func Go(name string, fn func()) {
	go Run(name, fn)
}

// Supervise runs a long-lived loop and restarts it (with a growing, capped
// delay) if it panics. It returns when fn returns normally or ctx is done.
func Supervise(ctx context.Context, name string, fn func()) {
	delay := 2 * time.Second
	for {
		panicked := false
		func() {
			defer func() {
				if r := recover(); r != nil {
					panicked = true
					report(name, r)
				}
			}()
			fn()
		}()
		if !panicked || ctx.Err() != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		if delay < time.Minute {
			delay *= 2
		}
	}
}
