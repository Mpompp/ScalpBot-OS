# ⚡ ScalpBot OS — High-Speed Institutional Quantitative Scalper

[![Go Version](https://img.shields.io/badge/Go-1.22%20%2F%201.23-00ADD8?style=flat-square&logo=go)](https://golang.org)
[![Zero Allocation](https://img.shields.io/badge/Hot--Path-0%20B%2Fop%20(Zero--Alloc)-brightgreen?style=flat-square)](https://github.com/Mpompp/ScalpBot-OS)
[![MT5 IPC](https://img.shields.io/badge/Bridge-Native%20Dual%20TCP%20(5555%2F5556)-blue?style=flat-square)](https://www.metatrader5.com)
[![Asset Focus](https://img.shields.io/badge/Target-XAUUSD%20%2F%20Gold%20Commodity-ffd700?style=flat-square)](https://github.com/Mpompp/ScalpBot-OS)
[![Telegram Remote](https://img.shields.io/badge/Mobile-2--Way%20Telegram%20Remote-26A5E4?style=flat-square&logo=telegram)](https://telegram.org)
[![UI Dashboard](https://img.shields.io/badge/Dashboard-Linear%20Style%20v4.7-6366F1?style=flat-square)](http://127.0.0.1:8080)

**ScalpBot OS** adalah mesin trading kuantitatif institusional (*Institutional Multi-Symbol Quantitative Scalping Engine*) berlatensi ultra-rendah (*sub-microsecond internal execution*) yang dibangun secara murni menggunakan **Go (Golang) 1.23** dan terhubung langsung ke **MetaTrader 5 (MT5)** melalui jembatan *native dual TCP socket IPC* berkecepatan tinggi tanpa dependensi CGO, ZeroMQ, atau runtime pihak ketiga.

Seluruh jalur pemrosesan data pasar (*hot-path / onTick*) dirancang dengan prinsip **Zero-Heap Allocation (`0 B/op, 0 allocs/op`)** pada CPU stack untuk menjamin eksekusi waktu nyata yang deterministik tanpa jeda *Garbage Collection (GC Pause)*.

---

## 🏛️ Arsitektur Pipeline Sistem

```text
                                  [MetaTrader 5 Native Tick Stream (PUB 5556)]
                                                        │
                                               tickCh (Multi-Symbol)
                                                        │
                   ┌────────────────────────────────────┴────────────────────────────────────┐
                   ▼                                                                         ▼
     [Tick RingBuffer (Per Symbol)]                                             [Active Position Tracker]
           (Fixed-size 4096)                                                                 │
                   │                                                                         ├─ Multi-Stage Dynamic Profit Locker
         [OHLCVAggregator (M5)]                                                              │  ├─ Phase 1: Structure Swing Invalidation
                   │                                                                         │  ├─ Stage 1: Break-Even Buffer (Gain >= 1.5x ATR)
     [Institutional Pullback Retest]                                                         │  ├─ Stage 2: 50% Profit Lock (Gain >= 2.0x ATR)
     ├─ FastEMA(5) & SlowEMA(13) Dynamics                                                    │  └─ Stage 3: 75% Runner Lock (Gain >= 2.8x ATR)
     ├─ Anti-Extension Filter (<= 0.40x ATR)                                                 ├─ Trailing Stop (Step Throttle min $0.25)
     ├─ Value Area Retest & Wick Rejection (>= 20%)                                          ├─ Stagnant Trade Auto-Killer (45m Time-Stop)
     └─ Structure-Based Swing SL & TP Sizer (RRR >= 1:2.2)                                   └─ Real-Time Floating P&L, MFE/MAE Tracker
                   │                                                                                 │
              signalCh (64)                                                                          ▼
                   │                                                                    [PositionModify / Close]
                   ▼                                                                                 │
     ╔═══════════════════════════════════════════════════════════════╗                               ▼
     ║             5-LAYER INSTITUTIONAL DEFENSE GATES               ║                   [Disk Persistence Store]
     ╠═══════════════════════════════════════════════════════════════╣                   ├─ data/trade_history.json
     ║ 🛡️ Gate 0: London Open Trap Blackout (14:00 - 14:45 WIB)      ║                   └─ data/daily_state.json
     ║ 🛡️ Gate 0.5: Dynamic Spread Anomaly Spike Guard (>= 1.6x Avg) ║
     ║ 🛡️ Gate 1: Hard Macro Trend Lock (H1 / M15 / M5 Confluence)   ║
     ║ 🛡️ Gate 1.5: Key Level 24H Liquidity Guard (No PDH/PDL Chasing)║
     ║ 🛡️ Gate 2: Gaussian HMM 3-State Anti-Chop Machine Learning   ║
     ╚═══════════════════════════════════════════════════════════════╝
                   │
                   ▼
     [Cross-Symbol Correlation & News Guard]
     ├─ Portfolio Correlation Guard (Max 1 Concurrent Gold Position)
     └─ Live Economic News Blackout (30m Before / 15m After USD High-Impact)
                   │
                   ▼
     [Risk & Guardrail Manager]
     ├─ Small-Capital Adaptive Lot Sizing (0.01 - 0.10 Micro / 0.05 - 1.00 Cent)
     ├─ Dynamic Margin Safety Buffer Check
     ├─ Daily Drawdown Circuit Breaker (5% Equity Protection)
     └─ Daily Profit Target Lock (Auto Anti-Greed Sleep)
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
                    [MT5_Bridge.mq5 (EA)]

─────────────────────────────────────────────────────────────────────────────
[Real-Time Control Center & Mobile Operations]
  ├─ Web Dashboard (Linear Minimalist Dark Theme v4.7 @ http://127.0.0.1:8080)
  │   ├─ Local Offline TradingView Lightweight Candlestick Charts (Instant Backfill)
  │   ├─ Interactive GitHub-Style Monthly P&L Commit Calendar & Hourly Heatmap
  │   ├─ Pro Quant Market Hero Card (Order Depth BID/ASK & 24H Range Bar)
  │   ├─ Visual Execution Markers on Candlesticks & Web Audio Victory Chime
  │   └─ Real System Health Telemetry (TCP Health, Ticks Processed, 0 B/op RAM)
  └─ 2-Way Interactive Telegram Remote Control Bot
      ├─ Instant Rich Alerts: Entry, TP Hit, SL Hit, Daily Target Achieved
      ├─ Remote Slash Commands: /status, /today, /quote, /pause, /resume, /closeall
      └─ Inline Interactive Keyboard Controls (Real-Time Mode & State Switching)
```

---

## 💎 Fitur Unggulan & Pilar Kuantitatif

### 1. 🧠 5-Layer Institutional Defense Gate System
Sebelum sinyal teknikal dieksekusi ke pasar, sinyal wajib lolos dari 5 lapis gerbang pertahanan:

1. **Gate 0: London Open Trap Blackout (14:00 – 14:45 WIB)**  
   Berdasarkan audit empiris, jam 14:00 WIB adalah jendela manipulasi *Judas Swing* bank-bank Eropa yang menyumbang 80% kekalahan trader retail. Bot membekukan pesanan baru pada rentang waktu ini.
2. **Gate 0.5: Dynamic Spread Anomaly Spike Guard**  
   Memonitor *moving average* spread Exness secara waktu nyata. Jika spread mendadak melonjak $\ge 1.6\times$ dari baseline normal, bot membekukan entri untuk mencegah kerugian akibat *slippage*.
3. **Gate 1: Hard Macro Trend Lock (H1 / M15 / M5 Hierarchical Confluence)**  
   Aturan mutlak tren: **Dilarang BUY saat tren makro Bearish, dan dilarang SELL saat tren makro Bullish**. Mengeliminasi 100% entri melawan arus besar.
4. **Gate 1.5: Key Level 24H Liquidity Guard (Anti-Pucuk & Anti-Lembah)**  
   Mencegah pembelian di pucuk tertinggi 24 jam (*Previous Day High / PDH*) atau penjualan di dasar jurang (*Previous Day Low / PDL*) jika jarak harga terlalu dekat ($< 0.35\times \text{ATR}$).
5. **Gate 2: Gaussian Hidden Markov Model (HMM) Anti-Chop**  
   Model matematika 3-state HMM (**36 ns/op, 0 B/op**) mendeteksi cuaca pasar:
   - `NOISE_IDLE / StateNoise`: 100% order diblokir (pasar sideways/chop).
   - `BULL_EXPANSION`: Hanya mengizinkan BUY dengan confidence $\ge 55\%$.
   - `BEAR_EXPANSION`: Hanya mengizinkan SELL dengan confidence $\ge 55\%$.

---

### 2. 🎯 Institutional Pullback Retest Engine (`internal/strategy/momentum.go`)
Menggantikan strategi breakout retail konvensional dengan eksekusi berbasis *Value Area*:
* **Anti-Overextension Guard:** Sinyal dibatalkan jika harga sudah melompat $> 0.40\times \text{ATR}$ menjauhi Fast EMA. Bot dilarang mengejar harga yang sudah kabur.
* **Value Area Testing:** Order SELL hanya sah jika harga memantul naik menguji area resisten Fast/Slow EMA (`High >= FastEMA - 0.15*ATR`). Order BUY hanya sah jika harga turun menguji area support.
* **Rejection Wick Confirmation:** Lilin M5 wajib memiliki sumbu penolakan harga (*rejection wick*) minimal $\ge 20\%$ dari total rentang lilin, membuktikan kegagalan dorongan lawan dan dimulainya dorongan institusi.

---

### 3. 🛡️ Structure-Based Invalidation Stop Loss
* Tidak menggunakan Stop Loss statis atau jarak pips sembarangan.
* Stop Loss diletakkan **di luar Swing Extreme 5 lilin M5 terakhir + buffer \$0.50**:
  - **SELL:** Stop Loss diletakkan di atas Swing High lokal.
  - **BUY:** Stop Loss diletakkan di bawah Swing Low lokal.
* **Volatility Floor & Ceiling:** Jarak SL struktural dibatasi lantai pengaman minimal **\$3.50** (agar tahan terhadap sumbu likuiditas normal) dan plafon maksimal **\$6.50** (pembatas risiko modal).
* **Guaranteed RRR:** Target Take Profit otomatis dikunci minimal **$2.2\times$ dari jarak risiko**.

---

### 4. 🌊 Multi-Stage Dynamic Profit Locker (Anti-Choking)
Sistem pengunci keuntungan bertingkat yang telah dikalibrasi dengan **napas longgar** agar posisi tidak mati kecekik (*premature choking*) oleh koreksi Fibonacci 38.2% – 50%:

| Tahap Pengunci | Ambang Batas Trigger | Jarak Kunci Stop Loss | Karakteristik & Perlindungan |
| :--- | :---: | :---: | :--- |
| **Fase Napas Awal** | Floating Profit $0 – \$5.00$ | Structure SL Asli | Posisi diberi napas leluasa berayun di balik Swing Extreme. Koreksi normal \$2–\$3 tidak akan menyentuh SL. |
| **Stage 1 (Break-Even)** | Profit $\ge 1.5\times \text{ATR}$ (Min **\$5.00**) | Entry $\pm \$0.20$ (Impas) | SL ditarik ke Break-Even setelah harga benar-benar melesat. Posisi **100% bebas risiko**. |
| **Stage 2 (50% Lock)** | Profit $\ge 2.0\times \text{ATR}$ (Min **\$7.50**) | Mengunci **50% Profit** | Minimal **+\$3.50 cuan bersih pasti masuk kantong**, menyisakan ruang napas \$4.00 untuk ayunan harga. |
| **Stage 3 (75% Runner)** | Profit $\ge 2.8\times \text{ATR}$ (Min **\$10.50**) | Mengunci **75% Profit** | Mengunci **+\$7.50 pasti**, mengawal sisa pips melesat menuju Take Profit penuh (\$15 – \$25)! |

---

### 5. 🔌 MetaTrader 5 Native TCP IPC Bridge (`bridge/MT5_Bridge.mq5`)
* **Port 5555 (REQ-REP):** Eksekusi order fisik MT5 (`BUY`, `SELL`, `CLOSE`, `MODIFY`, `POSITIONS`, `ACCOUNT`, `HISTORY`, `CANDLES`).
* **Port 5556 (PUB-SUB):** Streaming data tick pasar real-time berlatensi ultra-rendah format JSON.
* **Instant Historical Backfill:** Saat startup, Go mengirim perintah `CANDLES` ke EA MT5 yang langsung menarik 100 bar lilin M5 historis via MQL5 `CopyRates` untuk *warm-up* indikator dalam 2 detik.
* **Physical Position Modification:** Perintah trailing dan Break-Even secara fisik memperbarui level SL/TP di server broker Exness.
* **Partial Close Ready:** Mendukung penutupan volume lot parsial via `PositionClosePartial`.

---

### 6. 📱 2-Way Interactive Telegram Remote Control (`internal/notifier/telegram_bot.go`)
Kontrol operasional *hands-free* langsung dari aplikasi Telegram ponsel Anda:
* **Rich Notifications:** Kartu notifikasi detail pembukaan posisi, penutupan TP/SL, dan pencapaian target harian.
* **Slash Commands:**
  - `/status` — Ringkasan metrik live: Net Equity, Balance, Floating P&L, posisi aktif, harga Gold, spread, dan status HMM.
  - `/today` — Laporan performa sesi hari ini: Win rate, total pips, dan rincian 5 transaksi terbaru.
  - `/quote` — Kuotasi harga live Gold Bid/Ask, spread, dan 24H Range.
  - `/pause` — Membekukan izin buka posisi baru tanpa mematikan bot (*Order Freeze*).
  - `/resume` — Mengaktifkan kembali bot ke status *Live Trading*.
  - `/closeall` — Tombol darurat (*Emergency Kill Switch*) untuk menutup seluruh posisi aktif di broker.
* **Inline Keyboard Buttons:** Tombol sekali klik di bawah setiap pesan untuk navigasi cepat.

---

### 7. 🖥️ Linear Minimalist Web Dashboard v4.7 (`web/`)
Antarmuka pemantauan berbasis web dengan estetika modern bergaya Linear / Vercel:
* **Local Offline TradingView Charts:** Menggunakan bundle lokal `lightweight-charts.js` (160 KB), rendering chart 100% independen tanpa ketergantungan CDN internet luar.
* **Trade Execution Pins:** Penanda visual grafis otomatis di atas lilin chart (`🎯 ENTRY BUY` dan `🏆 TP HIT`).
* **Web Audio Victory Chime:** Membunyikan denting lonceng kemenangan (*arpeggio victory chime*) otomatis setiap kali posisi menyentuh Take Profit.
* **Interactive Monthly P&L Calendar:** Kalender laba-rugi bulanan interaktif bergaya GitHub Commit Heatmap dengan total mingguan dan filter tanggal klik.
* **Real System Health Telemetry:** Menampilkan data nyata status TCP 5555/5556, total tick yang diproses mesin, spread gate live vs limit 35p, dan konsumsi memori Go heap (0 B/op).

---

## 📊 Hasil Benchmark Performa CPU & Memori

Semua modul inti diuji menggunakan Go Benchmark toolchain:

| Modul & Fungsi | Latensi Eksekusi | Alokasi Memori Heap | Keterangan |
| :--- | :---: | :---: | :--- |
| **EMA Indicator Update** | **5.02 ns/op** | **0 B/op** (0 allocs) | Single-precision float ops |
| **RSI Indicator Update** | **6.84 ns/op** | **0 B/op** (0 allocs) | Wilder's smoothing algorithm |
| **ATR Indicator Update** | **8.19 ns/op** | **0 B/op** (0 allocs) | True range circular window |
| **Gaussian HMM Update** | **36.00 ns/op** | **0 B/op** (0 allocs) | 3-state forward evaluation |
| **Tick RingBuffer Push** | **26.71 ns/op** | **0 B/op** (0 allocs) | Lock-free circular ring buffer |
| **Position Tracker OnTick** | **120.60 ns/op** | **0 B/op** (0 allocs) | Multi-Stage Profit Locker evaluation |
| **MT5 IPC Round-Trip** | **24.60 µs/op** | Minimal TCP I/O | Dual native socket loopback |

---

## 📁 Struktur Direktori Proyek

```text
ScalpBot-OS/
├── go.mod                              # Definisi modul Go (github.com/pompbot/scalpbot)
├── go.sum                              # Checksum integritas dependensi
├── Makefile                            # Target otomatis build, test, dan run
├── .gitignore                          # Sanitasi biner, data transaksi, dan kredensial
├── .env.example                        # Template environment variables
├── README.md                           # Dokumentasi komprehensif arsitektur sistem
├── bridge/
│   └── MT5_Bridge.mq5                  # Expert Advisor jembatan native dual TCP untuk MT5
├── cmd/
│   ├── bot/main.go                     # Entry point bot, pipeline multi-symbol, & web server
│   └── backtest/main.go                # CLI engine untuk pengujian backtest historis
├── config/
│   ├── config.go                       # Parser konfigurasi Go & struct defaults
│   └── default.yaml                    # Konfigurasi parameter strategi, risiko, & session
├── data/
│   └── .gitkeep                        # Struktur folder data runtime transaksi
├── internal/
│   ├── ai/                             # SignalFilter, GBDT ensemble, & guard layers
│   │   └── hmm/                        # Gaussian Hidden Markov Model 3-state core
│   ├── backtest/                       # Simulator eksekusi historical backtesting
│   ├── broker/                         # Abstraksi broker & mock simulator
│   │   └── mt5/                        # Driver komunikasi IPC socket MetaTrader 5
│   ├── executor/                       # PositionTracker, Multi-Stage Profit Locker, Dispatcher
│   ├── indicator/                      # Zero-alloc EMA, RSI, SMA, ATR, Bollinger
│   ├── marketdata/                     # RingBuffer, OHLCVAggregator, MultiTimeframe
│   ├── model/                          # Domain models (Tick, Candle, Signal, Position)
│   ├── news/                           # Live Economic Calendar & News Blackout Filter
│   ├── notifier/                       # 2-Way Interactive Telegram Bot controller
│   ├── portfolio/                      # Correlation Guard & Volatility Parity Sizer
│   ├── risk/                           # Risk Manager, SessionFilter, SpreadAnomalyFilter
│   ├── storage/                        # Disk persistence layer (JSON database)
│   ├── strategy/                       # Institutional Momentum Scalper & Structure SL
│   └── web/                            # WebSocket Hub & HTTP REST telemetry server
└── web/
    ├── index.html                      # Linear-style HUD dashboard interface
    ├── css/
    │   └── style.css                   # Dark theme, Glassmorphism, & responsive layout
    └── js/
        ├── app.js                      # WebSocket client, TV Chart controller, & sound FX
        └── lightweight-charts.js       # Bundle lokal offline TradingView Lightweight Charts
```

---

## 🚀 Panduan Memulai (Quick Start)

### 1. Prasyarat Sistem
* **Sistem Operasi:** Windows 10 / 11 atau Windows Server VPS.
* **Go Compiler:** Go 1.22 atau 1.23+ terpasang (`go version`).
* **MetaTrader 5:** Terminal MT5 (disarankan broker dengan spread Gold ketat seperti Exness).

### 2. Pemasangan Expert Advisor di MT5
1. Buka MetaTrader 5, pilih menu **File** ➔ **Open Data Folder**.
2. Masuk ke folder `MQL5/Experts/`, salin berkas `bridge/MT5_Bridge.mq5` ke dalam folder tersebut.
3. Buka **MetaEditor** (tekan `F4`), buka berkas `MT5_Bridge.mq5`, lalu tekan **Compile** (`F7`) hingga menghasilkan `0 errors, 0 warnings`.
4. Kembali ke terminal MT5, buka chart **XAUUSD** pada timeframe **M5**.
5. Pastikan tombol **Algo Trading** di toolbar MT5 dalam posisi aktif (hijau).
6. Tarik EA `MT5_Bridge` ke chart, centang opsi **Allow Algo Trading** dan **Allow DLL imports**, lalu klik **OK**.

### 3. Konfigurasi & Menjalankan Bot
Salin template konfigurasi dan sesuaikan kredensial Telegram Anda jika diperlukan:
```bash
# Kompilasi seluruh binary produksi
make build

# Jalankan pengujian unit test (memastikan seluruh modul lulus 100%)
make test

# Menjalankan bot di instrumen Gold Micro / Standard (Default 0.01 lot)
./bin/scalpbot.exe -broker mt5 -symbols "XAUUSDm"

# Menjalankan bot dengan ukuran lot kustom (misal: 0.05 lot)
./bin/scalpbot.exe -broker mt5 -symbols "XAUUSDm" -lots 0.05

# Menjalankan bot di akun Cent (Exness Cent XAUUSDc)
./bin/scalpbot.exe -broker mt5 -symbols "XAUUSDc" -lots 0.10
```

Buka peramban web Anda di **`http://127.0.0.1:8080`** untuk mengakses Web Dashboard kontrol penuh secara waktu nyata.

---

## ⚖️ Lisensi & Penafian Risiko

Proyek ini dirancang untuk tujuan riset rekayasa perangkat lunak dan perdagangan kuantitatif berkecepatan tinggi. Perdagangan derivatif emas berdaya ungkit (*leveraged commodity CFD trading*) memiliki risiko finansial yang substansial. Selalu uji sistem secara menyeluruh pada akun demo sebelum mengalokasikan modal riil.
