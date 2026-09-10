package triangulate

import (
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/abdallah-elbeheiry/AqwaborEngine/examples/mapdata"
)

func TestTriangulateSquare(t *testing.T) {
	ring := []int32{0, 0, 10, 0, 10, 10, 0, 10}
	tris := Ring(ring, 1)
	if len(tris) != 6 {
		t.Fatalf("want 2 triangles, got %d", len(tris)/3)
	}
	if got := triArea(ring, 1, tris); math.Abs(got-100) > 1e-9 {
		t.Fatalf("want area 100, got %v", got)
	}
}

func TestTriangulateConcave(t *testing.T) {
	ring := []int32{0, 0, 20, 0, 20, 10, 10, 10, 10, 20, 0, 20}
	tris := Ring(ring, 1)
	if got := triArea(ring, 1, tris); math.Abs(got-300) > 1e-9 {
		t.Fatalf("want area 300, got %v", got)
	}
}

func TestTriangulateWorld(t *testing.T) {
	path := filepath.Join("..", "examples", "world_v3.json")
	world, err := mapdata.LoadJSON(path)
	if err != nil {
		t.Skipf("world data not available: %v", err)
	}

	scale := float64(world.Scale)
	var rings, verts, tris, bad int
	var worst, totalWant, totalErr float64
	start := time.Now()
	for li := range world.Layers {
		if world.Layers[li].Kind != mapdata.KindRing {
			continue
		}
		for _, gid := range world.Layers[li].GeomIDs {
			s := world.GeomStart[gid]
			n := world.GeomN[gid]
			coords := world.Coords[s : s+n*2]
			out := Ring(coords, scale)
			rings++
			verts += int(n)
			tris += len(out) / 3

			want := math.Abs(SignedArea2(coords, scale)) / 2
			got := triArea(coords, scale, out)
			totalWant += want
			totalErr += math.Abs(got - want)
			if want > 1e-8 {
				rel := math.Abs(got-want) / want
				if rel > worst {
					worst = rel
				}
				if rel > 1e-6 {
					bad++
				}
			}
		}
	}
	elapsed := time.Since(start)
	t.Logf("%d rings, %d verts -> %d triangles in %v (%.0f verts/s)",
		rings, verts, tris, elapsed.Round(time.Millisecond),
		float64(verts)/elapsed.Seconds())
	t.Logf("area error %.3e of %.3e square degrees total (%.2e relative)",
		totalErr, totalWant, totalErr/totalWant)
	t.Logf("rings above 1e-8 square degrees off by more than 1e-6: %d of %d (worst %.3e)",
		bad, rings, worst)
	if rel := totalErr / totalWant; rel > 1e-8 {
		t.Errorf("total area error %.3e relative, want below 1e-8", rel)
	}
	if worst > 0.01 {
		t.Errorf("worst ring off by %.3e, want below 1%%", worst)
	}
}

func triArea(coords []int32, scale float64, tris []int32) float64 {
	total := 0.0
	for i := 0; i+2 < len(tris); i += 3 {
		a, b, c := tris[i], tris[i+1], tris[i+2]
		ax, ay := float64(coords[a*2])/scale, float64(coords[a*2+1])/scale
		bx, by := float64(coords[b*2])/scale, float64(coords[b*2+1])/scale
		cx, cy := float64(coords[c*2])/scale, float64(coords[c*2+1])/scale
		total += math.Abs((bx-ax)*(cy-ay)-(by-ay)*(cx-ax)) / 2
	}
	return total
}
