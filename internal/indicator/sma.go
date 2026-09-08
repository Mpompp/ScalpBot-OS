package indicator

// SMA implements a Simple Moving Average over a fixed sliding window.
// Uses a circular buffer to guarantee 0 heap allocations on Update().
type SMA struct {
	period int
	values []float64
	idx    int
	count  int
	sum    float64
	ready  bool
}

// NewSMA creates a new Simple Moving Average indicator.
func NewSMA(period int) *SMA {
	if period <= 0 {
		period = 50
	}
	return &SMA{
		period: period,
		values: make([]float64, period),
	}
}

// Update appends a new price/metric and returns the updated average.
// Zero-allocation via circular buffer index overwriting.
func (s *SMA) Update(value float64) float64 {
	if s.count < s.period {
		s.values[s.idx] = value
		s.sum += value
		s.idx = (s.idx + 1) % s.period
		s.count++
		if s.count >= s.period {
			s.ready = true
		}
		return s.sum / float64(s.count)
	}

	// Overwrite oldest value in-place without slice reallocation
	s.sum -= s.values[s.idx]
	s.values[s.idx] = value
	s.sum += value
	s.idx = (s.idx + 1) % s.period
	return s.sum / float64(s.period)
}

// Value returns the current SMA value.
func (s *SMA) Value() float64 {
	if s.count == 0 {
		return 0
	}
	return s.sum / float64(s.count)
}

// Ready returns true when enough samples have filled the window.
func (s *SMA) Ready() bool {
	return s.ready
}
