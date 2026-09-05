# ⚡ ScalpBot OS — High-Speed Institutional Quantitative Scalper

ScalpBot OS adalah mesin trading kuantitatif institusional (*Institutional Quantitative Multi-Symbol Scalping Engine*) berlatensi ultra rendah (*sub-microsecond internal execution*) yang dibangun secara murni menggunakan **Go (Golang) 1.22+** dan terhubung langsung ke **MetaTrader 5 (MT5)** melalui jembatan *native dual TCP socket IPC* berkecepatan tinggi tanpa dependensi CGO, ZeroMQ, atau library pihak ketiga.

Seluruh jalur pemrosesan data pasar (*hot-path / onTick*) dirancang dengan prinsip **Zero-Heap Allocation** (`0 B/op, 0 allocs/op`) untuk menjamin eksekusi waktu nyata yang deterministik tanpa jeda *Garbage Collection (GC Pause)*.



---

## 🏛️ Arsitektur Sistem Terintegrasi

## 🔥 Volatility-Filtered Donchian Breakout Strategy (Utama)

Strategi utama sekarang adalah **Donchian Breakout** dengan filter volatilitas ATR, tren EMA, dan range filter. Ini adalah strategi yang paling konservatif dan sesuai dengan target 5-15% per bulan dengan profit factor ≥1.5.

Parameter default (setelah calibrate via backtest + forward test):
- ATR Period: 18
- EMA Filter Period: 42 (M15)
- Donchian Bars: 24
- Range Multiplier: 1.35
- Risk Per Trade: 0.4%
- Max Open Positions: 1
- Circuit Breaker: 12% drawdown / 2.5% daily loss
- Max Daily Trades: 7
- Max Spread: 35 pips

Ini strategi momentum breakout yang mekanis, mudah diuji, dan tidak ada martingale/grid.

---


```
                                  [MetaTrader 5 Tick Stream (PUB 5556)]
                                                    │
                                           tickCh (Multi-Symbol)
                                                    │
                   ┌────────────────────────────────┴────────────────────────────────┐
                   ▼                                                                 ▼
     [Tick RingBuffer (Per Symbol)]                                     [Active Position Tracker]
           (Fixed-size 4096)                                                         │
                   │                                                                 ├─ Trailing Stop (Adaptive ATR / Pips)
         [OHLCVAggregator (M1)]                                                      ├─ Auto Break-Even (Buffer Protection)
                   │                                                                 ├─ Multi-Stage Partial TP (50% Scaling)
         [Dual-Strategy Engine]                                                      ├─ Stagnant Trade Auto-Killer (35m)
         ├─ Trend Momentum Scalper (EMA5/13+RSI)                                     ├─ Dynamic Time-Stop Duration
         └─ Mean-Reversion Range Scalper (Bollinger)                                 └─ Real-Time Floating P&L & Pip Tracker
                   │                                                                         │
              signalCh (64)                                                                  ▼
                   │                                                            [Broker.Close / Modify Stop]
      [AI & ML Intelligence Gate]                                                            │
      ├─ Macro Trend Guard (M1/M5/M15 Alignment)                                             ▼
      ├─ Gaussian HMM 3-State Regime Detector (36 ns/op, 0 B/op)               [Disk Persistence Store]
      │   ├─ StateNoise  → 100% IDLE (blocks all orders)                        ├─ data/trade_history.json
      │   ├─ StateBull   → BUY only  (conf >= 70%)                              └─ data/daily_state.json
      │   └─ StateBear   → SELL only (conf >= 70%)
      ├─ 12 Zero-Alloc Feature Extractor (138 ns/op)
      ├─ Anti-Pucuk & Anti-Lembah Exhaustion Guards
      └─ GBDT Decision Tree Ensemble Scorer (15 ns/op)
                   │
   [Cross-Symbol Correlation Guard]
   (Blocks Over-Concentrated Currency Risk)
                   │
    [Live Economic News Blackout]
    (Auto-Halt Orders on High-Impact Events)
                   │
      [Risk & Guardrail Manager]
      ├─ Strict Max 1 Position Per Symbol
      ├─ Volatility-Parity Lot Sizing (FX & Gold)
      ├─ Cent Account Auto-Scaling (USC / USD)
      ├─ Tick Velocity / TPS Spike Guard
      ├─ Spread Anomaly Baseline Filter
      ├─ Daily Drawdown Circuit Breaker (5%)
      └─ Auto Daily Profit Target Lock (5% / 50 USC)
                   │
              orderCh (32)
                   │
    [Executor Dispatcher Worker Pool]
                   │
         ┌─────────┴─────────┐
         ▼                   ▼
   [Mock Broker]    [MT5 Broker Adapter]
   (Simulation)              │ (REQ-REP 5555 TCP)
                             ▼
                    [MT5_Bridge.mq5 v2.20 (EA)]

─────────────────────────────────────────────────────────────────────────────
[Real-Time Control Center & Analytics] (http://127.0.0.1:8080)
  ├─ TradingView Lightweight Candlestick Charts (Multi-Timeframe 15S / 1M / 5M)
  ├─ Interactive Hover Crosshair OHLC Legend & Real-Time Clock
  ├─ 60 FPS Auto-Scaling Canvas Equity & Realized PnL Curve
  ├─ Dynamic Mode Switcher (🌿 SANTAI | ⚖️ BALANCED | 🔥 AGRESIF)
  ├─ Dynamic Market Focus (🌐 ALL | 🪙 GOLD ONLY | 💵 FOREX ONLY)
  ├─ Performance & Trade Analytics Tab (Win Rate, Profit Factor, Realized R:R)
  ├─ Completed Trades Audit Log Table with Instant CSV Export
  ├─ 2-Way Interactive Telegram Remote Control Bot (Inline Buttons)
  └─ Atomic State Controls (🚨 Emergency Kill-Switch & Dynamic AI Toggle)
```

