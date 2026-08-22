package main

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestParallelLoopRunsAllTasks(t *testing.T) {
	const n = 2000
	var count atomic.Int64

	l := NewLoop(1000)
	l.Start()
	defer l.Stop()

	for range n {
		l.Once(func() { count.Add(1) })
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if count.Load() == n {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected %d runs, got %d", n, count.Load())
}
