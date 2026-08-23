package schedulers

import (
	"runtime"
	"sync"
)

type Completable interface {
	Wait()
}

type Future[T any] struct {
	getOnce func() T
	doneCh  chan struct{}
}

func Go[T any](fn func() T) *Future[T] {
	f := &Future[T]{
		doneCh: make(chan struct{}),
	}
	f.getOnce = sync.OnceValue(fn)

	go func() {
		f.getOnce()
		close(f.doneCh)
	}()

	return f
}

func (f *Future[T]) Get() T {
	<-f.doneCh
	return f.getOnce()
}

func (f *Future[T]) Wait() {
	<-f.doneCh
}

func AwaitAll(futures ...Completable) {
	for _, f := range futures {
		if f != nil {
			f.Wait()
		}
	}
}

func ParallelFor(count int, fn func(i int)) {
	n := runtime.GOMAXPROCS(0)
	chunk := max(count/n, 1)
	var wg sync.WaitGroup

	for i := range n {
		start := i * chunk
		end := start + chunk
		if i == n-1 {
			end = count
		}
		if start >= end {
			continue
		}

		wg.Add(1)
		go func(start, end int) {
			defer wg.Done()
			for j := start; j < end; j++ {
				fn(j)
			}
		}(start, end)
	}

	wg.Wait()
}