---

## 💎 Fitur Unggulan & Pilar Kuantitatif

### 1. 🧠 Arsitektur AI & Machine Learning (`internal/ai/`, `internal/ai/hmm/`)

#### Gaussian Hidden Markov Model (HMM) — Inti Deteksi Regime
Engine utama pengambilan keputusan: 3-State Gaussian HMM yang berjalan **36 ns/op** dengan **0 B/op heap allocation** sepenuhnya di CPU stack.

- **3 Observation Features per candle M1**:
  - `LogReturn` = `ln(Close_t / Close_{t-1})` — arah pergerakan harga
  - `RelATR` = `ATR / Close` — volatilitas relatif
  - `VolumeImbalance` = `(BuyVol - SellVol) / TotalVol` — dominansi beli/jual, clamp [-1,+1]

- **3 Regime States**:
  | State | Kondisi Pasar | Aksi Trading |
  |-------|--------------|--------------|
  | `NOISE_IDLE` | Sideways / random walk | 🚫 100% IDLE — Semua order diblokir |
  | `BULL_EXPANSION` | Momentum bullish + buyer dominance | ✅ BUY saja (conf ≥ 70%) |
  | `BEAR_EXPANSION` | Momentum bearish + seller dominance | ✅ SELL saja (conf ≥ 70%) |

- **MinConfidence per Mode Trading**:
  | Mode | Min Conf (Trend) | Min Conf (Range) |
  |------|-----------------|-----------------|
  | 🌿 SANTAI | 80% | 70% |
  | ⚖️ BALANCED | 70% | 60% |
  | 🔥 AGRESIF | 40% | 45% |

#### Guard Layers AI
- **Macro Trend Guard (M5/M15)**: Sinyal BUY diblokir jika M5/M15 bearish, sinyal SELL diblokir jika M5/M15 bullish.
- **Anti-Pucuk Guard**: BUY diblokir jika `upperWick > 30%` atau `RSI > 68`.
- **Anti-Lembah Guard**: SELL diblokir jika `lowerWick > 30%` atau `RSI < 32`.
- **Anti-Duplicate Signal Guard**: Guard `candleClosedThisTick` mencegah duplikasi sinyal OnTick + OnCandle pada penutupan M1 yang sama.

