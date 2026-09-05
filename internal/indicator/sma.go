package indicator

// SMA implements a Simple Moving Average over a fixed sliding window.
type SMA struct {
	period int
	values []float64
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
		values: make([]float64, 0, period),
	}
}

// Update appends a new price/metric and returns the updated average.
func (s *SMA) Update(value float64) float64 {
	if len(s.values) < s.period {
		s.values = append(s.values, value)
		s.sum += value
		if len(s.values) >= s.period {
			s.ready = true
		}
		return s.sum / float64(len(s.values))
	}

	s.sum -= s.values[0]
	s.values = append(s.values[1:], value)
	s.sum += value
	return s.sum / float64(s.period)
}

// Value returns the current SMA value.
func (s *SMA) Value() float64 {
	if len(s.values) == 0 {
		return 0
	}
	return s.sum / float64(len(s.values))
}

// Ready returns true when enough samples have filled the window.
func (s *SMA) Ready() bool {
	return s.ready
}
