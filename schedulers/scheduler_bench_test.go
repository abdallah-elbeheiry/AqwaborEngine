package schedulers

import (
	"math"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// Global sinks to prevent Dead Code Elimination (DCE)
var (
	Sink      float64
	SinkSlice []float64
)

func BenchmarkScheduler_TickThroughput(b *testing.B) {
	for _, every := range []uint{1, 10, 100, 1000} {
		name := "Every_" + strconv.FormatUint(uint64(every), 10)
		b.Run(name, func(b *testing.B) {
			s := NewScheduler()
			s.SetMasterHz(1000)
			var count atomic.Int64

			s.Run(func(st TickState) {
				count.Add(1)
			}, every)

			quantum := time.Millisecond // 1ms matches 1000Hz base exactly

			b.ResetTimer()
			start := time.Now()
			startCount := count.Load()

			for i := 0; i < b.N; i++ {
				s.Advance(quantum)
			}

			elapsed := time.Since(start)
			ticks := count.Load() - startCount
			if elapsed.Seconds() > 0 {
				b.ReportMetric(float64(ticks)/elapsed.Seconds(), "ticks/sec")
			}
		})
	}
}

func BenchmarkScheduler_MathThroughput(b *testing.B) {
	for _, work := range []int{10, 100, 1000, 10000} {
		name := "Work_" + strconv.Itoa(work)
		b.Run(name, func(b *testing.B) {
			s := NewScheduler()
			s.SetMasterHz(1000)
			var count atomic.Int64
			var localSum float64

			s.Run(func(st TickState) {
				var sum float64
				for j := range work {
					sum += math.Sin(float64(st.Tick)*0.001) * math.Cos(float64(j)*0.01)
				}
				localSum += sum
				count.Add(1)
			}, 1)

			quantum := time.Millisecond

			b.ResetTimer()
			start := time.Now()
			startCount := count.Load()

			for i := 0; i < b.N; i++ {
				s.Advance(quantum)
			}

			elapsed := time.Since(start)
			Sink = localSum // Prevent DCE

			ticks := count.Load() - startCount
			if elapsed.Seconds() > 0 {
				b.ReportMetric(float64(ticks)/elapsed.Seconds(), "ticks/sec")
			}
		})
	}
}

func BenchmarkScheduler_MaxSpeedMath(b *testing.B) {
	s := NewScheduler()
	s.SetMasterHz(1000)
	var count atomic.Int64
	var localSum float64

	s.Run(func(st TickState) {
		var sum float64
		for j := range 100000 {
			sum += math.Sin(float64(st.Tick)*0.001) * math.Cos(float64(j)*0.01)
		}
		localSum += sum
		count.Add(1)
	}, 1)

	quantum := time.Millisecond

	b.ResetTimer()
	start := time.Now()
	startCount := count.Load()

	for i := 0; i < b.N; i++ {
		s.Advance(quantum)
	}

	elapsed := time.Since(start)
	Sink = localSum // Prevent DCE

	ticks := count.Load() - startCount
	if elapsed.Seconds() > 0 {
		b.ReportMetric(float64(ticks)/elapsed.Seconds(), "ticks/sec")
	}
}

func BenchmarkScheduler_MultiRateThroughput(b *testing.B) {
	s := NewScheduler()
	s.SetMasterHz(1000)
	var count1, count2, count3, count4 atomic.Int64
	var sums [4]float64

	s.Run(func(st TickState) {
		var sum float64
		for j := range 25 {
			sum += math.Sin(float64(st.Tick)*0.01) * math.Cos(float64(j)*0.1)
		}
		sums[0] += sum
		count1.Add(1)
	}, 1)

	s.Run(func(st TickState) {
		var sum float64
		for j := range 50 {
			sum += math.Sin(float64(st.Tick)*0.01) * math.Cos(float64(j)*0.1)
		}
		sums[1] += sum
		count2.Add(1)
	}, 10)

	s.Run(func(st TickState) {
		var sum float64
		for j := range 100 {
			sum += math.Sqrt(float64(st.Tick) + float64(j))
		}
		sums[2] += sum
		count3.Add(1)
	}, 100)

	s.Run(func(st TickState) {
		var sum float64
		for j := range 200 {
			sum += math.Log(float64(st.Tick)*0.001 + float64(j)*0.01 + 1)
		}
		sums[3] += sum
		count4.Add(1)
	}, 1000)

	quantum := time.Millisecond

	b.ResetTimer()
	start := time.Now()
	c1, c2, c3, c4 := count1.Load(), count2.Load(), count3.Load(), count4.Load()

	for i := 0; i < b.N; i++ {
		s.Advance(quantum)
	}

	elapsed := time.Since(start)
	SinkSlice = sums[:] // Prevent DCE

	if elapsed.Seconds() > 0 {
		b.ReportMetric(float64(count1.Load()-c1)/elapsed.Seconds(), "ticks/sec-every1")
		b.ReportMetric(float64(count2.Load()-c2)/elapsed.Seconds(), "ticks/sec-every10")
		b.ReportMetric(float64(count3.Load()-c3)/elapsed.Seconds(), "ticks/sec-every100")
		b.ReportMetric(float64(count4.Load()-c4)/elapsed.Seconds(), "ticks/sec-every1000")
	}
}

func BenchmarkScheduler_SpeedScaling(b *testing.B) {
	speeds := []float64{0.1, 0.5, 1.0, 2.0, 10.0, 100.0}

	for _, speed := range speeds {
		name := "Speed_" + strconv.FormatFloat(speed, 'f', 1, 64) + "x"
		b.Run(name, func(b *testing.B) {
			s := NewScheduler()
			s.SetMasterHz(1000) // Respect 1000Hz max
			s.SetSpeed(speed)

			var count atomic.Int64
			var localSum float64

			s.Run(func(st TickState) {
				var sum float64
				for j := range 50 {
					sum += math.Sin(float64(st.Tick)*0.01) * math.Cos(float64(j)*0.1)
				}
				localSum += sum
				count.Add(1)
			}, 1)

			quantum := time.Millisecond

			b.ResetTimer()
			start := time.Now()
			startCount := count.Load()

			for i := 0; i < b.N; i++ {
				s.Advance(quantum)
			}

			elapsed := time.Since(start)
			Sink = localSum // Prevent DCE

			ticks := count.Load() - startCount
			if elapsed.Seconds() > 0 {
				b.ReportMetric(float64(ticks)/elapsed.Seconds(), "ticks/sec")
			}
		})
	}
}
