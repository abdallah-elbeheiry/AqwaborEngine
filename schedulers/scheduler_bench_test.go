package schedulers

import (
	"math"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func BenchmarkScheduler_TickThroughput(b *testing.B) {
	for _, hz := range []float64{1000, 10000, 100000, 1000000} {
		name := "Hz_" + strconv.FormatFloat(hz, 'f', 0, 64)
		b.Run(name, func(b *testing.B) {
			s := NewScheduler()
			var count atomic.Int64

			s.Run(func(st TickState) {
				count.Add(1)
			}, hz)

			s.Start()
			defer s.Stop()

			time.Sleep(50 * time.Millisecond)

			b.ResetTimer()
			start := time.Now()
			startCount := count.Load()

			for i := 0; i < b.N; i++ {
				time.Sleep(time.Millisecond)
			}

			elapsed := time.Since(start)
			ticks := count.Load() - startCount
			b.ReportMetric(float64(ticks)/elapsed.Seconds(), "ticks/sec")
		})
	}
}

func BenchmarkScheduler_MathThroughput(b *testing.B) {
	for _, work := range []int{10, 100, 1000} {
		name := "Work_" + strconv.Itoa(work)
		b.Run(name, func(b *testing.B) {
			s := NewScheduler()
			var count atomic.Int64

			s.Run(func(st TickState) {
				var sum float64
				for j := range work {
					sum += math.Sin(float64(st.Tick)*0.001) * math.Cos(float64(j)*0.01)
				}
				_ = sum
				count.Add(1)
			}, 100_000.0)

			s.Start()
			defer s.Stop()

			time.Sleep(50 * time.Millisecond)

			b.ResetTimer()
			start := time.Now()
			startCount := count.Load()

			for i := 0; i < b.N; i++ {
				time.Sleep(time.Millisecond)
			}

			elapsed := time.Since(start)
			ticks := count.Load() - startCount
			b.ReportMetric(float64(ticks)/elapsed.Seconds(), "ticks/sec")
		})
	}
}

func BenchmarkScheduler_MaxSpeedMath(b *testing.B) {
	s := NewScheduler()
	var count atomic.Int64

	s.Run(func(st TickState) {
		var sum float64
		for j := range 100 {
			sum += math.Sin(float64(st.Tick)*0.001) * math.Cos(float64(j)*0.01)
		}
		_ = sum
		count.Add(1)
	}, 1_000_000.0)

	s.Start()
	defer s.Stop()

	time.Sleep(50 * time.Millisecond)

	b.ResetTimer()
	start := time.Now()
	startCount := count.Load()

	for i := 0; i < b.N; i++ {
		time.Sleep(time.Millisecond)
	}

	elapsed := time.Since(start)
	ticks := count.Load() - startCount
	b.ReportMetric(float64(ticks)/elapsed.Seconds(), "ticks/sec")
}

func BenchmarkScheduler_MultiRateThroughput(b *testing.B) {
	s := NewScheduler()
	var count1m, count100k, count10k, count1k atomic.Int64

	s.Run(func(st TickState) {
		var sum float64
		for j := range 25 {
			sum += math.Sin(float64(st.Tick)*0.01) * math.Cos(float64(j)*0.1)
		}
		_ = sum
		count1m.Add(1)
	}, 1_000_000.0)

	s.Run(func(st TickState) {
		var sum float64
		for j := range 50 {
			sum += math.Sin(float64(st.Tick)*0.01) * math.Cos(float64(j)*0.1)
		}
		_ = sum
		count100k.Add(1)
	}, 100_000.0)

	s.Run(func(st TickState) {
		var sum float64
		for j := range 100 {
			sum += math.Sqrt(float64(st.Tick) + float64(j))
		}
		_ = sum
		count10k.Add(1)
	}, 10_000.0)

	s.Run(func(st TickState) {
		var sum float64
		for j := range 200 {
			sum += math.Log(float64(st.Tick)*0.001 + float64(j)*0.01 + 1)
		}
		_ = sum
		count1k.Add(1)
	}, 1_000.0)

	s.Start()
	defer s.Stop()

	time.Sleep(50 * time.Millisecond)

	b.ResetTimer()
	start := time.Now()
	c1m := count1m.Load()
	c100k := count100k.Load()
	c10k := count10k.Load()
	c1k := count1k.Load()

	for i := 0; i < b.N; i++ {
		time.Sleep(time.Millisecond)
	}

	elapsed := time.Since(start)
	b.ReportMetric(float64(count1m.Load()-c1m)/elapsed.Seconds(), "ticks/sec-target-1m")
	b.ReportMetric(float64(count100k.Load()-c100k)/elapsed.Seconds(), "ticks/sec-target-100k")
	b.ReportMetric(float64(count10k.Load()-c10k)/elapsed.Seconds(), "ticks/sec-target-10k")
	b.ReportMetric(float64(count1k.Load()-c1k)/elapsed.Seconds(), "ticks/sec-target-1k")
}

func BenchmarkScheduler_SpeedScaling(b *testing.B) {
	s := NewScheduler()
	var count atomic.Int64

	s.Run(func(st TickState) {
		var sum float64
		for j := range 50 {
			sum += math.Sin(float64(st.Tick)*0.01) * math.Cos(float64(j)*0.1)
		}
		_ = sum
		count.Add(1)
	}, 10_000.0)

	s.Start()
	defer s.Stop()

	speeds := []float64{0.1, 0.5, 1.0, 2.0, 10.0, 100.0}
	for _, speed := range speeds {
		name := "Speed_" + strconv.FormatFloat(speed, 'f', 1, 64) + "x"
		b.Run(name, func(b *testing.B) {
			s.SetSpeed(speed)
			time.Sleep(30 * time.Millisecond)

			b.ResetTimer()
			start := time.Now()
			startCount := count.Load()

			for i := 0; i < b.N; i++ {
				time.Sleep(time.Millisecond)
			}

			elapsed := time.Since(start)
			ticks := count.Load() - startCount
			b.ReportMetric(float64(ticks)/elapsed.Seconds(), "ticks/sec")
		})
	}
}
