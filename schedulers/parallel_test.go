package schedulers

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
	start := time.Now()
	result2 := f.Get()
	if time.Since(start) > time.Microsecond*5 {
		t.Fatalf("second Get should be instantaneous, took %v", time.Since(start))
	}
	if result2 != val {
		t.Fatalf("cached value mismatch: %q != %q", result2, val)
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
	if elapsed < 5*time.Millisecond {
		t.Fatalf("Get should have waited for goroutine, took %v", elapsed)
	}
	if result != 123 {
		t.Fatalf("expected 123, got %d", result)
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

func TestAwaitAll_Multiple(t *testing.T) {
	finish := make(chan struct{}, 3)
	f1 := Go(func() int { <-finish; return 1 })
	f2 := Go(func() int { <-finish; return 2 })
	f3 := Go(func() int { <-finish; return 3 })
	go func() { AwaitAll(f1, f2, f3) }()
	close(finish)
}

func TestParallelFor_ManyCores(t *testing.T) {
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
