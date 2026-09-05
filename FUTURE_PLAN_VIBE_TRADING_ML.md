# Rencana Pengembangan Masa Depan (Future Plan): ML & Vibe-Trading Integration

Dokumen ini mendokumentasikan visi dan *roadmap* teknis untuk integrasi Machine Learning tingkat lanjut serta riset faktor kuantitatif menggunakan framework **HKUDS/Vibe-Trading**.

---

## 1. Integrasi HKUDS/Vibe-Trading (AI Quant Research Lab)

### A. Konsep Kolaborasi
* **Vibe-Trading (Python + Multi-Agent Swarm)**: Berfungsi sebagai *Laboratorium Riset & Backtesting*.
  - Meneliti data historis multi-tahun (M1, M5, M15).
  - Menguji 450+ rumus faktor kuantitatif (*Alpha Zoo*).
  - Melakukan simulasi portofolio dan mendiagnosis bias trader (*Shadow Account*).
* **ScalpBot (Go Native)**: Berfungsi sebagai *Mesin Eksekusi Lapangan (Execution Engine)*.
  - Menerima model/bobot hasil riset dari Vibe-Trading.
  - Mengeksekusi order tick-by-tick sub-milidetik di MetaTrader 5 (MT5).
  - Mengawal manajemen risiko real-time (Drawdown limit, Trailing ATR, News blackout).

### B. Langkah Implementasi Masa Depan
1. **Setup Environment**: Instalasi `HKUDS/Vibe-Trading` di environment lokal/server terpisah untuk riset.
2. **Eksplorasi Alpha Factor**: Menjalankan prompt analisis spesifik untuk Gold (XAUUSD):
   - *"Temukan kombinasi Alpha Factor volatilitas dan volume imbalance yang memiliki Sharpe Ratio > 1.8 pada sesi London Open."*
3. **Konversi Formula**: Rumus matematika terbaik yang ditemukan Vibe-Trading diterjemahkan ke dalam modul Go di `internal/indicator/` atau `internal/ai/features.go`.

---

## 2. Roadmap Machine Learning dari Data Transaksi Historis

### A. Pipeline Pelatihan Data (Offline Training)
1. **Ekspor Data**: Mengambil riwayat tick dan candle M1/M5 XAUUSD dari broker MT5 (minimal 12-24 bulan terakhir).
2. **Feature Engineering**: Menggunakan 12 fitur inti yang sudah ada di `internal/ai/features.go`:
   - `FeatEMADelta`: Deviasi tren.
   - `FeatRSINorm`: Ekstremitas momentum.
   - `FeatSpreadRatio`: Biaya transaksi real-time.
   - `FeatTickVelocity`: Tekanan order transaksi (TPS).
   - `FeatBodyRatio`, `FeatUpperWickRatio`, `FeatLowerWickRatio`: Struktur candlestick (microstructure).
   - `FeatVolRatio`: Rasio ekspansi volatilitas.
3. **Metode Labeling (Triple Barrier Method)**:
   - Target Upper Barrier (+1): Kena TP duluan.
   - Target Lower Barrier (-1): Kena SL duluan.
   - Time Barrier (0): Flat/stagnan.
4. **Model Training**:
   - Melatih model **LightGBM** / **CatBoost** di Python dengan *Purged K-Fold Cross Validation* untuk mencegah *overfitting*.

### B. Runtime Inference di Bot Go
1. Ekspor pohon keputusan (*decision tree splits*) dari model Python ke format biner atau JSON.
2. Perbarui modul `internal/ai/scorer.go` agar mengimpor bobot model tersebut.
3. Bot mengevaluasi probabilitas kemenangan dalam < 1 mikrodetik per tick tanpa beban komputasi Python.

---

## 3. Strategi Khusus Pasar Emas (XAUUSD) yang Akan Datang

1. **Asian Session Mean-Reversion (Range Scalper)**:
   - Mengaktifkan `internal/strategy/range_scalper.go` khusus jam 01:00 – 10:00 WIB untuk memanen pips kecil di pasar tenang.
2. **London Opening Range Breakout (ORB)**:
   - Mengeksekusi breakout impulsif pada 30 menit pertama sesi London (14:00 – 15:30 WIB).
3. **Fair Value Gap (FVG) / Order Block Retest**:
   - Memasang limit order di area diskon candle impulsif untuk menghindari beli di pucuk (*buying the high*).
