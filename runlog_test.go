package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRunLogTruncates(t *testing.T) {
	run := newRunLog()
	half := bytes.Repeat([]byte("x"), maxRunLogSize/2+10)
	for range 3 {
		if n, err := run.Write(half); err != nil || n != len(half) {
			t.Fatalf("write = %d, %v", n, err)
		}
	}
	if got := len(run.String()); got != maxRunLogSize {
		t.Fatalf("kept %d bytes, want %d", got, maxRunLogSize)
	}
	run.finish(nil)
	if !strings.Contains(run.summary(), "not shown") {
		t.Fatalf("summary %q doesn't mention truncation", run.summary())
	}
}

// readUntil reads lines until one contains want, failing after a timeout.
func readUntil(t *testing.T, r *bufio.Reader, want string) string {
	t.Helper()
	var got strings.Builder
	done := make(chan error, 1)
	go func() {
		for {
			line, err := r.ReadString('\n')
			got.WriteString(line)
			if strings.Contains(line, want) || err != nil {
				done <- err
				return
			}
		}
	}()
	select {
	case err := <-done:
		if err != nil && err != io.EOF {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %q", want)
	}
	return got.String()
}

func TestRunLogFollowStreams(t *testing.T) {
	run := newRunLog()
	_, _ = run.Write([]byte("first line\n"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = run.follow(r.Context(), w)
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body := bufio.NewReader(resp.Body)

	// output arrives while the run is still going
	readUntil(t, body, "first line")
	_, _ = run.Write([]byte("second line\n"))
	readUntil(t, body, "second line")

	run.finish(errors.New("exit status 1"))
	rest := readUntil(t, body, "Failed: exit status 1")
	if strings.Contains(rest, "first line") {
		t.Fatalf("output repeated: %q", rest)
	}
}

func TestRunLogFollowStopsWhenClientLeaves(t *testing.T) {
	run := newRunLog()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = run.follow(ctx, httptest.NewRecorder())
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("follow kept running after the client left")
	}
}
