// Package render provides a GPU-driven render submission layer.
package render

import (
	"sort"
	"unsafe"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

// InstanceBuffer is a long-lived GPU buffer for instance data with two upload
// modes chosen by the caller:
//
//   - Sparse: Write(i, ...) per changed index. Dirty ranges track only touched
//     slots; Flush uploads a merged set of small spans.
//
//   - Dense: WriteAll(slice) or WriteAt(start, slice). One range, one upload,
//     no dirty-range machinery overhead.
//
// Choose the mode that matches your update pattern.  Mixing both on the same
// buffer within a single frame is safe (dirty ranges accumulate and Flush
// handles them all), but a frame that rewrites every instance should not go
// through N Write calls.
type InstanceBuffer struct {
	dev         *wgpu.Device
	buffer      *wgpu.Buffer
	cpuData     []InstanceData
	capacity    int
	dirtyRanges [][2]int // [start, end) in instances
	count       int      // number of valid instances written this frame

	// retired holds buffers replaced by a larger one. A frame already submitted
	// may still be reading the old buffer, so it is released after enough
	// frames have passed rather than at the moment it is replaced.
	retired []retiredBuffer
	frame   uint64
}

type retiredBuffer struct {
	buf   *wgpu.Buffer
	frame uint64
}

// bufferRetireAfter is how many frames a replaced buffer is held before release.
const bufferRetireAfter = 3

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
		dev:         dev,
		buffer:      buf,
		cpuData:     make([]InstanceData, capacity),
		capacity:    capacity,
		dirtyRanges: make([][2]int, 0, 8),
	}
}

// grow replaces the GPU buffer with one at least n instances long, doubling so
// the amortised cost stays one copy an element. The CPU-side array carries over;
// the GPU side is marked entirely dirty, because the new buffer holds nothing.
//
// Growing rather than panicking is the point: a game that spawns one more
// sprite than the number guessed at startup used to crash.
func (ib *InstanceBuffer) grow(n int) bool {
	if n <= ib.capacity {
		return true
	}
	if ib.dev == nil {
		log.Error("instance buffer cannot grow without a device", "want", n, "capacity", ib.capacity)
		return false
	}

	capacity := max(ib.capacity, 1)
	for capacity < n {
		capacity *= 2
	}

	buf, err := ib.dev.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "instance buffer",
		Size:  uint64(capacity * instanceDataSize),
		Usage: gputypes.BufferUsageVertex | gputypes.BufferUsageCopyDst | gputypes.BufferUsageStorage,
	})
	if err != nil {
		log.Error("failed to grow the instance buffer", "capacity", capacity, "err", err)
		return false
	}

	if ib.buffer != nil {
		ib.retired = append(ib.retired, retiredBuffer{buf: ib.buffer, frame: ib.frame})
	}
	ib.buffer = buf

	grown := make([]InstanceData, capacity)
	copy(grown, ib.cpuData)
	ib.cpuData = grown
	ib.capacity = capacity

	// Everything written so far has to be uploaded again: the new buffer is
	// empty and the old one's contents did not travel with it.
	ib.dirtyRanges = ib.dirtyRanges[:0]
	if ib.count > 0 {
		ib.dirtyRanges = append(ib.dirtyRanges, [2]int{0, ib.count})
	}

	log.Debug("instance buffer grew", "capacity", capacity)
	return true
}

// BeginFrame advances the frame counter and releases buffers retired long
// enough ago that no submitted frame can still be reading them.
func (ib *InstanceBuffer) BeginFrame() {
	ib.frame++
	kept := ib.retired[:0]
	for _, r := range ib.retired {
		if ib.frame-r.frame >= bufferRetireAfter {
			r.buf.Release()
			continue
		}
		kept = append(kept, r)
	}
	ib.retired = kept
}

// Release frees the GPU buffer and anything still retired. A batch that is not
// released leaks its buffer for the life of the process.
func (ib *InstanceBuffer) Release() {
	for _, r := range ib.retired {
		r.buf.Release()
	}
	ib.retired = nil
	if ib.buffer != nil {
		ib.buffer.Release()
		ib.buffer = nil
	}
}

// Write marks a single instance slot for upload. Use this for sparse updates
// where only a few instances change per frame. Does not compare old vs new.
// For full-buffer rewrites use WriteAll instead.
func (ib *InstanceBuffer) Write(index int, data *InstanceData) {
	if index < 0 {
		log.Error("instance index is negative", "index", index)
		return
	}
	if index >= ib.capacity && !ib.grow(index+1) {
		return
	}
	if ib.count <= index {
		ib.count = index + 1
	}
	ib.cpuData[index] = *data
	ib.dirtyRanges = append(ib.dirtyRanges, [2]int{index, index + 1})
}

// WriteAll copies src into the buffer starting at slot 0, sets the count,
// and marks [0, len(src)) dirty in one range. Use this for dense frames
// where every live instance is rewritten.
func (ib *InstanceBuffer) WriteAll(src []InstanceData) {
	n := len(src)
	if n > ib.capacity && !ib.grow(n) {
		return
	}
	copy(ib.cpuData[:n], src)
	ib.count = n
	if n > 0 {
		ib.dirtyRanges = append(ib.dirtyRanges, [2]int{0, n})
	}
}

// WriteAt copies src into the buffer starting at start, sets the count
// to max(count, start+len(src)), and marks one dirty range.
// Use this for a batch write that covers a contiguous region.
func (ib *InstanceBuffer) WriteAt(start int, src []InstanceData) {
	n := len(src)
	if n == 0 {
		return
	}
	if start < 0 {
		log.Error("instance write starts before zero", "start", start)
		return
	}
	if start+n > ib.capacity && !ib.grow(start+n) {
		return
	}
	copy(ib.cpuData[start:start+n], src)
	end := start + n
	if ib.count < end {
		ib.count = end
	}
	ib.dirtyRanges = append(ib.dirtyRanges, [2]int{start, end})
}

// Flush uploads all dirty ranges to the GPU buffer.
// Must be called once per frame before drawing (DrawInstanced does this
// automatically). Callers using WriteAll/WriteAt already emit clean ranges;
// this merge is a safety net for mixed sparse+dense patterns in one frame.
func (ib *InstanceBuffer) Flush(queue *wgpu.Queue) {
	if len(ib.dirtyRanges) == 0 {
		return
	}

	merged := mergeRanges(ib.dirtyRanges)

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

// SetCount manually sets the instance count without writing data.
// Useful when the caller has filled CPUData directly (e.g. via InstanceSlice)
// and then calls WriteAll, or wants to control count independently.
func (ib *InstanceBuffer) SetCount(n int) {
	if n < 0 {
		log.Error("instance count is negative", "n", n)
		return
	}
	if n > ib.capacity && !ib.grow(n) {
		return
	}
	ib.count = n
}

// InstanceSlice returns a mutable slice of the CPU-side backing array.
// Write into it, then call WriteAll or WriteAt to mark the range dirty.
// This avoids per-slot Write calls when packing active instances into a
// contiguous block (see particle.Emitter.WriteInstances).
func InstanceSlice(ib *InstanceBuffer, n int) []InstanceData {
	if n > ib.capacity && !ib.grow(n) {
		return nil
	}
	return ib.cpuData[:n:n]
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
