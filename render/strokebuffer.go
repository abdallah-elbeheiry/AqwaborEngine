package render

import (
	"sort"
	"unsafe"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

// StrokeBuffer is a long-lived GPU buffer for stroke segment instances with
// dirty-range tracking.  It follows the same two-mode upload model as
// InstanceBuffer: sparse Write for individual segment changes, or dense
// WriteAll for full polyline rebuilds.
type StrokeBuffer struct {
	buffer      *wgpu.Buffer
	cpuData     []StrokeSegment
	capacity    int
	dirtyRanges [][2]int // [start, end) in segments
	count       int      // valid segments written this frame
}

// NewStrokeBuffer creates a stroke buffer with the given capacity.
func NewStrokeBuffer(dev *wgpu.Device, capacity int) *StrokeBuffer {
	size := uint64(capacity * strokeSegmentSize)
	buf, err := dev.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "stroke buffer",
		Size:  size,
		Usage: gputypes.BufferUsageVertex | gputypes.BufferUsageCopyDst | gputypes.BufferUsageStorage,
	})
	if err != nil {
		panic(err)
	}
	return &StrokeBuffer{
		buffer:   buf,
		cpuData:  make([]StrokeSegment, capacity),
		capacity: capacity,
	}
}

// Write marks a single segment slot for upload.  Use for sparse updates
// where only a few segments change per frame.
func (sb *StrokeBuffer) Write(index int, seg *StrokeSegment) {
	if index < 0 || index >= sb.capacity {
		panic("stroke segment index out of bounds")
	}
	if sb.count <= index+1 {
		sb.count = index + 1
	}
	sb.cpuData[index] = *seg
	sb.dirtyRanges = append(sb.dirtyRanges, [2]int{index, index + 1})
}

// WriteAll copies src into the buffer starting at slot 0, sets the count,
// and marks [0, len(src)) dirty in one range.  Use for full polyline rebuilds.
func (sb *StrokeBuffer) WriteAll(src []StrokeSegment) {
	n := len(src)
	if n > sb.capacity {
		panic("WriteAll: src exceeds stroke buffer capacity")
	}
	copy(sb.cpuData[:n], src)
	sb.count = n
	if n > 0 {
		sb.dirtyRanges = append(sb.dirtyRanges, [2]int{0, n})
	}
}

// WriteAt copies src into the buffer starting at start, sets the count,
// and marks one dirty range.
func (sb *StrokeBuffer) WriteAt(start int, src []StrokeSegment) {
	n := len(src)
	if n == 0 {
		return
	}
	if start < 0 || start+n > sb.capacity {
		panic("WriteAt: range exceeds stroke buffer capacity")
	}
	copy(sb.cpuData[start:start+n], src)
	end := start + n
	if sb.count < end {
		sb.count = end
	}
	sb.dirtyRanges = append(sb.dirtyRanges, [2]int{start, end})
}

// Flush uploads all dirty ranges to the GPU buffer.
// Must be called once per frame before drawing (DrawStrokes does this
// automatically when called through Renderer).
func (sb *StrokeBuffer) Flush(queue *wgpu.Queue) {
	if len(sb.dirtyRanges) == 0 {
		return
	}

	merged := mergeStrokeRanges(sb.dirtyRanges)

	// When many small ranges span most of the buffer, collapse to one upload.
	if len(merged) > 8 {
		minStart, maxEnd, dirty := merged[0][0], merged[0][1], 0
		for _, r := range merged {
			if r[0] < minStart {
				minStart = r[0]
			}
			if r[1] > maxEnd {
				maxEnd = r[1]
			}
			dirty += r[1] - r[0]
		}
		span := maxEnd - minStart
		if span > 0 && dirty*2 >= span {
			merged = [][2]int{{minStart, maxEnd}}
		}
	}

	for _, r := range merged {
		start, end := r[0], r[1]
		byteStart := uint64(start * strokeSegmentSize)
		byteSize := uint64((end - start) * strokeSegmentSize)
		src := unsafe.Slice((*byte)(unsafe.Pointer(&sb.cpuData[start])), int(byteSize))
		queue.WriteBuffer(sb.buffer, byteStart, src)
	}

	sb.dirtyRanges = sb.dirtyRanges[:0]
}

// Reset clears the segment count and dirty ranges for the next frame.
func (sb *StrokeBuffer) Reset() {
	sb.count = 0
	sb.dirtyRanges = sb.dirtyRanges[:0]
}

// SetCount manually sets the segment count without writing data.
func (sb *StrokeBuffer) SetCount(n int) {
	if n < 0 || n > sb.capacity {
		panic("SetCount: out of range")
	}
	sb.count = n
}

// Buffer returns the underlying GPU buffer.
func (sb *StrokeBuffer) Buffer() *wgpu.Buffer { return sb.buffer }

// Count returns the number of valid segments written this frame.
func (sb *StrokeBuffer) Count() int { return sb.count }

// Capacity returns the maximum number of segments.
func (sb *StrokeBuffer) Capacity() int { return sb.capacity }

// CPUData returns the CPU-side segment data slice (for direct access).
func (sb *StrokeBuffer) CPUData() []StrokeSegment { return sb.cpuData }

// mergeStrokeRanges merges overlapping and adjacent stroke ranges in-place.
func mergeStrokeRanges(ranges [][2]int) [][2]int {
	if len(ranges) == 0 {
		return nil
	}
	sort.Slice(ranges, func(i, j int) bool { return ranges[i][0] < ranges[j][0] })
	out := ranges[:1]
	for _, r := range ranges[1:] {
		last := &out[len(out)-1]
		if r[0] <= last[1] {
			if r[1] > last[1] {
				last[1] = r[1]
			}
		} else {
			out = append(out, r)
		}
	}
	return out
}
