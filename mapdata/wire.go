package mapdata

type WireWorld struct {
	Format     string         `json:"format"`
	Version    int            `json:"version"`
	Source     string         `json:"source"`
	Scale      int32          `json:"scale"`
	Note       string         `json:"note"`
	Counts     WireCounts     `json:"counts"`
	DrawOrder  []WireDrawPass `json:"drawOrder"`
	Background string         `json:"background"`
	Layers     []WireLayer    `json:"layers"`
}

type WireCounts struct {
	Layers     int `json:"layers"`
	Geometries int `json:"geometries"`
	Vertices   int `json:"vertices"`
}

type WireDrawPass struct {
	Layer      string `json:"layer"`
	Fill       string `json:"fill"`
	Stroke     string `json:"stroke"`
	RankFilter bool   `json:"rankFilter"`
}

type WireLayer struct {
	Name       string         `json:"name"`
	Kind       int            `json:"kind"`
	Count      int            `json:"count"`
	Geometries []WireGeometry `json:"geometries"`
}

type WireGeometry struct {
	Rank   int     `json:"rank"`
	N      int     `json:"n"`
	Coords []int32 `json:"coords"`
}
