// Package render provides a GPU-driven render submission layer.
package render

import (
	"sort"
	"unsafe"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

// InstanceBuffer is a long-lived GPU buffer for instance data with dirty-range tracking.
// It maintains a CPU-side slice of InstanceData and uploads only dirty ranges via WriteBuffer.
type InstanceBuffer struct {
	buffer      *wgpu.Buffer
	cpuData     []InstanceData
	capacity    int
	dirtyRanges [][2]int // [start, end) in instances
	count       int      // number of valid instances written this frame
}

// NewInstanceBuffer creates a new instance buffer with the given capacity.
// The buffer carries Storage usage so the compute cull pass can read it.
func NewInstanceBuffer(dev *wgpu.Device, capacity int) *InstanceBuffer {
	size := uint64(capacity * instanceDataSize)
	buf, err := dev.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "instance buffer",
		Size:  size,
		Usage: gputypes.BufferUsageVertex | gputypes.BufferUsageCopyDst | gputypes.BufferUsageStorage,
	})
	if err != nil {
		panic(err)
	}
	return &InstanceBuffer{
		buffer:      buf,
		cpuData:     make([]InstanceData, capacity),
		capacity:    capacity,
		dirtyRanges: make([][2]int, 0, 8),
	}
}

// Write copies instance data at the given index and marks the range dirty.
// Call once per instance per frame (only for changed instances).
// Note: Write always marks dirty even if the value is unchanged. A frame
// that rewrites every instance degenerates to one full upload; callers with
// dense updates should just write all instances and let Flush coalesce.
func (ib *InstanceBuffer) Write(index int, data *InstanceData) {
	if index < 0 || index >= ib.capacity {
		panic("instance index out of bounds")
	}
	if ib.count <= index {
		ib.count = index + 1
	}
	ib.cpuData[index] = *data
	ib.markDirty(index, index+1)
}

// markDirty adds a dirty range, merging with adjacent if possible.
func (ib *InstanceBuffer) markDirty(start, end int) {
	ib.dirtyRanges = append(ib.dirtyRanges, [2]int{start, end})
}

// Flush uploads all dirty ranges to the GPU buffer.
// Must be called once per frame before drawing (DrawInstanced does this
// automatically). When dirty writes are dense, ranges are coalesced into a
// single span upload instead of many small WriteBuffers.
func (ib *InstanceBuffer) Flush(queue *wgpu.Queue) {
	if len(ib.dirtyRanges) == 0 {
		return
	}

	merged := mergeRanges(ib.dirtyRanges)

	// Coalesce to one span upload when dense: if the span covers mostly
	// dirty instances, one WriteBuffer beats many small ones.
	if len(merged) > 1 {
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
		if span > 0 && (len(merged) > 8 || dirty*2 >= span) {
			merged = [][2]int{{minStart, maxEnd}}
		}
	}

	for _, r := range merged {
		start, end := r[0], r[1]
		byteStart := uint64(start * instanceDataSize)
		byteSize := uint64((end - start) * instanceDataSize)

		// Upload the dirty range from CPU data
		src := unsafe.Slice((*byte)(unsafe.Pointer(&ib.cpuData[start])), int(byteSize))
		queue.WriteBuffer(ib.buffer, byteStart, src)
	}

	ib.dirtyRanges = ib.dirtyRanges[:0]
}

// mergeRanges merges overlapping and adjacent ranges.
// It sorts in place with sort.Slice (O(n log n)) and merges into the same
// backing array to avoid allocating on every flush.
func mergeRanges(ranges [][2]int) [][2]int {
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

// Reset clears the instance count and dirty ranges for the next frame.
func (ib *InstanceBuffer) Reset() {
	ib.count = 0
	ib.dirtyRanges = ib.dirtyRanges[:0]
}

// Buffer returns the underlying GPU buffer.
func (ib *InstanceBuffer) Buffer() *wgpu.Buffer {
	return ib.buffer
}

// Count returns the number of valid instances written this frame.
func (ib *InstanceBuffer) Count() int {
	return ib.count
}

// Capacity returns the maximum number of instances.
func (ib *InstanceBuffer) Capacity() int {
	return ib.capacity
}

// CPUData returns the CPU-side instance data slice (for direct access if needed).
func (ib *InstanceBuffer) CPUData() []InstanceData {
	return ib.cpuData
}