### 2. 🎯 Dual-Strategy Engine (`internal/strategy/`)

#### Momentum Scalper (Trend Hunter)
Sinyal entry berdasarkan 4 kondisi:
1. **Golden Cross**: `prevFast ≤ prevSlow → fastEMA > slowEMA` + RSI < 70 + anti-extension 1.5x ATR
2. **Death Cross**: `prevFast ≥ prevSlow → fastEMA < slowEMA` + RSI > 30 + anti-extension
3. **Pullback BUY**: Established uptrend + price dips into FastEMA zone + bullish close + RSI 38–68
4. **Rally SELL**: Established downtrend + price rallies into FastEMA zone + bearish close + RSI 32–62

#### Mean-Reversion Range Scalper (Sideways Session)
- **BUY**: `candle.Low ≤ BB.Lower` atau `%B ≤ 0.15` **AND** `RSI ≤ 38`
- **SELL**: `candle.High ≥ BB.Upper` atau `%B ≥ 0.85` **AND** `RSI ≥ 62`
- **Bandwidth Guard**: Hanya aktif saat `bbBandwidth ≤ 0.005` (pasar ranging/sempit)

### 3. 🛡️ Institutional Risk Management (`internal/risk/`, `internal/executor/`)

#### Lot Sizing Formula (ATR-Proportional)
```
riskAmount     = equity × riskPerTrade         // 900 USC × 0.5% = 4.50 USC per trade
atrPips        = ATR × pipMultiplier
pipValuePerLot = PipValue(symbol, 1.0)
lots           = riskAmount / (atrPips × pipValuePerLot)
lots           = clamp(lots, minLot=0.01, maxLot=0.01)  // Fixed 0.01 lot untuk 900 USC
```

#### Guard Chain Risk Manager
| Guard | Kondisi | Error |
|-------|---------|-------|
| 0 | Market/session filter | ErrFilterRejected |
| 2 | Circuit breaker open | ErrCircuitOpen |
| 3 | DailyPnL loss ≥ 5% equity | ErrDailyDrawdownHit |
| 3b | DailyPnL profit ≥ 50 USC | ErrDailyProfitTargetReached |
| 4 | Open positions ≥ 2 | ErrMaxPositionsOpen |
| 4b | Symbol positions ≥ 1 | Reject: symbol sudah ada posisi |
| 5 | Spread > maxSpread (Gold: 60 pip, JPY: 3.5 pip, FX: 2 pip) | ErrSpreadTooWide |

#### Position Lifecycle (per-tick OnTick steps)
| Step | Guard | Trigger |
|------|-------|---------|
| 1 | P&L calc | Setiap tick |
| 2 | Time-Stop | elapsed ≥ 35 menit → AUTO CLOSE |
| 3 | Partial TP1 | pnlPips ≥ 5.0 pips (opsional, saat ini off) |
| 4 | Break-Even | pnlPips ≥ 8.0 pips → SL ke Entry+1pip |
| 5 | Trailing Stop | New HWM → ratchet SL (12 pips fixed / 1.5×ATR) |
| 6 | SL Hit | Bid/Ask crosses StopLoss |
| 7 | TP Hit | Bid/Ask crosses TakeProfit |

#### Gold-Specific Calibration
| Parameter | Nilai | Keterangan |
|-----------|-------|-----------|
| MinSLDistance | $0.50 | Minimum SL distance |
| MinTPDistance | $1.00 | Minimum TP distance |
| **MaxSL Hard-Cap** | **$1.50** | Pipeline cap — ATR tidak boleh melebihi ini |
| MaxTPDistance | $3.00 | Maximum TP cap |
| StagnantMinutes | 8 menit | Exit lebih cepat dari Gold stagnan |

### 4. 🌐 Multi-Symbol Portfolio & Correlation Guard (`internal/portfolio/`)
- Proses multi-pair bersamaan tanpa *lock contention* per pipeline.
- **Correlation Guard**: Blokir posisi kumulatif pada pair berkorelasi (EURUSD + GBPUSD).
- Suffix broker otomatis: `EURUSDc`, `XAUUSDc`, `EURUSD.m`, `XAUUSD.pro`.

