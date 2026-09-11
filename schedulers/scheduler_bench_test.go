package schedulers

import (
	"math"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func BenchmarkScheduler_TickThroughput(b *testing.B) {
	for _, every := range []uint{1, 10, 100, 1000} {
		name := "Every_" + strconv.FormatUint(uint64(every), 10)
		b.Run(name, func(b *testing.B) {
			s := NewScheduler()
			s.SetMasterHz(1000) // 1kHz base
			var count atomic.Int64

			s.Run(func(st TickState) {
				count.Add(1)
			}, every)

			quantum := time.Second / 1000

			b.ResetTimer()
			start := time.Now()
			startCount := count.Load()

			for i := 0; i < b.N; i++ {
				s.Advance(quantum)
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
			s.SetMasterHz(100_000)
			var count atomic.Int64

			s.Run(func(st TickState) {
				var sum float64
				for j := range work {
					sum += math.Sin(float64(st.Tick)*0.001) * math.Cos(float64(j)*0.01)
				}
				_ = sum
				count.Add(1)
			}, 1)

			quantum := time.Duration(float64(time.Second) / 100_000.0)

			b.ResetTimer()
			start := time.Now()
			startCount := count.Load()

			for i := 0; i < b.N; i++ {
				s.Advance(quantum)
			}

			elapsed := time.Since(start)
			ticks := count.Load() - startCount
			b.ReportMetric(float64(ticks)/elapsed.Seconds(), "ticks/sec")
		})
	}
}

func BenchmarkScheduler_MaxSpeedMath(b *testing.B) {
	s := NewScheduler()
	s.SetMasterHz(1_000_000)
	var count atomic.Int64

	s.Run(func(st TickState) {
		var sum float64
		for j := range 100 {
			sum += math.Sin(float64(st.Tick)*0.001) * math.Cos(float64(j)*0.01)
		}
		_ = sum
		count.Add(1)
	}, 1)

	quantum := time.Duration(float64(time.Second) / 1_000_000.0)

	b.ResetTimer()
	start := time.Now()
	startCount := count.Load()

	for i := 0; i < b.N; i++ {
		s.Advance(quantum)
	}

	elapsed := time.Since(start)
	ticks := count.Load() - startCount
	b.ReportMetric(float64(ticks)/elapsed.Seconds(), "ticks/sec")
}

func BenchmarkScheduler_MultiRateThroughput(b *testing.B) {
	s := NewScheduler()
	s.SetMasterHz(1_000_000)
	var count1, count2, count3, count4 atomic.Int64

	s.Run(func(st TickState) {
		var sum float64
		for j := range 25 {
			sum += math.Sin(float64(st.Tick)*0.01) * math.Cos(float64(j)*0.1)
		}
		_ = sum
		count1.Add(1)
	}, 1)

	s.Run(func(st TickState) {
		var sum float64
		for j := range 50 {
			sum += math.Sin(float64(st.Tick)*0.01) * math.Cos(float64(j)*0.1)
		}
		_ = sum
		count2.Add(1)
	}, 10)

	s.Run(func(st TickState) {
		var sum float64
		for j := range 100 {
			sum += math.Sqrt(float64(st.Tick) + float64(j))
		}
		_ = sum
		count3.Add(1)
	}, 100)

	s.Run(func(st TickState) {
		var sum float64
		for j := range 200 {
			sum += math.Log(float64(st.Tick)*0.001 + float64(j)*0.01 + 1)
		}
		_ = sum
		count4.Add(1)
	}, 1000)

	quantum := time.Duration(float64(time.Second) / 1_000_000.0)

	b.ResetTimer()
	start := time.Now()
	c1 := count1.Load()
	c2 := count2.Load()
	c3 := count3.Load()
	c4 := count4.Load()

	for i := 0; i < b.N; i++ {
		s.Advance(quantum)
	}

	elapsed := time.Since(start)
	b.ReportMetric(float64(count1.Load()-c1)/elapsed.Seconds(), "ticks/sec-every1")
	b.ReportMetric(float64(count2.Load()-c2)/elapsed.Seconds(), "ticks/sec-every10")
	b.ReportMetric(float64(count3.Load()-c3)/elapsed.Seconds(), "ticks/sec-every100")
	b.ReportMetric(float64(count4.Load()-c4)/elapsed.Seconds(), "ticks/sec-every1000")
}

func BenchmarkScheduler_SpeedScaling(b *testing.B) {
	s := NewScheduler()
	s.SetMasterHz(10_000)
	var count atomic.Int64

	s.Run(func(st TickState) {
		var sum float64
		for j := range 50 {
			sum += math.Sin(float64(st.Tick)*0.01) * math.Cos(float64(j)*0.1)
		}
		_ = sum
		count.Add(1)
	}, 1)

	speeds := []float64{0.1, 0.5, 1.0, 2.0, 10.0, 100.0}
	for _, speed := range speeds {
		name := "Speed_" + strconv.FormatFloat(speed, 'f', 1, 64) + "x"
		b.Run(name, func(b *testing.B) {
			s.SetSpeed(speed)
			quantum := s.Quantum()

			b.ResetTimer()
			start := time.Now()
			startCount := count.Load()

			for i := 0; i < b.N; i++ {
				s.Advance(quantum)
			}

			elapsed := time.Since(start)
			ticks := count.Load() - startCount
			b.ReportMetric(float64(ticks)/elapsed.Seconds(), "ticks/sec")
		})
	}
}
