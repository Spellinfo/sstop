package collector

// EMA implements Exponential Moving Average smoothing for bandwidth rates.
type EMA struct {
	alpha  float64
	value  float64
	primed bool
}

// NewEMA creates a new EMA with the given smoothing factor (0 < alpha <= 1).
// Higher alpha = more responsive, lower alpha = smoother.
func NewEMA(alpha float64) *EMA {
	return &EMA{alpha: alpha}
}

// Update feeds a new sample and returns the smoothed value.
func (e *EMA) Update(sample float64) float64 {
	if !e.primed {
		e.value = sample
		e.primed = true
	} else {
		e.value = e.alpha*sample + (1-e.alpha)*e.value
	}
	return e.value
}