### 5. 🔌 MetaTrader 5 Native TCP IPC Bridge
- **Port 5555 (REQ-REP)**: Eksekusi order (BUY/SELL/CLOSE/POSITIONS/ACCOUNT/HISTORY).
- **Port 5556 (PUB-SUB)**: Streaming tick berkecepatan tinggi JSON.
- Latensi round-trip: **24.6 µs**. Dual-attempt execution: attempt 1 BUY+SL/TP, attempt 2 jika broker tolak SL → market open + PositionModify.

### 6. 💾 Disk Persistence Layer (`internal/storage/`)
- `data/trade_history.json` — seluruh riwayat transaksi selesai.
- `data/daily_state.json` — status profit harian & circuit breaker.
- Auto-load saat restart, anti-duplikasi tiket, 1-to-1 parity dengan tab History MT5.

### 7. 🎛️ Control Center Web Dashboard (`internal/web/`, `web/`)
- TradingView Lightweight Charts, multi-timeframe (15S/1M/5M), crosshair hover OHLC.
- Analytics tab: Win Rate, Profit Factor, Gross P&L, Realized R:R, per-symbol breakdown.
- Mode & Focus switcher real-time via WebSocket.

### 8. 📱 Telegram Remote Control Bot (`internal/notifier/`)
- Notifikasi instan: trade buka/tutup, TP/SL hit, circuit breaker.
- Remote: ganti mode, ganti fokus, status akun, Emergency Close All.

---

## 📊 Hasil Benchmark Performa CPU & Memori

| Modul & Fungsi | Latensi Eksekusi | Alokasi Memori |
|---|---|---|
| **EMA Indicator Update** | **5.02 ns/op** | **0 B/op** |
| **RSI Indicator Update** | **6.84 ns/op** | **0 B/op** |
| **ATR Indicator Update** | **8.19 ns/op** | **0 B/op** |
| **Gaussian HMM Update** | **36 ns/op** | **0 B/op** |
| **AI GBDT Decision Tree** | **15.49 ns/op** | **0 B/op** |
| **Portfolio Correlation Guard** | **18.20 ns/op** | **0 B/op** |
| **Tick RingBuffer Push** | **26.71 ns/op** | **0 B/op** |
| **Position Tracker OnTick** | **120.60 ns/op** | **0 B/op** |
| **AI 12-Feature Extractor** | **138.50 ns/op** | **0 B/op** |
| **MT5 IPC Round-Trip** | **24.60 µs/op** | Minimal TCP I/O |

---

## 📁 Struktur Direktori Proyek

