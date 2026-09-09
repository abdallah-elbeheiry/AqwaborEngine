package maprender

import "testing"

// The filter the map data documents: the more ground a pixel covers, the fewer
// minor features are worth drawing.
func TestRankForZoom(t *testing.T) {
	world := RankForZoom(1)      // one pixel per degree, 110 km of ground
	regional := RankForZoom(100) // 1.1 km a pixel
	close := RankForZoom(10000)  // 11 m a pixel

	if !(world < regional && regional < close) {
		t.Fatalf("rank did not rise with zoom: %d, %d, %d", world, regional, close)
	}
	if world < 0 || close > MaxRank {
		t.Fatalf("rank left the 0..%d range: %d and %d", MaxRank, world, close)
	}
	if got := RankForZoom(0); got != MaxRank {
		t.Fatalf("a zero zoom gave rank %d, want everything drawn", got)
	}
	if got := RankForZoom(-1); got != MaxRank {
		t.Fatalf("a negative zoom gave rank %d, want everything drawn", got)
	}
}

// A pass's per-rank counts have to rise with rank and end at the full range,
// or a level of detail would drop geometry it should keep.
func TestPassRangeCountFor(t *testing.T) {
	var p PassRange
	p.Offset = 100
	p.Count = 90
	for i := range p.ByRank {
		p.ByRank[i] = uint32(i * 7)
	}
	p.ByRank[MaxRank] = 90

	for i := 1; i <= MaxRank; i++ {
		if p.ByRank[i] < p.ByRank[i-1] {
			t.Fatalf("rank %d draws less than rank %d", i, i-1)
		}
	}
	if p.CountFor(MaxRank) != 90 {
		t.Fatalf("the highest rank draws %d, want the whole pass at 90", p.CountFor(MaxRank))
	}
	if p.CountFor(-5) != p.ByRank[0] {
		t.Error("a rank below zero did not clamp to the lowest")
	}
	if p.CountFor(999) != p.ByRank[MaxRank] {
		t.Error("a rank above the range did not clamp to the highest")
	}
}

func TestClampRank(t *testing.T) {
	for _, c := range []struct{ in, want int }{
		{-1, 0}, {0, 0}, {5, 5}, {MaxRank, MaxRank}, {MaxRank + 1, MaxRank}, {999, MaxRank},
	} {
		if got := clampRank(c.in); got != c.want {
			t.Errorf("clampRank(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}
