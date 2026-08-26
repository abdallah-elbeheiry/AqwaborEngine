package mapdata

import (
	"encoding/json"
	"errors"
	"os"
)

const (
	KindRing = 0
	KindLine = 1
)

type Color struct {
	R, G, B, A float32
}

type DrawPass struct {
	LayerIndex  int
	FillColor   *Color
	StrokeColor *Color
	RankFilter  bool
}

type Layer struct {
	Name    string
	Kind    int
	GeomIDs []int32
}

type World struct {
	Scale      int32
	Background Color
	DrawOrder  []DrawPass
	Layers     []Layer
	Coords     []int32
	GeomRank   []int32
	GeomN      []int32
	GeomStart  []int32
	GeomLayer  []int32
	MinX       []int32
	MinY       []int32
	MaxX       []int32
	MaxY       []int32
	GeomCount  int32
}

func LoadJSON(path string) (*World, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return LoadJSONBytes(data)
}
AqwaborEngine/
func LoadJSONBytes(data []byte) (*World, error) {
	var wire WireWorld
	if err := json.Unmarshal(data, &wire); err != nil {
		return nil, err
	}
	return buildWorld(&wire)
}

func buildWorld(wire *WireWorld) (*World, error) {
	if wire.Scale == 0 {
		return nil, errors.New("scale is zero")
	}
	if wire.Counts.Geometries == 0 || wire.Counts.Vertices == 0 {
		return nil, errors.New("empty geometry")
	}

	var w World
	w.Scale = wire.Scale
	w.Background = parseColor(wire.Background)

	totalGeoms := 0
	for _, layer := range wire.Layers {
		totalGeoms += len(layer.Geometries)
	}
	if totalGeoms != wire.Counts.Geometries {
		return nil, errors.New("geometry count mismatch")
	}

	w.GeomRank = make([]int32, totalGeoms)
	w.GeomN = make([]int32, totalGeoms)
	w.GeomStart = make([]int32, totalGeoms)
	w.GeomLayer = make([]int32, totalGeoms)
	w.MinX = make([]int32, totalGeoms)
	w.MinY = make([]int32, totalGeoms)
	w.MaxX = make([]int32, totalGeoms)
	w.MaxY = make([]int32, totalGeoms)

	w.Coords = make([]int32, 0, wire.Counts.Vertices*2)

	w.Layers = make([]Layer, len(wire.Layers))
	w.DrawOrder = make([]DrawPass, 0, len(wire.DrawOrder))

	geomIdx := 0
	coordIdx := 0

	for li, wireLayer := range wire.Layers {
		layer := Layer{
			Name:    wireLayer.Name,
			Kind:    wireLayer.Kind,
			GeomIDs: make([]int32, len(wireLayer.Geometries)),
		}
		for gi, wireGeom := range wireLayer.Geometries {
			if len(wireGeom.Coords) != wireGeom.N*2 {
				return nil, errors.New("coords length mismatch")
			}
			layer.GeomIDs[gi] = int32(geomIdx)

			w.GeomRank[geomIdx] = int32(wireGeom.Rank)
			w.GeomN[geomIdx] = int32(wireGeom.N)
			w.GeomStart[geomIdx] = int32(coordIdx)
			w.GeomLayer[geomIdx] = int32(li)

			minX, minY := int32(1<<31-1), int32(1<<31-1)
			maxX, maxY := int32(-1<<31), int32(-1<<31)
			for i := 0; i < wireGeom.N*2; i += 2 {
				x := wireGeom.Coords[i]
				y := wireGeom.Coords[i+1]
				w.Coords = append(w.Coords, x, y)
				if x < minX {
					minX = x
				}
				if x > maxX {
					maxX = x
				}
				if y < minY {
					minY = y
				}
				if y > maxY {
					maxY = y
				}
				coordIdx += 2
			}
			w.MinX[geomIdx] = minX
			w.MinY[geomIdx] = minY
			w.MaxX[geomIdx] = maxX
			w.MaxY[geomIdx] = maxY

			geomIdx++
		}
		w.Layers[li] = layer
	}

	for _, pass := range wire.DrawOrder {
		layerIdx := -1
		for i, layer := range w.Layers {
			if layer.Name == pass.Layer {
				layerIdx = i
				break
			}
		}
		if layerIdx == -1 {
			return nil, errors.New("drawOrder references unknown layer: " + pass.Layer)
		}
		dp := DrawPass{
			LayerIndex: layerIdx,
			RankFilter: pass.RankFilter,
		}
		if pass.Fill != "" && pass.Fill != "null" {
			c := parseColor(pass.Fill)
			dp.FillColor = &c
		}
		if pass.Stroke != "" && pass.Stroke != "null" {
			c := parseColor(pass.Stroke)
			dp.StrokeColor = &c
		}
		w.DrawOrder = append(w.DrawOrder, dp)
	}

	w.GeomCount = int32(geomIdx)

	if len(w.Coords) != wire.Counts.Vertices*2 {
		return nil, errors.New("vertex count mismatch after flattening")
	}

	return &w, nil
}

func (w *World) Unload() {
	w.Coords = nil
	w.GeomRank = nil
	w.GeomN = nil
	w.GeomStart = nil
	w.GeomLayer = nil
	w.MinX = nil
	w.MinY = nil
	w.MaxX = nil
	w.MaxY = nil
	for i := range w.Layers {
		w.Layers[i].GeomIDs = nil
	}
	w.Layers = nil
	w.DrawOrder = nil
	w.GeomCount = 0
}

func parseColor(s string) Color {
	if len(s) == 7 && s[0] == '#' {
		var r, g, b uint32
		_, _ = parseHex(s[1:3], &r)
		_, _ = parseHex(s[3:5], &g)
		_, _ = parseHex(s[5:7], &b)
		return Color{
			R: float32(r) / 255.0,
			G: float32(g) / 255.0,
			B: float32(b) / 255.0,
			A: 1.0,
		}
	}
	return Color{R: 1, G: 1, B: 1, A: 1}
}

func parseHex(s string, out *uint32) (int, error) {
	var val uint32
	for _, c := range s {
		val <<= 4
		switch {
		case c >= '0' && c <= '9':
			val |= uint32(c - '0')
		case c >= 'a' && c <= 'f':
			val |= uint32(c - 'a' + 10)
		case c >= 'A' && c <= 'F':
			val |= uint32(c - 'A' + 10)
		default:
			return 0, errors.New("invalid hex")
		}
	}
	*out = val
	return len(s), nil
}
