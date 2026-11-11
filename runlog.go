package main

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// maxRunLogSize caps the output kept per run, so a runaway job can't exhaust memory.
const maxRunLogSize = 4 << 20

// runLog collects a sign job's output while it runs, for /status to stream.
type runLog struct {
	mu        sync.Mutex
	started   time.Time
	finished  time.Time
	err       error
	output    []byte
	truncated bool
}

func newRunLog() *runLog {
	return &runLog{started: time.Now()}
}

func (r *runLog) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if room := maxRunLogSize - len(r.output); len(p) > room {
		r.output = append(r.output, p[:max(room, 0)]...)
		r.truncated = true
	} else {
		r.output = append(r.output, p...)
	}
	return len(p), nil
}

func (r *runLog) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return string(r.output)
}

func (r *runLog) finish(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.finished = time.Now()
	r.err = err
}

// snapshot returns the output from offset on, and whether the run has finished.
func (r *runLog) snapshot(offset int) ([]byte, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.output[offset:len(r.output):len(r.output)], !r.finished.IsZero()
}

// follow writes the run's output to w, and keeps writing new output until the run finishes or
// the client goes away.
func (r *runLog) follow(ctx context.Context, w http.ResponseWriter) error {
	fmt.Fprintf(w, "Last job started %s\n\n", r.started.UTC().Format(time.RFC1123))
	flusher, _ := w.(http.Flusher)
	offset := 0
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		chunk, done := r.snapshot(offset)
		if len(chunk) > 0 {
			if _, err := w.Write(chunk); err != nil {
				return nil
			}
			offset += len(chunk)
		}
		if done {
			fmt.Fprintf(w, "\n%s\n", r.summary())
			return nil
		}
		if flusher != nil {
			flusher.Flush()
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (r *runLog) summary() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := "Succeeded"
	if r.err != nil {
		result = "Failed: " + r.err.Error()
	}
	if r.truncated {
		result += fmt.Sprintf(" (output after the first %d MB not shown)", maxRunLogSize>>20)
	}
	return fmt.Sprintf("%s after %s.", result, r.finished.Sub(r.started).Round(time.Second))
}