```
Project Scalping/
├── SYSTEM_ANALYSIS_EXPORT.md          # Audit teknis lengkap: logika kode, formula, flowchart
├── go.mod                              # Definisi module Go & dependensi
├── go.sum                              # Checksum integritas paket
├── Makefile                            # Target build, test, dan run
├── bin/
│   ├── scalpbot.exe                    # Bot trading utama & server web dashboard
│   └── backtester.exe                  # High-speed historical tick backtester
├── bridge/
│   ├── MT5_Bridge.mq5                  # Expert Advisor jembatan native TCP untuk MT5
│   └── MT5_Bridge.ex5                  # Biner terkompilasi EA MT5
├── cmd/
│   ├── bot/main.go                     # Multi-symbol orchestrator, web server, & sync loops
│   └── backtest/main.go                # CLI engine untuk backtest historis
├── config/
│   ├── config.go                       # Struct parser YAML & environment overrides
│   └── default.yaml                    # Konfigurasi parameter trading & risk management
├── data/
│   ├── trade_history.json              # Database permanen transaksi selesai
│   └── daily_state.json                # Status profit harian & circuit breaker
├── internal/
│   ├── ai/
│   │   ├── hmm/gaussian_hmm.go         # Gaussian HMM 3-State Engine (36 ns/op, 0 B/op)
│   │   ├── features.go                 # Zero-alloc 12-feature extractor
│   │   ├── regime.go                   # Market regime classifier
│   │   ├── scorer.go                   # GBDT decision tree ensemble scorer
│   │   └── filter.go                   # AI coordinator, gate routing, HMM bridge
│   ├── backtest/                       # High-Speed Tick Backtester
│   ├── broker/                         # Broker abstraction & MT5 TCP adapter
│   ├── executor/
│   │   ├── dispatcher.go               # Worker pool order execution
│   │   └── tracker.go                  # Position Tracker (Trailing/BE/Stagnant Killer)
│   ├── indicator/                      # EMA, RSI, ATR, Bollinger (streaming, 0-alloc)
│   ├── marketdata/                     # RingBuffer & OHLCV Aggregator (M1/M5/M15)
│   ├── model/                          # Domain: Tick, Candle, Order, Signal, Position
│   ├── news/                           # Live Economic Calendar & News Blackout
│   ├── notifier/                       # Telegram Interactive Bot & Webhook
│   ├── portfolio/                      # Multi-Symbol Correlation Guard & Sizing
│   ├── risk/                           # Risk Manager & Circuit Breakers
│   ├── storage/                        # Thread-Safe Disk Persistence
│   └── web/                            # WebSocket Hub & HTTP Server
└── web/                                # Modern Glassmorphism Web Frontend
    ├── index.html
    ├── css/style.css                   # Deep Obsidian & Neon Glow design system
    └── js/app.js                       # WebSocket client, TradingView, analytics renderer
```

---

## 🛠️ Panduan Penggunaan & Instalasi

### 1. Kompilasi Binary Produksi
```powershell
$env:Path = "C:\Program Files\Go\bin;" + $env:Path
go build -ldflags="-s -w" -o bin/scalpbot.exe ./cmd/bot/
go build -ldflags="-s -w" -o bin/backtester.exe ./cmd/backtest/
```

### 2. Pemasangan EA di MetaTrader 5
1. MT5 → **File** → **Open Data Folder** → `MQL5/Experts/` → salin `bridge/MT5_Bridge.mq5`
2. MetaEditor (F4) → buka `MT5_Bridge.mq5` → **Compile** (F7)
3. Drag `MT5_Bridge` ke chart aktif → centang **"Allow Algo Trading"**
4. Pastikan tombol **Algo Trading** di toolbar berwarna **hijau**

### 3. Menjalankan ScalpBot
```powershell
.\bin\scalpbot.exe -broker mt5 -symbols "EURUSDc,GBPUSDc,USDJPYc,XAUUSDc"
```
Buka dashboard: **`http://127.0.0.1:8080`**

### 4. Menjalankan Seluruh Unit Test Suite
```powershell
go test ./...                          # Seluruh unit test (19 packages, 100% PASS)
go test -bench=. -benchmem ./internal/ # Benchmark alokasi memori
go test -v ./internal/ai/hmm/...       # Test HMM spesifik dengan verbose
```

---

## ⚙️ Variabel Lingkungan & Konfigurasi

| Variabel | Default | Keterangan |
|---|---|---|
| `SCALP_SYMBOL` | `EURUSD` | Simbol default |
| `SCALP_BROKER_TYPE` | `mt5` | `mock` atau `mt5` |
| `SCALP_MT5_CMD` | `127.0.0.1:5555` | Socket REQ-REP order |
| `SCALP_MT5_STREAM` | `127.0.0.1:5556` | Socket PUB tick stream |
| `SCALP_WEB_PORT` | `8080` | Port HTTP dashboard |
| `SCALP_AI_ENABLED` | `true` | Filter AI HMM aktif |
| `SCALP_TG_TOKEN` | — | Token Bot Telegram |
| `SCALP_TG_CHAT` | — | Chat ID Telegram |

---

## 📄 Lisensi & Disclaimer

Perangkat lunak ini dikembangkan untuk keperluan riset trading kuantitatif institusional. Selalu lakukan pengujian mendalam pada akun demo dan backtesting historis sebelum menggunakan modal nyata. Past performance tidak menjamin hasil masa depan.
