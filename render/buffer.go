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
type instanceBufferOf[T any] struct {
	dev         *wgpu.Device
	stride      int
	label       string
	buffer      *wgpu.Buffer
	cpuData     []T
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

// InstanceBuffer holds the 64-byte sprite instances.
type InstanceBuffer = instanceBufferOf[InstanceData]

// SubcellBuffer holds the 16-byte palette-indexed instances.
type SubcellBuffer = instanceBufferOf[SubcellInstance]

// NewInstanceBuffer creates a sprite instance buffer.
// The buffer carries Storage usage so the compute cull pass can read it.
func NewInstanceBuffer(dev *wgpu.Device, capacity int) *InstanceBuffer {
	return newInstanceBufferOf[InstanceData](dev, capacity, "instance buffer")
}

// NewSubcellBuffer creates a compact instance buffer for the palette path.
func NewSubcellBuffer(dev *wgpu.Device, capacity int) *SubcellBuffer {
	return newInstanceBufferOf[SubcellInstance](dev, capacity, "subcell buffer")
}

func newInstanceBufferOf[T any](dev *wgpu.Device, capacity int, label string) *instanceBufferOf[T] {
	stride := int(unsafe.Sizeof(*new(T)))
	buf, err := dev.CreateBuffer(&wgpu.BufferDescriptor{
		Label: label,
		Size:  uint64(capacity * stride),
		Usage: gputypes.BufferUsageVertex | gputypes.BufferUsageCopyDst | gputypes.BufferUsageStorage,
	})
	if err != nil {
		panic(err)
	}
	return &instanceBufferOf[T]{
		dev:         dev,
		stride:      stride,
		label:       label,
		buffer:      buf,
		cpuData:     make([]T, capacity),
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
func (ib *instanceBufferOf[T]) grow(n int) bool {
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
		Label: ib.label,
		Size:  uint64(capacity * ib.stride),
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

	grown := make([]T, capacity)
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
func (ib *instanceBufferOf[T]) BeginFrame() {
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
func (ib *instanceBufferOf[T]) Release() {
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
func (ib *instanceBufferOf[T]) Write(index int, data *T) {
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
func (ib *instanceBufferOf[T]) WriteAll(src []T) {
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
func (ib *instanceBufferOf[T]) WriteAt(start int, src []T) {
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

// gapMerge is how far apart two dirty ranges may be and still be uploaded as
// one, in instances.
//
// Both sides of the trade were measured on an M4 Max. A WriteBuffer call costs
// about 1.5 us: 2,000 of them for 2,000 moved instances took 3 ms of a frame.
// The bus does about 10 GB/s: one 31 MB upload also took 3 ms. So merging
// across a gap pays whenever
//
//	gap * 64 bytes / 10 GB/s  <  1.5 us
//
// which is a gap of about 234 instances. Half of that is comfortably profitable
// rather than marginal, and it bounds the bytes a merge can waste.
//
// This is a floor, not a cure. Dirty instances that are already next to each
// other cost 0.9 ms where the same number scattered cost 3.4 ms, and no
// threshold closes that: keeping what moves together in the buffer does.
const gapMerge = 128

// maxUploads is how many uploads one flush may issue whatever the ranges look
// like. Past it the closest pairs are joined until it fits, which sends more
// bytes and fewer commands.
const maxUploads = 16

// uploadPlan turns dirty ranges into the ranges actually uploaded: sorted,
// merged where they touch or nearly touch, and joined until there are few
// enough calls. It never returns less than what was dirty, so the plan is
// always safe - the CPU copy is authoritative, and sending a clean instance
// again changes nothing.
//
// It is separate from Flush because this is the decision worth testing, and it
// needs no device to make.
func uploadPlan(ranges [][2]int) [][2]int {
	merged := mergeRanges(ranges)
	if len(merged) < 2 {
		return merged
	}

	// Join neighbours that are close enough that another call costs more than
	// the gap between them.
	out := merged[:1]
	for _, r := range merged[1:] {
		last := &out[len(out)-1]
		if r[0]-last[1] <= gapMerge {
			if r[1] > last[1] {
				last[1] = r[1]
			}
			continue
		}
		out = append(out, r)
	}

	// Still too many calls: join the closest pair until it fits.
	for len(out) > maxUploads {
		best, bestGap := 0, -1
		for i := 0; i+1 < len(out); i++ {
			gap := out[i+1][0] - out[i][1]
			if bestGap < 0 || gap < bestGap {
				best, bestGap = i, gap
			}
		}
		out[best][1] = out[best+1][1]
		out = append(out[:best+1], out[best+2:]...)
	}
	return out
}

// Flush uploads the dirty ranges to the GPU buffer.
// Must be called once per frame before drawing; DrawInstanced and the cull do
// it for themselves, so a caller rarely needs to.
func (ib *instanceBufferOf[T]) Flush(queue *wgpu.Queue) {
	if len(ib.dirtyRanges) == 0 {
		return
	}

	for _, r := range uploadPlan(ib.dirtyRanges) {
		start, end := r[0], r[1]
		if end > len(ib.cpuData) {
			end = len(ib.cpuData)
		}
		if start >= end {
			continue
		}
		byteStart := uint64(start * ib.stride)
		byteSize := uint64((end - start) * ib.stride)
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
func (ib *instanceBufferOf[T]) Reset() {
	ib.count = 0
	ib.dirtyRanges = ib.dirtyRanges[:0]
}

// SetCount manually sets the instance count without writing data.
// Useful when the caller has filled CPUData directly (e.g. via InstanceSlice)
// and then calls WriteAll, or wants to control count independently.
func (ib *instanceBufferOf[T]) SetCount(n int) {
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
func InstanceSlice[T any](ib *instanceBufferOf[T], n int) []T {
	if n > ib.capacity && !ib.grow(n) {
		return nil
	}
	return ib.cpuData[:n:n]
}

// Buffer returns the underlying GPU buffer.
func (ib *instanceBufferOf[T]) Buffer() *wgpu.Buffer {
	return ib.buffer
}

// Count returns the number of valid instances written this frame.
func (ib *instanceBufferOf[T]) Count() int {
	return ib.count
}

// Capacity returns the maximum number of instances.
func (ib *instanceBufferOf[T]) Capacity() int {
	return ib.capacity
}

// CPUData returns the CPU-side instance data slice (for direct access if needed).
func (ib *instanceBufferOf[T]) CPUData() []T {
	return ib.cpuData
}
