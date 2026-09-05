// Package hmm implements an institutional-grade, zero-heap-allocation
// 3-State Gaussian Hidden Markov Model (HMM) for financial regime detection.
package hmm

import (
	"math"
)

// MarketState represents the latent hidden market regime.
type MarketState int

const (
	// StateNoise: Random walk, low directional momentum, sideways chop -> 100% IDLE.
	StateNoise MarketState = 0
	// StateBull: Bullish momentum expansion, strong buyer volume imbalance -> BUY allowed.
	StateBull MarketState = 1
	// StateBear: Bearish momentum expansion, strong seller volume imbalance -> SELL allowed.
	StateBear MarketState = 2
)

// String returns the human-readable representation of MarketState.
func (s MarketState) String() string {
	switch s {
	case StateBull:
		return "BULL_EXPANSION"
	case StateBear:
		return "BEAR_EXPANSION"
	default:
		return "NOISE_IDLE"
	}
}

const (
	NumStates   = 3
	NumFeatures = 3
)

// ObservationVector holds the 3 stack-allocated quantitative features for HMM inference:
// [0] LogReturn: ln(Close_t / Close_{t-1})
// [1] RelATR: ATR_t / Close_t
// [2] VolumeImbalance: (BuyerVol - SellerVol) / (TotalVol + 1e-9), clamped to [-1, +1]
type ObservationVector [NumFeatures]float64

// GaussianHMM maintains the hidden Markov state probabilities across sequential observations.
// Guaranteed 0 B/op and 0 heap allocations per Update call.
type GaussianHMM struct {
	// alpha holds the normalized forward state posterior probabilities [P(S_0), P(S_1), P(S_2)]
	alpha [NumStates]float64

	// Transition probability matrix A[from][to]
	trans [NumStates][NumStates]float64

	// Gaussian emission parameters
	means   [NumStates][NumFeatures]float64
	inv2Var [NumStates][NumFeatures]float64 // 1 / (2 * variance)
	logNorm [NumStates]float64              // log(normalization constant)

	initialized bool
}

// Calibration v6 — Robust Gaussian HMM for Intraday Swing & Multi-Timeframe Regimes:
//   - Prior: Neutral [0.34, 0.33, 0.33] to avoid startup bias.
//   - Transition: Balanced matrix (Noise self-loop 0.60, Bull/Bear persistence 0.70).
//   - LogReturn: Noise centered at 0.0, Bull at +0.0008, Bear at -0.0008.
//   - RelATR: Noise baseline 0.0005, Bull/Bear expansion 0.0010.
//   - Variances: Asymmetric design (Noise σ_ret=0.0004, Bull/Bear σ_ret=0.0010, σ_vol=0.30).
func NewGaussianHMM() *GaussianHMM {
	hmm := &GaussianHMM{
		// Initial prior: neutral / equiprobable to avoid startup Noise lock
		alpha: [NumStates]float64{0.34, 0.33, 0.33},

		// Transition matrix: Balanced persistence and escape rates
		trans: [NumStates][NumStates]float64{
			// From Noise -> [Noise, Bull, Bear]
			{0.60, 0.20, 0.20},
			// From Bull -> [Noise, Bull, Bear]
			{0.25, 0.70, 0.05},
			// From Bear -> [Noise, Bull, Bear]
			{0.25, 0.05, 0.70},
		},

		// Emission Means: [LogReturn, RelATR, VolumeImbalance]
		means: [NumStates][NumFeatures]float64{
			// State 0 (Noise): Near-zero return, baseline ATR, balanced volume
			{0.0000, 0.0005, 0.00},
			// State 1 (Bull): Bullish expansion return, elevated ATR, buyer dominance
			{+0.0008, 0.0010, +0.25},
			// State 2 (Bear): Bearish expansion return, elevated ATR, seller dominance
			{-0.0008, 0.0010, -0.25},
		},
	}

	// Variances: ASYMMETRIC design for proper 3-state discrimination.
	//
	// Key insight:
	//   - Noise: narrow LogReturn (flat price is signature), wide Volume (ignores direction)
	//   - Bull/Bear: moderate LogReturn, focused Volume (directional confirmation)
	variances := [NumStates][NumFeatures]float64{
		// Noise: narrow LogReturn, wide Volume (ignores direction)
		{0.00045 * 0.00045, 0.0008 * 0.0008, 0.75 * 0.75}, // σ_ret=0.00045, σ_relATR=0.0008, σ_vol=0.75
		// Bull: moderate LogReturn, moderate ATR, focused Volume
		{0.0009 * 0.0009, 0.0008 * 0.0008, 0.35 * 0.35}, // σ_ret=0.0009, σ_relATR=0.0008, σ_vol=0.35
		// Bear: moderate LogReturn, moderate ATR, focused Volume
		{0.0009 * 0.0009, 0.0008 * 0.0008, 0.35 * 0.35}, // σ_ret=0.0009, σ_relATR=0.0008, σ_vol=0.35
	}

	// Precalculate inverse 2*variance and log normalization constants
	logSqrt2Pi := 0.5 * math.Log(2.0*math.Pi)
	for s := 0; s < NumStates; s++ {
		logN := 0.0
		for f := 0; f < NumFeatures; f++ {
			v := variances[s][f]
			hmm.inv2Var[s][f] = 1.0 / (2.0 * v)
			logN += -logSqrt2Pi - 0.5*math.Log(v)
		}
		hmm.logNorm[s] = logN
	}

	hmm.initialized = true
	return hmm
}

