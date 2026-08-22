package main

import (
	"runtime"
	"testing"
)

const benchSize = 10_000_000

func isPrime(n int) bool {
	if n < 2 {
		return false
	}
	for i := 2; i*i <= n; i++ {
		if n%i == 0 {
			return false
		}
	}
	return true
}

func BenchmarkPrimeSequential(b *testing.B) {
	for i := 0; i < b.N; i++ {
		results := make([]bool, benchSize)
		for j := range benchSize {
			results[j] = isPrime(j)
		}
	}
}

func BenchmarkPrimeParallelFor(b *testing.B) {
	for i := 0; i < b.N; i++ {
		results := make([]bool, benchSize)
		ParallelFor(benchSize, func(j int) {
			results[j] = isPrime(j)
		})
	}
}

func BenchmarkPrimeFutures(b *testing.B) {
	numWorkers := runtime.GOMAXPROCS(0)
	for i := 0; i < b.N; i++ {
		results := make([]bool, benchSize)
		chunk := benchSize / numWorkers
		futures := make([]*Future[bool], numWorkers)

		for w := range numWorkers {
			start := w * chunk
			end := start + chunk
			if w == numWorkers-1 {
				end = benchSize
			}
			s, e := start, end
			futures[w] = Go(func() bool {
				for j := s; j < e; j++ {
					results[j] = isPrime(j)
				}
				return true
			})
		}
		for _, f := range futures {
			f.Get()
		}
	}
}
