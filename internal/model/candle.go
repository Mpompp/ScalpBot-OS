package model

// Candle represents an OHLCV bar aggregated from tick data.
// TickCount tracks the number of ticks that formed this candle for
// volume-weighted calculations and data quality checks.
type Candle struct {
	Symbol      string  // Currency pair
	Open        float64 // Opening price
	High        float64 // Highest price during period
	Low         float64 // Lowest price during period
	Close       float64 // Closing price
	Volume      float64 // Tick volume (number of price changes)
	BuyerVol    float64 // Up-tick buyer volume
	SellerVol   float64 // Down-tick seller volume
	TickCount   int32   // Number of ticks in this candle
	TimestampNs int64   // Period start time as Unix nanoseconds
}

// Range returns the price range of the candle (High - Low).
func (c Candle) Range() float64 {
	return c.High - c.Low
}

// IsBullish returns true if the candle closed higher than or equal to open.
func (c Candle) IsBullish() bool {
	return c.Close >= c.Open
}

// Body returns the absolute body size of the candle |Close - Open|.
func (c Candle) Body() float64 {
	body := c.Close - c.Open
	if body < 0 {
		return -body
	}
	return body
}