// Update executes one step of the Forward Algorithm on the stack.
// Returns the most likely MarketState and its posterior confidence probability in [0.0, 1.0].
// Guaranteed 0 heap allocations. Fully unrolled for maximum throughput.
func (h *GaussianHMM) Update(obs ObservationVector) (MarketState, float64) {
	// 1. Compute log emissions inline (fully unrolled, no loops)
	d00 := obs[0] - h.means[0][0]
	d01 := obs[1] - h.means[0][1]
	d02 := obs[2] - h.means[0][2]
	logE0 := h.logNorm[0] - (d00*d00*h.inv2Var[0][0] + d01*d01*h.inv2Var[0][1] + d02*d02*h.inv2Var[0][2])

	d10 := obs[0] - h.means[1][0]
	d11 := obs[1] - h.means[1][1]
	d12 := obs[2] - h.means[1][2]
	logE1 := h.logNorm[1] - (d10*d10*h.inv2Var[1][0] + d11*d11*h.inv2Var[1][1] + d12*d12*h.inv2Var[1][2])

	d20 := obs[0] - h.means[2][0]
	d21 := obs[1] - h.means[2][1]
	d22 := obs[2] - h.means[2][2]
	logE2 := h.logNorm[2] - (d20*d20*h.inv2Var[2][0] + d21*d21*h.inv2Var[2][1] + d22*d22*h.inv2Var[2][2])

	// Numerical safety clamp
	if logE0 < -100.0 {
		logE0 = -100.0
	}
	if logE1 < -100.0 {
		logE1 = -100.0
	}
	if logE2 < -100.0 {
		logE2 = -100.0
	}

	// 2. Find max log-emission and subtract (log-sum-exp trick)
	maxLogE := logE0
	if logE1 > maxLogE {
		maxLogE = logE1
	}
	if logE2 > maxLogE {
		maxLogE = logE2
	}

	// 3. Convert to linear — only 2 Exp calls needed (max emission = 1.0)
	e0 := math.Exp(logE0 - maxLogE)
	e1 := math.Exp(logE1 - maxLogE)
	e2 := math.Exp(logE2 - maxLogE)

	// Floor
	if e0 < 1e-12 {
		e0 = 1e-12
	}
	if e1 < 1e-12 {
		e1 = 1e-12
	}
	if e2 < 1e-12 {
		e2 = 1e-12
	}

	// 4. Forward step (fully unrolled)
	a0, a1, a2 := h.alpha[0], h.alpha[1], h.alpha[2]

	n0 := e0 * (a0*h.trans[0][0] + a1*h.trans[1][0] + a2*h.trans[2][0])
	n1 := e1 * (a0*h.trans[0][1] + a1*h.trans[1][1] + a2*h.trans[2][1])
	n2 := e2 * (a0*h.trans[0][2] + a1*h.trans[1][2] + a2*h.trans[2][2])

	sum := n0 + n1 + n2

	// 5. Normalize
	if sum > 0 && !math.IsNaN(sum) && !math.IsInf(sum, 0) {
		inv := 1.0 / sum
		h.alpha[0] = n0 * inv
		h.alpha[1] = n1 * inv
		h.alpha[2] = n2 * inv
	} else {
		h.alpha[0] = 0.34
		h.alpha[1] = 0.33
		h.alpha[2] = 0.33
	}

	// 6. Argmax
	bestState := StateNoise
	maxConf := h.alpha[0]
	if h.alpha[1] > maxConf {
		bestState = StateBull
		maxConf = h.alpha[1]
	}
	if h.alpha[2] > maxConf {
		bestState = StateBear
		maxConf = h.alpha[2]
	}

	return bestState, maxConf
}

// Posterior returns the current posterior distribution across all 3 states.
func (h *GaussianHMM) Posterior() (pNoise, pBull, pBear float64) {
	return h.alpha[0], h.alpha[1], h.alpha[2]
}

// Reset re-initializes the HMM state distribution to neutral prior.
func (h *GaussianHMM) Reset() {
	h.alpha = [NumStates]float64{0.34, 0.33, 0.33}
}
