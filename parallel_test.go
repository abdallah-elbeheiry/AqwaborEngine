package main

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestFuture_GetBasic(t *testing.T) {
	val := "hello"
	valCopy := val
	f := Go(func() string {
		return valCopy
	})
	result := f.Get()
	if result != val {
		t.Fatalf("expected %q, got %q", val, result)
	}
	// second Get returns cached value instantly
	start := time.Now()
	result2 := f.Get()
	if time.Since(start) > time.Microsecond*5 { // should be very fast
		t.Fatalf("second Get should be instantaneous, took %v", time.Since(start))
	}
	if result2 != val {
		t.Fatalf("cached value mismatch: %q != %q", result2, val)
	}
}

func TestFuture_GetMultipleTypes(t *testing.T) {
	type myStruct struct {
		x int
		y string
	}
	s := myStruct{42, "test"}
	f := Go(func() myStruct { return s })
	result := f.Get()
	if result.x != s.x || result.y != s.y {
		t.Fatalf("struct mismatch: %v != %v", result, s)
	}
}

func TestFuture_WaitsForCompletion(t *testing.T) {
	done := make(chan struct{})
	f := Go(func() int {
		<-done
		return 123
	})
	go func() { time.Sleep(10 * time.Millisecond); close(done) }()
	start := time.Now()
	result := f.Get()
	elapsed := time.Since(start)
	if elapsed < 5*time.Millisecond { // should be delayed
		t.Fatalf("Get should have waited for goroutine, took %v", elapsed)
	}
	if result != 123 {
		t.Fatalf("expected 123, got %d", result)
	}
}

func TestFuture_WaitMethod(t *testing.T) {
	started := make(chan struct{})
	completed := make(chan struct{})
	f := Go(func() int {
		close(started)
		<-completed
		return 42
	})
	// Give goroutine time to start
	<-started
	waitDone := make(chan struct{})
	go func() {
		f.Wait()
		close(waitDone)
	}()
	if len(waitDone) != 0 { // should still be waiting
		t.Fatalf("Wait should block until completion")
	}
	close(completed)
	<-waitDone
}

func TestAwaitAll_Single(t *testing.T) {
	f := Go(func() int { return 1 })
	AwaitAll(f)
}

func TestAwaitAll_Multiple(t *testing.T) {
	finish := make(chan struct{}, 3)
	f1 := Go(func() int { <-finish; return 1 })
	f2 := Go(func() int { <-finish; return 2 })
	f3 := Go(func() int { <-finish; return 3 })
	// start waiters
	go func() { AwaitAll(f1, f2, f3) }()
	close(finish)
}

func TestAwaitAll_Heterogeneous(t *testing.T) {
	fInt := Go(func() int { time.Sleep(5 * time.Millisecond); return 42 })
	fStr := Go(func() string { time.Sleep(10 * time.Millisecond); return "hello" })
	fSlice := Go(func() []int { return []int{1, 2, 3} })
	AwaitAll(fInt, fStr, fSlice)
	if v := fInt.Get(); v != 42 {
		t.Fatalf("int mismatch: %d", v)
	}
	if v := fStr.Get(); v != "hello" {
		t.Fatalf("string mismatch: %q", v)
	}
	if v := fSlice.Get(); len(v) != 3 {
		t.Fatalf("slice mismatch: %v", v)
	}
}

func TestClosureWithArguments(t *testing.T) {
	add := func(a, b int) int { return a + b }
	arg1, arg2 := 5, 10
	f := Go(func() int { return add(arg1, arg2) })

	if result := f.Get(); result != 15 {
		t.Fatalf("closure argument passing failed: expected 15, got %d", result)
	}
}

func TestParallelFor_Basic(t *testing.T) {
	var counter atomic.Int64
	ParallelFor(100, func(i int) {
		counter.Add(1)
	})
	if counter.Load() != 100 {
		t.Fatalf("ParallelFor failed: expected 100, got %d", counter.Load())
	}
}

func TestParallelFor_Deterministic(t *testing.T) {
	results := make([]int, 100)
	fn := func(i int) { results[i] = i }
	ParallelFor(100, fn)
	for i, v := range results {
		if v != i {
			t.Fatalf("ParallelFor order wrong: position %d has %d", i, v)
		}
	}
}

func TestParallelFor_ConsecutiveChunks(t *testing.T) {
	n := runtime.GOMAXPROCS(0)
	if n == 0 {
		t.Skip("no CPUs available")
	}
	counts := make([]atomic.Int64, n)
	total := n * 10
	ParallelFor(total, func(i int) {
		for idx := range n {
			start := idx * (total / n)
			end := start + (total / n)
			if idx == n-1 {
				end = total
			}
			if i >= start && i < end {
				counts[idx].Add(1)
				return
			}
		}
	})
	for i := range n {
		if counts[i].Load() != 10 {
			t.Fatalf("ParallelFor chunk %d expected 10 items, got %d", i, counts[i].Load())
		}
	}
}

func BenchmarkFutureSerial(b *testing.B) {
	for i := 0; i < b.N; i++ {
		v := Go(func() int { return i }).Get()
		_ = v
	}
}

func BenchmarkFutureParallel(b *testing.B) {
	var wg sync.WaitGroup
	for i := 0; i < b.N; i++ {
		wg.Add(1)
		go func(i int) {
			v := Go(func() int { return i }).Get()
			_ = v
			wg.Done()
		}(i)
	}
	wg.Wait()
}

func BenchmarkParallelForSequential(b *testing.B) {
	for i := 0; i < b.N; i++ {
		ParallelFor(1000, func(j int) {})
	}
}

func BenchmarkParallelForManyCores(b *testing.B) {
	for i := 0; i < b.N; i++ {
		ParallelFor(10000, func(j int) {})
	}
}
