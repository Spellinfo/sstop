package collector

// SparklineLen is the default number of samples kept for sparkline display.
const SparklineLen = 16

// RingBuffer is a fixed-size circular buffer of float64 values.
type RingBuffer struct {
	data  []float64
	size  int
	head  int // next write position
	count int // number of valid samples
}

// NewRingBuffer creates a RingBuffer with the default SparklineLen size.
func NewRingBuffer() *RingBuffer {
	return NewRingBufferN(SparklineLen)
}

// NewRingBufferN creates a RingBuffer with a custom size.
func NewRingBufferN(size int) *RingBuffer {
	if size <= 0 {
		size = SparklineLen
	}
	return &RingBuffer{
		data: make([]float64, size),
		size: size,
	}
}

// Push adds a new value to the buffer.
func (r *RingBuffer) Push(v float64) {
	// Lazy init for zero-value RingBuffers (backwards compat)
	if r.size == 0 {
		r.size = SparklineLen
		r.data = make([]float64, r.size)
	}
	r.data[r.head] = v
	r.head = (r.head + 1) % r.size
	if r.count < r.size {
		r.count++
	}
}

// Samples returns all valid samples in chronological order (oldest first).
func (r *RingBuffer) Samples() []float64 {
	if r.count == 0 {
		return nil
	}
	result := make([]float64, r.count)
	start := (r.head - r.count + r.size) % r.size
	for i := 0; i < r.count; i++ {
		result[i] = r.data[(start+i)%r.size]
	}
	return result
}
