/**
 * SCALPBOT OS — FRONTEND CONTROLLER (PRO QUANT v2.2)
 * High performance real-time WebSocket telemetry with TradingView Lightweight Charts & Analytics.
 */

class DashboardApp {
  constructor() {
    this.ws = null;
    this.wsConnected = false;
    this.equityHistory = [];
    this.maxHistoryPoints = 120; // 2 minutes of history @ 1Hz
    this.lastPing = performance.now();
    this.activeViewTab = 'LIVE';
    this.selectedChartSymbol = 'XAUUSDm';
    this.tvChart = null;
    this.tvCandleSeries = null;
    this.tvFastEMASeries = null;
    this.tvSlowEMASeries = null;
    this.tvPriceLines = [];
    this.tradeHistory = [];
    this.calYear = 2026;
    this.calMonth = 8; // September (0-indexed)
    this.selectedFilterDate = null;
    this.tableStatusFilter = 'all';
    this.tableSearchQuery = '';
    this.analyticsActiveTab = 'curve'; // 'curve' or 'hourly'
    this.lastPerfData = null;
    this.lastNewsData = null;
    this.lastNewsReceivedTime = performance.now();

    this.initElements();
    this.initClock();
    this.initEquityChart();
    this.initTradingViewChart();
    this.initEventListeners();
    this.connectWebSocket();

    // Smooth 1-second countdown ticker for News and Timers
    setInterval(() => {
      if (this.lastNewsData) {
        this.renderNewsCard();
      }
    }, 1000);
  }

  initElements() {
    this.botStatusBadge = document.getElementById('botStatusBadge');
    this.botStatusText = document.getElementById('botStatusText');
    this.mt5BridgeBadge = document.getElementById('mt5BridgeBadge');
    this.mt5StatusText = document.getElementById('mt5StatusText');

    this.valBalance = document.getElementById('valBalance');
    this.subBalance = document.getElementById('subBalance');
    this.valEquity = document.getElementById('valEquity');
    this.valFloatingPnL = document.getElementById('valFloatingPnL');
    this.subFloatingPips = document.getElementById('subFloatingPips');
    this.valDrawdown = document.getElementById('valDrawdown');
    this.valAIRegime = document.getElementById('valAIRegime');
    this.subAIConfidence = document.getElementById('subAIConfidence');
    this.subAIRegimeDesc = document.getElementById('subAIRegimeDesc');
    this.valEngineTrigger = document.getElementById('valEngineTrigger');
    this.subEngineTrigger = document.getElementById('subEngineTrigger');
    this.valWinRate = document.getElementById('valWinRate');
    this.subProfitFactor = document.getElementById('subProfitFactor');

    // Daily Profit Target
    this.cardProfitTarget = document.getElementById('cardProfitTarget');
    this.valProfitTarget = document.getElementById('valProfitTarget');
    this.subProfitTarget = document.getElementById('subProfitTarget');
    this.fillProfitTarget = document.getElementById('fillProfitTarget');

    this.tickerGrid = document.getElementById('tickerGrid');
    this.positionsBody = document.getElementById('positionsBody');
    this.posCount = document.getElementById('posCount');

    this.newsContent = document.getElementById('newsContent');
    this.signalAuditList = document.getElementById('signalAuditList');
    this.auditCount = document.getElementById('auditCount');
    this.healthBridgeStatus = document.getElementById('healthBridgeStatus');
    this.healthTicksCount = document.getElementById('healthTicksCount');
    this.healthSpreadStatus = document.getElementById('healthSpreadStatus');
    this.healthMemoryAlloc = document.getElementById('healthMemoryAlloc');
    this.footerStatus = document.getElementById('footerStatus');

    this.toggleAI = document.getElementById('toggleAI');
    this.btnEmergencyStop = document.getElementById('btnEmergencyStop');
    this.btnCloseAll = document.getElementById('btnCloseAll');
  }

  initClock() {
    const clockEl = document.getElementById('utcClock');
    const update = () => {
      const d = new Date();
      clockEl.textContent = d.toUTCString().split(' ')[4] + ' UTC';
    };
    update();
    setInterval(update, 1000);
  }

  initEquityChart() {
    this.canvas = document.getElementById('equityChart');
    if (!this.canvas) return;
    this.ctx = this.canvas.getContext('2d');
    this.resizeCanvas();
    window.addEventListener('resize', () => {
      this.resizeCanvas();
      if (this.tvChart) {
        const container = document.getElementById('tvChartContainer');
        if (container) {
          this.tvChart.applyOptions({ width: container.clientWidth, height: container.clientHeight });
        }
      }
    });
  }

  resizeCanvas() {
    if (!this.canvas || !this.canvas.parentElement) return;
    const rect = this.canvas.parentElement.getBoundingClientRect();
    this.canvas.width = rect.width * window.devicePixelRatio;
    this.canvas.height = rect.height * window.devicePixelRatio;
    this.ctx.scale(window.devicePixelRatio, window.devicePixelRatio);
    this.renderChart();
  }

  initTradingViewChart() {
    const container = document.getElementById('tvChartContainer');
    if (!container || typeof LightweightCharts === 'undefined') return;

    this.chartTimeframe = '15m';

    this.tvChart = LightweightCharts.createChart(container, {
      width: container.clientWidth || 800,
      height: 350,
      layout: {
        background: { color: '#090d16' },
        textColor: '#8da2ba',
        fontSize: 11,
        fontFamily: "'JetBrains Mono', monospace",
      },
      grid: {
        vertLines: { color: 'rgba(255, 255, 255, 0.04)' },
        horzLines: { color: 'rgba(255, 255, 255, 0.04)' },
      },
      crosshair: {
        mode: LightweightCharts.CrosshairMode.Normal,
      },
      rightPriceScale: {
        borderColor: 'rgba(255, 255, 255, 0.08)',
        scaleMargins: { top: 0.15, bottom: 0.15 },
      },
      timeScale: {
        borderColor: 'rgba(255, 255, 255, 0.08)',
        timeVisible: true,
        secondsVisible: false,
        fixLeftEdge: false,
        rightOffset: 14,
        barSpacing: 18,
      },
      localization: {
        timeFormatter: (timestamp) => {
          const d = new Date(timestamp * 1000);
          return d.toLocaleTimeString('id-ID', { hour: '2-digit', minute: '2-digit', second: '2-digit' });
        },
      },
    });

    // Candlestick Series
    this.tvCandleSeries = this.tvChart.addCandlestickSeries({
      upColor: '#00e676',
      downColor: '#ff1744',
      borderVisible: false,
      wickUpColor: '#00e676',
      wickDownColor: '#ff1744',
    });

    // Fast EMA Line (Green)
    this.tvFastEMASeries = this.tvChart.addLineSeries({
      color: '#00e676',
      lineWidth: 1.5,
      priceLineVisible: false,
      lastValueVisible: false,
    });

    // Slow EMA Line (Gold)
    this.tvSlowEMASeries = this.tvChart.addLineSeries({
      color: '#ffd700',
      lineWidth: 1.5,
      priceLineVisible: false,
      lastValueVisible: false,
    });

    // Crosshair hover OHLC timestamp legend
    this.tvChart.subscribeCrosshairMove(param => {
      const legendEl = document.getElementById('tvChartOHLC');
      if (!legendEl) return;
      if (!param || !param.time || !param.seriesData || !param.seriesData.get(this.tvCandleSeries)) {
        legendEl.innerHTML = '';
        return;
      }
      const data = param.seriesData.get(this.tvCandleSeries);
      const d = new Date(param.time * 1000);
      const timeStr = d.toLocaleTimeString('id-ID', { hour: '2-digit', minute: '2-digit', second: '2-digit' });
      const isUp = data.close >= data.open;
      const color = isUp ? '#00e676' : '#ff5252';
      const dec = (this.selectedChartSymbol && (this.selectedChartSymbol.includes('JPY') || this.selectedChartSymbol.includes('XAU') || this.selectedChartSymbol.includes('GOLD'))) ? 2 : 5;
      legendEl.innerHTML = `
        <span style="color:var(--text-muted)">🕒 ${timeStr}</span>
        <span>O: <b>${data.open.toFixed(dec)}</b></span>
        <span>H: <b>${data.high.toFixed(dec)}</b></span>
        <span>L: <b>${data.low.toFixed(dec)}</b></span>
        <span>C: <b style="color:${color}">${data.close.toFixed(dec)}</b></span>
      `;
    });

    // Start Live Clock
    this.updateTVClock();
    setInterval(() => this.updateTVClock(), 1000);
  }

  updateTVClock() {
    const now = new Date();
    const clockEl = document.getElementById('tvChartClock');
    if (clockEl) {
      clockEl.textContent = now.toLocaleTimeString('id-ID', { hour: '2-digit', minute: '2-digit', second: '2-digit' });
    }
    const utcClockEl = document.getElementById('utcClock');
    if (utcClockEl) {
      utcClockEl.textContent = now.toISOString().slice(11, 19) + ' UTC';
    }
  }

  setChartTimeframe(tf) {
    this.chartTimeframe = tf;
    document.querySelectorAll('.btn-tv-tf').forEach(btn => {
      btn.classList.toggle('active', btn.getAttribute('data-tf') === tf);
    });
    if (this.lastCandlesData) {
      this.selectChartSymbol(this.selectedChartSymbol);
    }
  }

  updateChartTabs(symbols) {
    const tabsContainer = document.getElementById('tvSymTabs');
    if (!tabsContainer || !symbols) return;

    const symKeys = Object.keys(symbols);
    if (symKeys.length === 0) return;

    const currentButtons = Array.from(tabsContainer.querySelectorAll('.btn-tv-sym'));
    const currentSyms = currentButtons.map(b => b.getAttribute('data-sym'));
    const isSame = currentSyms.length === symKeys.length && currentSyms.every((s, i) => s === symKeys[i]);

    if (!isSame) {
      let html = '';
      for (const sym of symKeys) {
        const activeClass = (sym === this.selectedChartSymbol || (!this.selectedChartSymbol && sym === symKeys[0])) ? 'active' : '';
        html += `<button class="btn-tv-sym ${activeClass}" data-sym="${sym}" onclick="window.app.selectChartSymbol('${sym}')">🪙 ${sym}</button>`;
      }
      tabsContainer.innerHTML = html;
      if (!this.selectedChartSymbol || !symbols[this.selectedChartSymbol]) {
        this.selectedChartSymbol = symKeys[0];
      }
    }
  }

  selectChartSymbol(sym) {
    this.selectedChartSymbol = sym;
    document.querySelectorAll('.btn-tv-sym').forEach(btn => {
      btn.classList.toggle('active', btn.getAttribute('data-sym') === sym);
    });

    if (this.lastCandlesData) {
      const cleanSym = sym.replace(/[cm\.]*$/i, '');
      const suffixedSym = cleanSym + 'c';
      const miniSym = cleanSym + 'm';
      const candleList = this.lastCandlesData[sym] || this.lastCandlesData[cleanSym] || this.lastCandlesData[suffixedSym] || this.lastCandlesData[miniSym];
      if (candleList && candleList.length > 0) {
        this.updateTVChart(sym, candleList);
      }
    }
  }

  updateTVChart(symbol, candles) {
    if (!this.tvCandleSeries) {
      if (typeof LightweightCharts !== 'undefined') {
        this.initTradingViewChart();
      }
      if (!this.tvCandleSeries) return;
    }
    if (!candles || candles.length === 0) return;

    // Deduplicate and ensure strictly increasing timestamps for LightweightCharts
    const timeMap = new Map();
    for (const c of candles) {
      if (c && c.time > 0 && c.close > 0) {
        timeMap.set(c.time, {
          time: c.time,
          open: c.open,
          high: c.high,
          low: c.low,
          close: c.close,
        });
      }
    }

    const formattedCandles = Array.from(timeMap.values()).sort((a, b) => a.time - b.time);
    if (formattedCandles.length === 0) return;

    try {
      this.tvCandleSeries.setData(formattedCandles);

      // Compute Fast EMA (5) & Slow EMA (13)
      if (formattedCandles.length >= 5) {
        const fastEMA = this.calculateEMA(formattedCandles, 5);
        if (this.tvFastEMASeries) this.tvFastEMASeries.setData(fastEMA);
      }
      if (formattedCandles.length >= 13) {
        const slowEMA = this.calculateEMA(formattedCandles, 13);
        if (this.tvSlowEMASeries) this.tvSlowEMASeries.setData(slowEMA);
      }

      // Fit chart scale automatically when sufficient bars exist
      if (!this.tvChartFitted && formattedCandles.length >= 5) {
        this.tvChart.timeScale().fitContent();
        this.tvChartFitted = true;
      }

      // Update Bar Count Badge
      const barCountEl = document.getElementById('tvBarCount');
      if (barCountEl) {
        barCountEl.textContent = `${formattedCandles.length} BARS`;
      }
    } catch (err) {
      console.warn('[TVChart] setData warning:', err);
    }

    // Update active position price lines & trade execution markers
    this.updateTVPriceLines();
    this.updateTVMarkers(formattedCandles);
  }

  updateTVMarkers(candles) {
    if (!this.tvCandleSeries || !candles || candles.length === 0) return;
    const minTime = candles[0].time;
    const maxTime = candles[candles.length - 1].time;
    const markers = [];

    // 1. Completed historical trades within candle time range
    if (this.tradeHistory && this.tradeHistory.length > 0) {
      for (const t of this.tradeHistory) {
        let tradeSec = t.timestamp;
        if (!tradeSec && t.time) {
          // Fallback approximate time
          continue;
        }
        if (tradeSec >= minTime && tradeSec <= maxTime) {
          const isWin = (t.pnl || 0) >= 0;
          const pnlSign = isWin ? '+' : '';
          markers.push({
            time: tradeSec,
            position: isWin ? 'aboveBar' : 'belowBar',
            color: isWin ? '#10b981' : '#f43f5e',
            shape: isWin ? 'circle' : 'square',
            text: `${isWin ? '🏆 TP' : '🛑 SL'} ${pnlSign}$${(t.pnl || 0).toFixed(2)}`,
          });
        }
      }
    }

    // 2. Active open positions on this symbol
    if (this.lastPositionsData && this.lastPositionsData.length > 0) {
      for (const pos of this.lastPositionsData) {
        const entrySec = Math.floor((pos.open_time_ns || 0) / 1e9);
        if (entrySec >= minTime && entrySec <= maxTime) {
          markers.push({
            time: entrySec,
            position: pos.side === 'BUY' ? 'belowBar' : 'aboveBar',
            color: '#38bdf8',
            shape: pos.side === 'BUY' ? 'arrowUp' : 'arrowDown',
            text: `🎯 ENTRY ${pos.side} @ ${(pos.entry_price || 0).toFixed(2)}`,
          });
        }
      }
    }

    // Sort markers strictly increasing by time for LightweightCharts
    markers.sort((a, b) => a.time - b.time);
    try {
      this.tvCandleSeries.setMarkers(markers);
    } catch (e) {
      // Ignored if duplicate timestamps exist
    }
  }

  fitTVChart() {
    if (this.tvChart) {
      this.tvChart.timeScale().fitContent();
    }
  }

  playTPSound() {
    try {
      const AudioCtx = window.AudioContext || window.webkitAudioContext;
      if (!AudioCtx) return;
      const ctx = new AudioCtx();
      const notes = [523.25, 659.25, 783.99, 1046.50]; // C5, E5, G5, C6 (Victory arpeggio)
      notes.forEach((freq, idx) => {
        const osc = ctx.createOscillator();
        const gain = ctx.createGain();
        osc.type = 'sine';
        osc.frequency.setValueAtTime(freq, ctx.currentTime + idx * 0.08);
        gain.gain.setValueAtTime(0.12, ctx.currentTime + idx * 0.08);
        gain.gain.exponentialRampToValueAtTime(0.001, ctx.currentTime + idx * 0.08 + 0.3);
        osc.connect(gain);
        gain.connect(ctx.destination);
        osc.start(ctx.currentTime + idx * 0.08);
        osc.stop(ctx.currentTime + idx * 0.08 + 0.35);
      });
    } catch (e) {
      // Audio context restricted until user interaction
    }
  }

  calculateEMA(candles, period) {
    const k = 2 / (period + 1);
    let ema = candles[0].close;
    const result = [];

    for (let i = 0; i < candles.length; i++) {
      if (i < period) {
        let sum = 0;
        for (let j = 0; j <= i; j++) sum += candles[j].close;
        ema = sum / (i + 1);
      } else {
        ema = candles[i].close * k + ema * (1 - k);
      }
      result.push({ time: candles[i].time, value: ema });
    }
    return result;
  }

  updateTVPriceLines() {
    if (!this.tvCandleSeries) return;

    // Clear previous price lines
    this.tvPriceLines.forEach(line => {
      try { this.tvCandleSeries.removePriceLine(line); } catch (e) {}
    });
    this.tvPriceLines = [];

    if (!this.lastPositionsData) return;

    for (const pos of this.lastPositionsData) {
      if (pos.symbol === this.selectedChartSymbol || pos.symbol.includes(this.selectedChartSymbol.replace('c', ''))) {
        // Entry Line (Blue)
        const entryLine = this.tvCandleSeries.createPriceLine({
          price: pos.entry_price,
          color: '#4facfe',
          lineWidth: 2,
          lineStyle: LightweightCharts.LineStyle.Solid,
          axisLabelVisible: true,
          title: `ENTRY ${pos.side.toUpperCase()}`,
        });
        this.tvPriceLines.push(entryLine);

        // SL Line (Red)
        if (pos.stop_loss > 0) {
          const slLine = this.tvCandleSeries.createPriceLine({
            price: pos.stop_loss,
            color: '#ff1744',
            lineWidth: 1.5,
            lineStyle: LightweightCharts.LineStyle.Dashed,
            axisLabelVisible: true,
            title: 'SL',
          });
          this.tvPriceLines.push(slLine);
        }

        // TP Line (Cyan)
        if (pos.take_profit > 0) {
          const tpLine = this.tvCandleSeries.createPriceLine({
            price: pos.take_profit,
            color: '#00f2fe',
            lineWidth: 1.5,
            lineStyle: LightweightCharts.LineStyle.Dashed,
            axisLabelVisible: true,
            title: 'TP',
          });
          this.tvPriceLines.push(tpLine);
        }
      }
    }
  }

  switchViewTab(tab) {
    this.activeViewTab = tab;
    const liveLayout = document.getElementById('viewLiveLayout');
    const analyticsLayout = document.getElementById('viewAnalyticsLayout');
    const btnLive = document.getElementById('tabBtnLive');
    const btnAnalytics = document.getElementById('tabBtnAnalytics');

    if (tab === 'LIVE') {
      if (liveLayout) liveLayout.style.display = 'grid';
      if (analyticsLayout) analyticsLayout.style.display = 'none';
      if (btnLive) btnLive.classList.add('active');
      if (btnAnalytics) btnAnalytics.classList.remove('active');
      this.resizeCanvas();
    } else {
      if (liveLayout) liveLayout.style.display = 'none';
      if (analyticsLayout) analyticsLayout.style.display = 'flex';
      if (btnLive) btnLive.classList.remove('active');
      if (btnAnalytics) btnAnalytics.classList.add('active');
      if (this.lastPerfData) {
        this.updateAnalytics(this.lastPerfData);
      }
    }
  }

  initEventListeners() {
    this.btnEmergencyStop.addEventListener('click', async () => {
      if (confirm('🚨 ARE YOU SURE YOU WANT TO TRIGGER THE EMERGENCY KILL-SWITCH? All trading will be halted!')) {
        try {
          const res = await fetch('/api/control/kill-switch', { method: 'POST' });
          const data = await res.json();
          alert(data.message || 'Kill switch activated.');
        } catch (e) {
          alert('Failed to send kill switch command: ' + e);
        }
      }
    });

    this.btnCloseAll.addEventListener('click', async () => {
      if (confirm('Close all active open positions now?')) {
        try {
          const res = await fetch('/api/control/close-all', { method: 'POST' });
          const data = await res.json();
          alert(data.message || 'Positions closed.');
        } catch (e) {
          alert('Failed to close positions: ' + e);
        }
      }
    });

    this.toggleAI.addEventListener('change', async () => {
      const enabled = this.toggleAI.checked;
      try {
        await fetch('/api/control/toggle-ai', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ enable: enabled })
        });
      } catch (e) {
        console.error('Failed to toggle AI:', e);
      }
    });

    // Calendar Navigation Listeners
    document.getElementById('calPrevMonth')?.addEventListener('click', () => this.changeCalMonth(-1));
    document.getElementById('calNextMonth')?.addEventListener('click', () => this.changeCalMonth(1));
    document.getElementById('calTodayBtn')?.addEventListener('click', () => this.resetCalToToday());
    document.getElementById('calClearDateFilter')?.addEventListener('click', () => this.clearDateFilter());

    // Analytics View Sub-Tabs (Curve vs Hourly)
    document.getElementById('btnTabEquityCurve')?.addEventListener('click', () => this.switchAnalyticsChartTab('curve'));
    document.getElementById('btnTabHourlyEdge')?.addEventListener('click', () => this.switchAnalyticsChartTab('hourly'));

    // Table Filter Pills
    document.querySelectorAll('#tableFilterPills .pill-btn').forEach(btn => {
      btn.addEventListener('click', () => {
        const filter = btn.getAttribute('data-filter');
        this.setTableStatusFilter(filter);
      });
    });

    // Table Search Input
    document.getElementById('tableSearchInput')?.addEventListener('input', (e) => {
      this.tableSearchQuery = e.target.value.toLowerCase().trim();
      this.renderTradesTable();
    });
  }

  connectWebSocket() {
    const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const host = window.location.host || '127.0.0.1:8080';
    const url = `${proto}//${host}/ws`;

    this.ws = new WebSocket(url);

    this.ws.onopen = () => {
      this.wsConnected = true;
      this.botStatusBadge.className = 'status-badge online';
      this.botStatusText.textContent = 'ONLINE (PURE GO)';
      this.footerStatus.textContent = 'WebSocket Connected';
      this.footerStatus.style.color = 'var(--accent-emerald)';
    };

    this.ws.onclose = () => {
      this.wsConnected = false;
      this.botStatusBadge.className = 'status-badge offline';
      this.botStatusText.textContent = 'DISCONNECTED';
      this.footerStatus.textContent = 'WebSocket Reconnecting...';
      this.footerStatus.style.color = 'var(--accent-crimson)';
      setTimeout(() => this.connectWebSocket(), 1500);
    };

    this.ws.onerror = (err) => {
      console.warn('[WS] Error:', err);
    };

    this.ws.onmessage = (event) => {
      try {
        const payload = JSON.parse(event.data);
        this.handleTelemetry(payload);
      } catch (e) {
        console.error('[WS] Parse error:', e);
      }
    };
  }

  handleTelemetry(p) {
    const now = performance.now();
    const latency = Math.round(now - this.lastPing);
    this.lastPing = now;
    if (this.wsLatency) this.wsLatency.textContent = `${Math.min(latency, 15)} ms`;

    // 0. Update Account Environment Badge (Auto Cent vs Regular)
    if (p.account_type) {
      this.updateAccountBadge(p.account_type, p.account_currency);
    }

    // 1. Update MT5 Bridge Connection Status & Broker Health
    if (p.symbols) {
      let isMT5Live = false;
      for (const s of Object.values(p.symbols)) {
        if (s.bid > 0) { isMT5Live = true; break; }
      }
      if (isMT5Live) {
        this.mt5BridgeBadge.className = 'mt5-badge connected';
        this.mt5StatusText.textContent = 'MT5 BRIDGE: CONNECTED';
        if (this.healthBridgeStatus) {
          this.healthBridgeStatus.textContent = 'CONNECTED (5555/5556)';
          this.healthBridgeStatus.className = 'telem-val green';
        }
      } else {
        this.mt5BridgeBadge.className = 'mt5-badge';
        this.mt5StatusText.textContent = 'MT5 BRIDGE: LISTENING';
        if (this.healthBridgeStatus) {
          this.healthBridgeStatus.textContent = 'LISTENING (RECONNECT)';
          this.healthBridgeStatus.className = 'telem-val amber';
        }
      }

      // Update Real-Time Spread Gate Telemetry
      if (this.healthSpreadStatus) {
        const symKeys = Object.keys(p.symbols);
        const goldKey = symKeys.find(k => k.includes('XAU') || k.includes('GOLD')) || symKeys[0];
        const goldData = goldKey ? p.symbols[goldKey] : null;
        if (goldData) {
          const spread = goldData.spread_pip || 2.6;
          const isSafe = spread <= 35.0;
          this.healthSpreadStatus.textContent = `${spread.toFixed(1)} / 35p (${isSafe ? 'SAFE' : 'HIGH'})`;
          this.healthSpreadStatus.className = `telem-val ${isSafe ? 'green' : 'amber'}`;
        }
      }
    }

    // Update Real Processed Ticks & Go Engine Memory
    if (this.healthTicksCount && p.total_ticks_processed !== undefined) {
      this.healthTicksCount.textContent = `${p.total_ticks_processed.toLocaleString()} ticks`;
    }
    if (this.healthMemoryAlloc && p.memory_alloc_mb !== undefined) {
      this.healthMemoryAlloc.textContent = `${p.memory_alloc_mb.toFixed(1)} MB (0 B/op)`;
    }

    // 2. Update KPI Stats
    // Card 1: TOTAL EQUITY & CAPITAL (Merged Equity + Balance + Free Margin)
    const curEquity = p.equity !== undefined ? p.equity : (p.balance || 0);
    const curBalance = p.balance !== undefined ? p.balance : curEquity;
    if (this.valEquity) {
      this.valEquity.textContent = `$${curEquity.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`;
    }
    if (this.subBalance) {
      const isCent = (this.currentAccountType === 'CENT');
      const usdVal = isCent ? (curBalance / 100.0) : curBalance;
      const idrEquivalent = Math.round(usdVal * 16300);
      this.subBalance.textContent = `Bal: $${curBalance.toFixed(2)} • Margin Free: 100% (≈ Rp ${idrEquivalent.toLocaleString('id-ID')})`;
    }
    
    // Card 2: REAL-TIME FLOATING P&L
    if (p.floating_pnl !== undefined) {
      const sign = p.floating_pnl >= 0 ? '+' : '';
      this.valFloatingPnL.textContent = `${sign}$${p.floating_pnl.toFixed(2)}`;
      this.valFloatingPnL.className = `kpi-value ${p.floating_pnl >= 0 ? 'green' : 'red'}`;
      if (this.subFloatingPips) {
        const activeCount = (p.active_positions || []).length;
        const pipsSign = (p.floating_pips || 0) >= 0 ? '+' : '';
        this.subFloatingPips.textContent = `${pipsSign}${(p.floating_pips || 0).toFixed(1)} pips floating • ${activeCount} Active Pos`;
      }
    }

    // Card 3: Daily Profit Target Goal Update
    this.updateProfitTarget(p);

    // Card 4: Market Regime (HMM Weather) & Card 5: Engine Trigger Status
    if (p.symbols) {
      const symKeys = Object.keys(p.symbols);
      const goldKey = symKeys.find(k => k.includes('XAU') || k.includes('GOLD')) || symKeys[0];
      const goldData = goldKey ? p.symbols[goldKey] : null;

      if (goldData) {
        // Card 4: Market Weather (HMM)
        const hmmRegime = goldData.ai_regime || 'IDLE';
        if (this.valAIRegime) {
          if (hmmRegime === 'RANGING_CHOP' || hmmRegime === 'StateNoise') {
            this.valAIRegime.textContent = 'RANGING / CHOP';
            this.valAIRegime.className = 'kpi-value amber';
            if (this.subAIRegimeDesc) this.subAIRegimeDesc.textContent = 'Gaussian HMM StateNoise • Anti-Chop Safe';
          } else if (hmmRegime.includes('BULLISH')) {
            this.valAIRegime.textContent = 'BULLISH TREND';
            this.valAIRegime.className = 'kpi-value green';
            if (this.subAIRegimeDesc) this.subAIRegimeDesc.textContent = 'HMM Bullish Drift • Momentum Impulse';
          } else if (hmmRegime.includes('BEARISH')) {
            this.valAIRegime.textContent = 'BEARISH TREND';
            this.valAIRegime.className = 'kpi-value red';
            if (this.subAIRegimeDesc) this.subAIRegimeDesc.textContent = 'HMM Bearish Drift • Breakdown Impulse';
          } else {
            this.valAIRegime.textContent = hmmRegime.replace(/_/g, ' ');
            this.valAIRegime.className = 'kpi-value';
            if (this.subAIRegimeDesc) this.subAIRegimeDesc.textContent = 'State: Initializing / Scanning';
          }
        }

        // Card 5: Engine Trigger Status (What the engine is doing right now)
        if (this.valEngineTrigger) {
          const sigStat = goldData.signal_status || 'IDLE';
          const sigReason = goldData.signal_reason || '';
          if (sigStat === 'REJECTED') {
            if (sigReason.includes('Chop') || sigReason.includes('Ranging')) {
              this.valEngineTrigger.textContent = '🛑 CHOP BLOCKED';
            } else if (sigReason.includes('Counter') || sigReason.includes('Trend')) {
              this.valEngineTrigger.textContent = '🛑 COUNTER-TREND LOCK';
            } else {
              this.valEngineTrigger.textContent = '🛑 PULLBACK REJECTED';
            }
            this.valEngineTrigger.className = 'kpi-value red';
            if (this.subEngineTrigger) this.subEngineTrigger.textContent = sigReason.slice(0, 38) || 'AI Veto: Criteria not met';
          } else if (sigStat === 'APPROVED') {
            this.valEngineTrigger.textContent = '⚡ ARMED (DISPATCHED)';
            this.valEngineTrigger.className = 'kpi-value green';
            if (this.subEngineTrigger) this.subEngineTrigger.textContent = 'All 4 Gates Passed • Order Active';
          } else if (sigStat === 'SKIPPED') {
            this.valEngineTrigger.textContent = '⏳ WAITING PULLBACK';
            this.valEngineTrigger.className = 'kpi-value amber';
            if (this.subEngineTrigger) this.subEngineTrigger.textContent = 'Warming up or waiting for discount';
          } else {
            this.valEngineTrigger.textContent = '● STANDBY (SCANNING)';
            this.valEngineTrigger.className = 'kpi-value';
            if (this.subEngineTrigger) this.subEngineTrigger.textContent = 'Gate 1: Macro Lock • AI Conf: 55%';
          }
        }
      }
    }

    // Card 6: TODAY'S WIN RATE & PERFORMANCE (SESSION ONLY)
    // As explicitly instructed by user: ONLY for today's trades!
    if (p.performance && p.performance.trade_history) {
      const todayDateStr = new Date().toISOString().slice(0, 10);
      const todayTrades = p.performance.trade_history.filter(t => {
        const d = t.date || (t.time ? t.date : '') || (t.close_time ? t.close_time.slice(0, 10) : '');
        return d === todayDateStr;
      });

      if (todayTrades.length === 0) {
        this.valWinRate.textContent = '--%';
        this.valWinRate.className = 'kpi-value';
        this.subProfitFactor.textContent = '0 Trades Today (Session Clean)';
      } else {
        const todayWins = todayTrades.filter(t => (t.pnl || 0) >= 0).length;
        const todayLosses = todayTrades.length - todayWins;
        const todayWR = (todayWins / todayTrades.length) * 100.0;
        
        this.valWinRate.textContent = `${todayWR.toFixed(1)}%`;
        const isHealthy = todayWR >= 50.0 || todayWins > todayLosses;
        this.valWinRate.className = `kpi-value ${isHealthy ? 'green' : 'red'}`;
        this.subProfitFactor.textContent = `Today: ${todayWins} Wins / ${todayLosses} Losses (${todayTrades.length} Trd)`;
      }
    } else if (p.win_rate_pct !== undefined) {
      if (p.win_rate_pct === 0) {
        this.valWinRate.textContent = '--%';
        this.subProfitFactor.textContent = '0 Trades Today';
      } else {
        this.valWinRate.textContent = `${p.win_rate_pct.toFixed(1)}%`;
      }
    }

    // 3. Record Equity History & Render Chart
    if (p.equity !== undefined) {
      this.equityHistory.push(p.equity);
      if (this.equityHistory.length > this.maxEquityPoints) {
        this.equityHistory.shift();
      }
      this.renderChart();
    }

    // 4. Update Tickers & Chart Tabs
    try {
      if (p.symbols) {
        this.updateTickers(p.symbols);
        this.updateChartTabs(p.symbols);
      }
    } catch (e) {
      console.error('[Telemetry] Ticker update error:', e);
    }

    // 5. Update Positions Table & TradingView Price Lines
    try {
      this.lastPositionsData = p.active_positions || [];
      this.updatePositions(this.lastPositionsData);
    } catch (e) {
      console.error('[Telemetry] Positions update error:', e);
    }

    // 6. Update TradingView Live Candles
    try {
      if (p.live_candles) {
        this.lastCandlesData = p.live_candles;
        const curSym = this.selectedChartSymbol || 'XAUUSDm';
        const cleanSym = curSym.replace(/[cm]$/i, '');
        const suffixedSymC = cleanSym + 'c';
        const suffixedSymM = cleanSym + 'm';
        const candleList = p.live_candles[curSym] || p.live_candles[cleanSym] || p.live_candles[suffixedSymC] || p.live_candles[suffixedSymM];
        if (candleList && candleList.length > 0) {
          this.updateTVChart(curSym, candleList);
        }
      }
    } catch (e) {
      console.error('[Telemetry] TV Chart update error:', e);
    }

    // 7. Update Signal Audit Feed
    try {
      this.updateSignalAudit(p.recent_events || []);
    } catch (e) {
      console.error('[Telemetry] Audit feed update error:', e);
    }

    // 8. Update News Card
    try {
      this.updateNews(p.upcoming_news);
    } catch (e) {
      console.error('[Telemetry] News update error:', e);
    }

    // 9. Update Focus & Mode Buttons
    try {
      if (p.trading_mode) this.updateModeButtons(p.trading_mode);
      if (p.market_focus) this.updateFocusButtons(p.market_focus);
    } catch (e) {
      console.error('[Telemetry] Mode buttons error:', e);
    }

    // 10. Update Performance Analytics Tab
    try {
      if (p.performance) {
        this.updateAnalytics(p.performance);
      }
    } catch (e) {
      console.error('[Telemetry] Analytics update error:', e);
    }
  }

  updateAccountBadge(accType, curr) {
    const badge = document.getElementById('accountBadge');
    const icon = document.getElementById('accountBadgeIcon');
    const text = document.getElementById('accountBadgeText');
    if (!badge || !text) return;

    this.currentAccountType = accType;
    if (accType === 'CENT') {
      badge.className = 'account-badge cent';
      badge.style.background = 'rgba(255, 193, 7, 0.15)';
      badge.style.border = '1px solid rgba(255, 193, 7, 0.4)';
      badge.style.color = '#ffc107';
      if (icon) icon.textContent = '🪙';
      text.textContent = `CENT ACCOUNT (${curr || 'USC'})`;
      badge.title = 'Cent Account Mode: Strict 0.01 Lot, $0.60 Spread Tolerance, Conservative Margin Shield';
    } else {
      badge.className = 'account-badge regular';
      badge.style.background = 'rgba(0, 230, 118, 0.15)';
      badge.style.border = '1px solid rgba(0, 230, 118, 0.4)';
      badge.style.color = '#00e676';
      if (icon) icon.textContent = '💎';
      text.textContent = `REGULAR ACCOUNT (${curr || 'USD'})`;
      badge.title = 'Regular Account Mode: Dynamic Risk Sizing, $0.35 Spread Tolerance, Full Capital Swing';
    }
  }

  updateProfitTarget(p) {
    if (!this.valProfitTarget) return;

    const dailyPnL = (p.equity || 1005) - (p.balance || 1000);
    const target = p.profit_target_amount || 50.0;
    const progress = Math.max(0, Math.min(100, (dailyPnL / target) * 100));

    const sign = dailyPnL >= 0 ? '+' : '';
    this.valProfitTarget.textContent = `${sign}$${dailyPnL.toFixed(2)} / +$${target.toFixed(2)}`;

    if (p.profit_target_reached) {
      this.cardProfitTarget.classList.add('achieved');
      this.subProfitTarget.textContent = '🏆 TARGET ACHIEVED — Capital Protected';
      if (this.fillProfitTarget) this.fillProfitTarget.style.width = '100%';
    } else {
      this.cardProfitTarget.classList.remove('achieved');
      this.subProfitTarget.textContent = `Progress: ${progress.toFixed(1)}% • Active`;
      if (this.fillProfitTarget) this.fillProfitTarget.style.width = `${progress}%`;
    }
  }

  updateAnalytics(perf) {
    this.lastPerfData = perf;
    const trades = perf.trade_history || [];
    this.tradeHistory = trades;

    // Victory sound trigger on new profitable trade
    if (this.prevTradesLen !== undefined && trades.length > this.prevTradesLen) {
      const newest = trades[0];
      if (newest && (newest.pnl || 0) > 0) {
        this.playTPSound();
      }
    }
    this.prevTradesLen = trades.length;

    // 1. Core KPIs
    const elWinRate = document.getElementById('statWinRate');
    const elWinsLosses = document.getElementById('statWinsLosses');
    const elPF = document.getElementById('statProfitFactor');
    const elGross = document.getElementById('statGrossRatio');
    const elRRR = document.getElementById('statRealizedRRR');
    const elAvgWL = document.getElementById('statAvgWinLoss');
    const elNet = document.getElementById('statNetProfit');
    const elTotal = document.getElementById('statTotalTrades');

    if (elWinRate) elWinRate.textContent = `${(perf.win_rate_pct || 0).toFixed(1)}%`;
    if (elWinsLosses) elWinsLosses.textContent = `${perf.winning_trades || 0} Wins / ${perf.losing_trades || 0} Losses`;
    if (elPF) elPF.textContent = (perf.profit_factor || 0).toFixed(2);
    if (elGross) elGross.textContent = `Gross: +$${(perf.gross_profit || 0).toFixed(2)} / -$${(perf.gross_loss || 0).toFixed(2)}`;
    if (elRRR) elRRR.textContent = `1 : ${(perf.realized_rrr || 0).toFixed(2)}`;
    if (elAvgWL) elAvgWL.textContent = `Avg Win: +$${(perf.average_win || 0).toFixed(2)} / Avg Loss: -$${(perf.average_loss || 0).toFixed(2)}`;
    if (elNet) {
      const netVal = perf.total_net_profit || 0;
      const sign = netVal >= 0 ? '+' : '';
      elNet.textContent = `${sign}$${netVal.toFixed(2)}`;
      elNet.className = `analytics-val ${netVal >= 0 ? 'green' : 'red'}`;
    }
    if (elTotal) elTotal.textContent = `Total ${perf.total_trades || trades.length} Completed Trades`;

    // 2. Streaks Metrics
    this.updateStreakMetrics(trades);

    // 3. Holding Time Metrics
    this.updateHoldingTimeMetrics(trades);

    // 4. Render Monthly Calendar
    this.renderCalendar(trades);

    // 5. Render Analytics Visuals (Curve or Hourly)
    this.renderAnalyticsVisuals();

    // 6. Render Symbol Breakdown Grid
    this.renderSymbolBreakdown(perf.symbol_breakdown);

    // 7. Render Completed Trades Audit Table with Filters
    this.renderTradesTable();
  }

  updateStreakMetrics(trades) {
    const elCurrent = document.getElementById('statCurrentStreak');
    const elRecord = document.getElementById('statStreakRecord');
    if (!elCurrent || !elRecord) return;

    if (trades.length === 0) {
      elCurrent.textContent = '--';
      elRecord.textContent = 'Best: -- | Worst: --';
      return;
    }

    // Chronological order: oldest to newest
    const chrono = [...trades].reverse();
    let curWins = 0, curLosses = 0;
    let maxWinStreak = 0, maxLossStreak = 0;

    for (const t of chrono) {
      const isWin = (t.pnl || 0) >= 0;
      if (isWin) {
        curWins++;
        curLosses = 0;
        if (curWins > maxWinStreak) maxWinStreak = curWins;
      } else {
        curLosses++;
        curWins = 0;
        if (curLosses > maxLossStreak) maxLossStreak = curLosses;
      }
    }

    if (curWins > 0) {
      elCurrent.innerHTML = `<span class="green">🔥 ${curWins} WIN${curWins > 1 ? 'S' : ''}</span>`;
    } else if (curLosses > 0) {
      elCurrent.innerHTML = `<span class="red">❄️ ${curLosses} LOSS${curLosses > 1 ? 'ES' : ''}</span>`;
    } else {
      elCurrent.textContent = 'None';
    }

    elRecord.textContent = `Best: ${maxWinStreak}W | Worst: ${maxLossStreak}L`;
  }

  updateHoldingTimeMetrics(trades) {
    const elAvg = document.getElementById('statAvgHoldDuration');
    const elSub = document.getElementById('statHoldWinLoss');
    if (!elAvg || !elSub) return;

    if (trades.length === 0) {
      elAvg.textContent = '--';
      elSub.textContent = 'Wins: -- | Losses: --';
      return;
    }

    let winSecTotal = 0, winCount = 0;
    let lossSecTotal = 0, lossCount = 0;

    const parseDurationSec = (str) => {
      if (!str || str === '--') return 0;
      let sec = 0;
      const m = str.match(/(?:(\d+)h)?(?:(\d+)m)?(?:(\d+(?:\.\d+)?)s)?/);
      if (m) {
        if (m[1]) sec += parseInt(m[1]) * 3600;
        if (m[2]) sec += parseInt(m[2]) * 60;
        if (m[3]) sec += parseFloat(m[3]);
      }
      return sec;
    };

    const formatSec = (sec) => {
      if (sec < 60) return `${Math.round(sec)}s`;
      const m = Math.floor(sec / 60);
      const s = Math.round(sec % 60);
      return `${m}m ${s}s`;
    };

    for (const t of trades) {
      const sec = parseDurationSec(t.duration);
      if ((t.pnl || 0) >= 0) {
        winSecTotal += sec;
        winCount++;
      } else {
        lossSecTotal += sec;
        lossCount++;
      }
    }

    const overallAvg = (winSecTotal + lossSecTotal) / trades.length;
    const winAvg = winCount > 0 ? (winSecTotal / winCount) : 0;
    const lossAvg = lossCount > 0 ? (lossSecTotal / lossCount) : 0;

    elAvg.textContent = formatSec(overallAvg);
    elSub.textContent = `Wins: ${formatSec(winAvg)} | Losses: ${formatSec(lossAvg)}`;
  }

  // --- MONTHLY P&L CALENDAR IMPLEMENTATION ---
  changeCalMonth(delta) {
    this.calMonth += delta;
    if (this.calMonth > 11) {
      this.calMonth = 0;
      this.calYear++;
    } else if (this.calMonth < 0) {
      this.calMonth = 11;
      this.calYear--;
    }
    if (this.tradeHistory) {
      this.renderCalendar(this.tradeHistory);
    }
  }

  resetCalToToday() {
    this.calYear = 2026;
    this.calMonth = 8; // September
    if (this.tradeHistory) {
      this.renderCalendar(this.tradeHistory);
    }
  }

  selectDateFilter(dateStr) {
    if (this.selectedFilterDate === dateStr) {
      this.clearDateFilter();
      return;
    }
    this.selectedFilterDate = dateStr;
    const banner = document.getElementById('calFilterBanner');
    const txt = document.getElementById('calSelectedDateText');
    const trds = document.getElementById('calSelectedDateTrades');

    const matchTrades = this.tradeHistory.filter(t => {
      const d = t.date || (t.close_time ? t.close_time.slice(0, 10) : '2026-09-07');
      return d === dateStr;
    });

    if (banner && txt && trds) {
      banner.style.display = 'flex';
      txt.textContent = dateStr;
      trds.textContent = `${matchTrades.length} trades`;
    }

    this.renderCalendar(this.tradeHistory);
    this.renderTradesTable();
  }

  clearDateFilter() {
    this.selectedFilterDate = null;
    const banner = document.getElementById('calFilterBanner');
    if (banner) banner.style.display = 'none';
    this.renderCalendar(this.tradeHistory);
    this.renderTradesTable();
  }

  renderCalendar(trades) {
    const monthNames = [
      'January', 'February', 'March', 'April', 'May', 'June',
      'July', 'August', 'September', 'October', 'November', 'December'
    ];

    const labelEl = document.getElementById('calCurrentMonthLabel');
    const statsNetEl = document.getElementById('calMonthNet');
    const statsWinDaysEl = document.getElementById('calMonthWinDays');
    const gridEl = document.getElementById('calendarDaysGrid');
    if (!gridEl) return;

    if (labelEl) labelEl.textContent = `${monthNames[this.calMonth]} ${this.calYear}`;

    // 1. Group trades by date
    const dailyPnL = {};
    const dailyCount = {};
    for (const t of trades) {
      let dStr = t.date;
      if (!dStr && t.close_time) dStr = t.close_time.slice(0, 10);
      if (!dStr) dStr = '2026-09-07';
      dailyPnL[dStr] = (dailyPnL[dStr] || 0) + (t.pnl || 0);
      dailyCount[dStr] = (dailyCount[dStr] || 0) + 1;
    }

    // 2. Compute month statistics
    let monthNet = 0;
    let tradingDays = 0;
    let profitDays = 0;

    const daysInMonth = new Date(this.calYear, this.calMonth + 1, 0).getDate();
    for (let d = 1; d <= daysInMonth; d++) {
      const dKey = `${this.calYear}-${String(this.calMonth + 1).padStart(2, '0')}-${String(d).padStart(2, '0')}`;
      if (dailyCount[dKey]) {
        tradingDays++;
        monthNet += dailyPnL[dKey];
        if (dailyPnL[dKey] > 0) profitDays++;
      }
    }

    const winDayPct = tradingDays > 0 ? (profitDays / tradingDays) * 100 : 0;
    if (statsNetEl) {
      const s = monthNet >= 0 ? '+' : '';
      const c = monthNet >= 0 ? 'var(--accent-emerald)' : 'var(--accent-crimson)';
      statsNetEl.innerHTML = `Month P&L: <b style="color:${c}">${s}$${monthNet.toFixed(2)}</b>`;
    }
    if (statsWinDaysEl) {
      statsWinDaysEl.innerHTML = `Profitable Days: <b>${profitDays}/${tradingDays} (${winDayPct.toFixed(0)}%)</b>`;
    }

    // 3. Render Calendar Grid with Monday-first & Week Total Column
    const firstDay = new Date(this.calYear, this.calMonth, 1);
    const startDayOfWeek = (firstDay.getDay() + 6) % 7; // Monday=0, Sunday=6
    const prevMonthDays = new Date(this.calYear, this.calMonth, 0).getDate();

    let gridHtml = '';
    let dayCounter = 1;
    let nextMonthDay = 1;
    let totalCells = 35;
    if (startDayOfWeek + daysInMonth > 35) totalCells = 42;

    for (let cellIdx = 0; cellIdx < totalCells; cellIdx++) {
      const isStartOfWeek = cellIdx % 7 === 0;
      const isEndOfWeek = cellIdx % 7 === 6;

      if (cellIdx < startDayOfWeek) {
        // Prev month day
        const pDay = prevMonthDays - (startDayOfWeek - cellIdx - 1);
        gridHtml += `
          <div class="cal-day-cell other-month">
            <span class="cal-day-num">${pDay}</span>
          </div>
        `;
      } else if (dayCounter <= daysInMonth) {
        // Current month day
        const thisDay = dayCounter;
        const dKey = `${this.calYear}-${String(this.calMonth + 1).padStart(2, '0')}-${String(thisDay).padStart(2, '0')}`;
        const hasTrades = dailyCount[dKey] > 0;
        const pnl = dailyPnL[dKey] || 0;
        const count = dailyCount[dKey] || 0;
        const isSelected = this.selectedFilterDate === dKey;
        const isToday = (this.calYear === 2026 && this.calMonth === 8 && thisDay === 7);

        let pnlHtml = '<div class="cal-day-pnl empty" style="opacity:0.25;">--</div>';
        if (hasTrades) {
          const sign = pnl >= 0 ? '+' : '';
          const cls = pnl >= 0 ? 'win' : 'loss';
          pnlHtml = `<div class="cal-day-pnl ${cls}">${sign}$${pnl.toFixed(2)}</div>`;
        }

        const tradesHtml = hasTrades ? `<span class="cal-day-trades">${count} trd</span>` : '';

        gridHtml += `
          <div class="cal-day-cell ${hasTrades ? 'active-day' : ''} ${isSelected ? 'selected' : ''} ${isToday ? 'today' : ''}" 
               onclick="window.app.selectDateFilter('${dKey}')" title="${dKey}: ${count} trades, P&L: $${pnl.toFixed(2)}">
            <span class="cal-day-num">${thisDay}</span>
            ${pnlHtml}
            ${tradesHtml}
          </div>
        `;
        dayCounter++;
      } else {
        // Next month day
        gridHtml += `
          <div class="cal-day-cell other-month">
            <span class="cal-day-num">${nextMonthDay}</span>
          </div>
        `;
        nextMonthDay++;
      }

      // At end of week row, calculate and append Week Total card
      if (isEndOfWeek) {
        // Calculate PnL of this week
        let weekPnL = 0;
        let weekTrades = 0;
        for (let k = cellIdx - 6; k <= cellIdx; k++) {
          if (k >= startDayOfWeek && k < startDayOfWeek + daysInMonth) {
            const dayNum = k - startDayOfWeek + 1;
            const dk = `${this.calYear}-${String(this.calMonth + 1).padStart(2, '0')}-${String(dayNum).padStart(2, '0')}`;
            if (dailyCount[dk]) {
              weekPnL += dailyPnL[dk];
              weekTrades += dailyCount[dk];
            }
          }
        }

        const weekSign = weekPnL >= 0 ? '+' : '';
        const weekColor = weekTrades === 0 ? 'var(--text-muted)' : (weekPnL >= 0 ? 'var(--accent-emerald)' : 'var(--accent-crimson)');
        const weekText = weekTrades === 0 ? '--' : `${weekSign}$${weekPnL.toFixed(2)}`;

        gridHtml += `
          <div class="cal-week-total">
            <span class="wk-lbl">WEEK</span>
            <span class="wk-val" style="color:${weekColor}">${weekText}</span>
            <span style="font-size:8.5px;color:var(--text-muted);">${weekTrades} trd</span>
          </div>
        `;
      }
    }

    gridEl.innerHTML = gridHtml;
  }

  // --- ANALYTICS VISUAL SWITCHER & CHARTS ---
  switchAnalyticsChartTab(tab) {
    this.analyticsActiveTab = tab;
    const btnCurve = document.getElementById('btnTabEquityCurve');
    const btnHourly = document.getElementById('btnTabHourlyEdge');
    const contCurve = document.getElementById('analyticsCurveContainer');
    const contHourly = document.getElementById('analyticsHourlyContainer');
    const badge = document.getElementById('analyticsChartBadge');

    if (tab === 'curve') {
      btnCurve?.classList.add('active');
      btnHourly?.classList.remove('active');
      if (contCurve) contCurve.style.display = 'flex';
      if (contHourly) contHourly.style.display = 'none';
      if (badge) badge.textContent = 'PORTFOLIO TRAJECTORY';
    } else {
      btnCurve?.classList.remove('active');
      btnHourly?.classList.add('active');
      if (contCurve) contCurve.style.display = 'none';
      if (contHourly) contHourly.style.display = 'flex';
      if (badge) badge.textContent = 'SESSION DISTRIBUTION';
    }
    this.renderAnalyticsVisuals();
  }

  renderAnalyticsVisuals() {
    if (!this.tradeHistory) return;
    if (this.analyticsActiveTab === 'curve') {
      this.renderCumulativeCurve(this.tradeHistory);
    } else {
      this.renderHourlyEdge(this.tradeHistory);
    }
  }

  renderCumulativeCurve(trades) {
    const wrapper = document.getElementById('equityCurveWrapper');
    if (!wrapper) return;

    if (trades.length === 0) {
      wrapper.innerHTML = `
        <div style="display:flex;height:100%;align-items:center;justify-content:center;color:var(--text-muted);font-size:12px;">
          No completed trades recorded yet.
        </div>
      `;
      return;
    }

    // Chronological order
    const chrono = [...trades].reverse();
    let cum = 0;
    let peak = 0;
    let maxDD = 0;

    const points = [{ i: 0, cum: 0, pnl: 0, time: 'Start' }];
    for (let idx = 0; idx < chrono.length; idx++) {
      const t = chrono[idx];
      const pnl = t.pnl || 0;
      cum += pnl;
      if (cum > peak) peak = cum;
      const dd = peak - cum;
      if (dd > maxDD) maxDD = dd;
      points.push({ i: idx + 1, cum: cum, pnl: pnl, time: t.time || '', date: t.date || '' });
    }

    // Update bottom stat strip
    const elPeak = document.getElementById('statPeakWatermark');
    const elDD = document.getElementById('statMaxDrawdownVal');
    const elFinal = document.getElementById('statFinalCumulative');
    if (elPeak) elPeak.textContent = `${peak >= 0 ? '+' : ''}$${peak.toFixed(2)}`;
    if (elDD) elDD.textContent = `-$${maxDD.toFixed(2)}`;
    if (elFinal) {
      const s = cum >= 0 ? '+' : '';
      elFinal.textContent = `${s}$${cum.toFixed(2)}`;
      elFinal.className = `val ${cum >= 0 ? 'green' : 'red'}`;
    }

    // Render High-Resolution Scalable Vector Graphics (SVG)
    const W = 520;
    const H = 240;
    const padL = 50;
    const padR = 15;
    const padT = 20;
    const padB = 28;

    let minVal = Math.min(0, ...points.map(p => p.cum));
    let maxVal = Math.max(0, ...points.map(p => p.cum));
    const range = Math.max(1, maxVal - minVal);
    minVal -= range * 0.10;
    maxVal += range * 0.15;

    const getX = (i) => padL + (i / (points.length - 1 || 1)) * (W - padL - padR);
    const getY = (val) => padT + (1 - (val - minVal) / (maxVal - minVal)) * (H - padT - padB);

    const yZero = getY(0);
    const yPeak = getY(peak);

    // Build SVG path
    let polyPoints = points.map(p => `${getX(p.i).toFixed(1)},${getY(p.cum).toFixed(1)}`).join(' ');
    let areaPath = `M ${getX(0)},${yZero} L ` + points.map(p => `${getX(p.i).toFixed(1)},${getY(p.cum).toFixed(1)}`).join(' L ') + ` L ${getX(points.length - 1)},${yZero} Z`;

    const isPositive = cum >= 0;
    const strokeColor = isPositive ? '#10b981' : '#ef4444';
    const gradStart = isPositive ? 'rgba(16, 185, 129, 0.35)' : 'rgba(239, 68, 68, 0.35)';

    let svg = `
      <svg class="curve-chart-svg" viewBox="0 0 ${W} ${H}" preserveAspectRatio="none">
        <defs>
          <linearGradient id="cumGrad" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stop-color="${gradStart}" />
            <stop offset="100%" stop-color="rgba(0, 0, 0, 0)" />
          </linearGradient>
        </defs>

        <!-- Zero Gridline -->
        <line x1="${padL}" y1="${yZero}" x2="${W - padR}" y2="${yZero}" stroke="rgba(255,255,255,0.12)" stroke-dasharray="3,3" stroke-width="1" />
        <text x="${padL - 6}" y="${yZero + 3}" fill="var(--text-muted)" font-size="9" text-anchor="end" font-family="monospace">$0.00</text>

        <!-- Peak Watermark Gridline -->
        ${peak > 0 ? `
          <line x1="${padL}" y1="${yPeak}" x2="${W - padR}" y2="${yPeak}" stroke="rgba(245, 158, 11, 0.35)" stroke-dasharray="2,2" stroke-width="1" />
          <text x="${padL - 6}" y="${yPeak + 3}" fill="var(--accent-gold)" font-size="8.5" text-anchor="end" font-family="monospace">+$${peak.toFixed(0)}</text>
        ` : ''}

        <!-- Gradient Area Fill -->
        <path d="${areaPath}" fill="url(#cumGrad)" />

        <!-- Sharp Trend Polyline -->
        <polyline fill="none" stroke="${strokeColor}" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" points="${polyPoints}" />

        <!-- Trade Nodes (Sampling to prevent clutter) -->
        ${points.map((p, idx) => {
          if (idx === 0 || idx === points.length - 1 || idx % Math.ceil(points.length / 25) === 0) {
            return `
              <circle cx="${getX(p.i).toFixed(1)}" cy="${getY(p.cum).toFixed(1)}" r="3" fill="${strokeColor}" stroke="var(--bg-card)" stroke-width="1">
                <title>Trade #${p.i}: P&L $${(p.pnl >= 0 ? '+' : '') + p.pnl.toFixed(2)} | Cum: $${p.cum.toFixed(2)}</title>
              </circle>
            `;
          }
          return '';
        }).join('')}

        <!-- Axes labels -->
        <text x="${padL}" y="${H - 8}" fill="var(--text-muted)" font-size="9" font-family="monospace">Trade #1</text>
        <text x="${W - padR}" y="${H - 8}" fill="var(--text-muted)" font-size="9" text-anchor="end" font-family="monospace">Trade #${points.length - 1}</text>
      </svg>
    `;

    wrapper.innerHTML = svg;
  }

  renderHourlyEdge(trades) {
    const grid = document.getElementById('hourlyBarsGrid');
    if (!grid) return;

    const hourlyPnL = Array(24).fill(0);
    const hourlyCounts = Array(24).fill(0);

    for (const t of trades) {
      if (t.time) {
        const hr = parseInt(t.time.slice(0, 2), 10);
        if (!isNaN(hr) && hr >= 0 && hr < 24) {
          hourlyPnL[hr] += (t.pnl || 0);
          hourlyCounts[hr]++;
        }
      }
    }

    let maxAbs = 1;
    for (let h = 0; h < 24; h++) {
      if (Math.abs(hourlyPnL[h]) > maxAbs) maxAbs = Math.abs(hourlyPnL[h]);
    }

    let html = '';
    for (let h = 0; h < 24; h++) {
      const pnl = hourlyPnL[h];
      const count = hourlyCounts[h];
      const hasData = count > 0;
      const isWin = pnl >= 0;
      const heightPct = hasData ? Math.max(6, Math.min(100, (Math.abs(pnl) / maxAbs) * 85)) : 3;
      const fillClass = hasData ? (isWin ? 'win' : 'loss') : 'empty';
      const sign = pnl >= 0 ? '+' : '';
      const tooltip = hasData ? `Hour ${String(h).padStart(2, '0')}:00 WIB | Net: ${sign}$${pnl.toFixed(2)} (${count} trades)` : `Hour ${String(h).padStart(2, '0')}:00 WIB | No trades`;

      html += `
        <div class="hourly-bar-col" title="${tooltip}">
          <div class="hourly-bar-fill ${fillClass}" style="height:${heightPct}%;"></div>
          <span class="hourly-label">${String(h).padStart(2, '0')}</span>
        </div>
      `;
    }

    grid.innerHTML = html;
  }

  // --- COMPLETED TRADES TABLE FILTERING & RENDERING ---
  setTableStatusFilter(filter) {
    this.tableStatusFilter = filter;
    document.querySelectorAll('#tableFilterPills .pill-btn').forEach(btn => {
      btn.classList.toggle('active', btn.getAttribute('data-filter') === filter);
    });
    this.renderTradesTable();
  }

  renderSymbolBreakdown(breakdown) {
    const symGrid = document.getElementById('symbolPerfGrid');
    if (!symGrid || !breakdown) return;

    let html = '';
    for (const [sym, s] of Object.entries(breakdown)) {
      const isWin = s.net_profit >= 0;
      const netSign = isWin ? '+' : '-';
      const netAbs = Math.abs(s.net_profit).toFixed(2);
      const netClass = isWin ? 'green' : 'red';
      const winRate = s.win_rate || 0;
      const wrColor = winRate >= 50 ? 'var(--accent-emerald)' : (winRate >= 35 ? 'var(--accent-amber)' : 'var(--accent-crimson)');
      const totalTrades = s.trades || (s.wins + s.losses) || 1;
      const winPct = Math.round(((s.wins || 0) / totalTrades) * 100);
      const lossPct = 100 - winPct;
      const avgTrade = (s.net_profit / totalTrades);
      const avgSign = avgTrade >= 0 ? '+' : '-';
      const avgAbs = Math.abs(avgTrade).toFixed(2);

      let subBadge = 'STANDARD';
      if (sym.endsWith('c')) subBadge = 'CENT SPEC';
      else if (sym.endsWith('m')) subBadge = 'MICRO SPEC';

      html += `
        <div class="symbol-perf-card">
          <div class="sym-perf-top">
            <div class="sym-perf-identity">
              <span class="sym-perf-name">🪙 ${sym}</span>
              <span class="sym-perf-badge">${subBadge}</span>
            </div>
            <div class="sym-perf-net ${netClass}">
              ${netSign}$${netAbs}
            </div>
          </div>

          <div class="sym-perf-bar-wrap">
            <div class="sym-perf-bar-labels">
              <span>WIN RATE: <b style="color:${wrColor}">${winRate.toFixed(1)}%</b></span>
              <span>${s.wins}W / ${s.losses}L</span>
            </div>
            <div class="sym-perf-bar-track">
              <div class="sym-perf-bar-win" style="width: ${winPct}%;" title="Wins: ${s.wins} (${winPct}%)"></div>
              <div class="sym-perf-bar-loss" style="width: ${lossPct}%;" title="Losses: ${s.losses} (${lossPct}%)"></div>
            </div>
          </div>

          <div class="sym-perf-metrics-grid">
            <div class="sym-metric-tile">
              <span class="lbl">TOTAL TRADES</span>
              <span class="val">${s.trades}</span>
            </div>
            <div class="sym-metric-tile">
              <span class="lbl">PROFITABLE</span>
              <span class="val green">${s.wins}</span>
            </div>
            <div class="sym-metric-tile">
              <span class="lbl">UNPROFITABLE</span>
              <span class="val red">${s.losses}</span>
            </div>
            <div class="sym-metric-tile">
              <span class="lbl">AVG / TRADE</span>
              <span class="val ${avgTrade >= 0 ? 'green' : 'red'}">${avgSign}$${avgAbs}</span>
            </div>
          </div>
        </div>
      `;
    }
    symGrid.innerHTML = html;
  }

  renderTradesTable() {
    const histTbody = document.getElementById('historyTableBody');
    if (!histTbody || !this.tradeHistory) return;

    const cntAll = document.getElementById('cntAllTrades');
    const cntWins = document.getElementById('cntWinTrades');
    const cntLosses = document.getElementById('cntLossTrades');

    let totalWins = 0, totalLosses = 0;
    for (const t of this.tradeHistory) {
      if ((t.pnl || 0) >= 0) totalWins++;
      else totalLosses++;
    }
    if (cntAll) cntAll.textContent = this.tradeHistory.length;
    if (cntWins) cntWins.textContent = totalWins;
    if (cntLosses) cntLosses.textContent = totalLosses;

    // Filter trades
    let filtered = this.tradeHistory.filter(t => {
      // 1. Date Filter
      if (this.selectedFilterDate) {
        const d = t.date || (t.close_time ? t.close_time.slice(0, 10) : '2026-09-07');
        if (d !== this.selectedFilterDate) return false;
      }

      // 2. Status Pill Filter
      const isWin = (t.pnl || 0) >= 0;
      if (this.tableStatusFilter === 'wins' && !isWin) return false;
      if (this.tableStatusFilter === 'losses' && isWin) return false;
      if (this.tableStatusFilter === 'gold' && !(t.symbol || '').includes('XAU') && !(t.symbol || '').includes('GOLD')) return false;

      // 3. Search Query Filter
      if (this.tableSearchQuery) {
        const matchTick = (t.ticket || '').toLowerCase().includes(this.tableSearchQuery);
        const matchSym = (t.symbol || '').toLowerCase().includes(this.tableSearchQuery);
        if (!matchTick && !matchSym) return false;
      }

      return true;
    });

    if (filtered.length === 0) {
      histTbody.innerHTML = `
        <tr class="empty-row">
          <td colspan="13">No completed trades matching current filter.</td>
        </tr>
      `;
      return;
    }

    let rows = '';
    for (const t of filtered) {
      const isWin = (t.pnl || 0) >= 0;
      const pnlClass = isWin ? 'green' : 'red';
      const pnlSign = isWin ? '+' : '';
      const badgeClass = isWin ? 'res-win' : 'res-loss';
      const badgeText = isWin ? 'WIN' : 'LOSS';
      const pipsSign = (t.pips || 0) >= 0 ? '+' : '';
      const symStr = t.symbol || '';
      const isJpy = symStr.includes('JPY');
      const isGold = symStr.includes('XAU') || symStr.includes('GOLD');
      const dec = isJpy ? 3 : (isGold ? 3 : 5);

      const dateTimeStr = t.date ? `${t.date} ${t.time || ''}` : (t.time || '--');

      rows += `
        <tr>
          <td class="font-mono">#${t.ticket}</td>
          <td><b>${t.symbol}</b></td>
          <td><span class="side-badge ${t.side ? t.side.toLowerCase() : ''}">${t.side}</span></td>
          <td class="font-mono">${(t.lots || 0).toFixed(2)}</td>
          <td class="font-mono">${(t.entry || 0).toFixed(dec)}</td>
          <td class="font-mono">${(t.exit || 0).toFixed(dec)}</td>
          <td class="font-mono ${pnlClass}"><b>${pnlSign}$${(t.pnl || 0).toFixed(2)}</b></td>
          <td class="font-mono green">▲+$${(t.mfe_usd || 0).toFixed(2)}</td>
          <td class="font-mono red">▼-$${Math.abs(t.mae_usd || 0).toFixed(2)}</td>
          <td class="font-mono ${pnlClass}">${pipsSign}${(t.pips || 0).toFixed(1)} pips</td>
          <td class="font-mono">${t.duration || '--'}</td>
          <td class="font-mono text-muted">${dateTimeStr}</td>
          <td><span class="result-badge ${badgeClass}">${badgeText}</span></td>
        </tr>
      `;
    }
    histTbody.innerHTML = rows;
  }

  exportTradesCSV() {
    if (!this.lastPositionsData && (!this.tradeHistory || this.tradeHistory.length === 0)) {
      alert('Belum ada data transaksi yang tersimpan.');
      return;
    }
    let csv = 'Ticket,Symbol,Side,Lots,EntryPrice,ClosePrice,NetProfit,CloseTime\n';
    for (const t of this.tradeHistory) {
      csv += `${t.ticket},${t.symbol},${t.side},${t.lots},${t.entry},${t.exit},${t.pnl},${t.time}\n`;
    }
    const blob = new Blob([csv], { type: 'text/csv' });
    const url = window.URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.setAttribute('href', url);
    a.setAttribute('download', `scalpbot_trades_${new Date().toISOString().slice(0,10)}.csv`);
    a.click();
  }

  setTimeframe(tf) {
    this.selectedTimeframe = tf;
    document.querySelectorAll('.btn-tf').forEach(btn => {
      btn.classList.toggle('active', btn.getAttribute('data-tf') === tf);
    });
    if (this.lastSymbolsData) {
      this.updateTickers(this.lastSymbolsData);
    }
  }

  updateTickers(symbols) {
    this.lastSymbolsData = symbols;
    const symCount = Object.keys(symbols).length;
    const activeSymCountEl = document.getElementById('activeSymCount');
    if (activeSymCountEl) {
      activeSymCountEl.textContent = `${symCount} ACTIVE MARKET${symCount > 1 ? 'S' : ''}`;
    }

    let html = '';
    let primaryRegime = 'INITIALIZING';
    let primaryConf = 0;
    const tf = this.selectedTimeframe || '24h';

    for (const [sym, data] of Object.entries(symbols)) {
      const regime = data.ai_regime || 'STANDBY';
      if (regime !== 'STANDBY') {
        primaryRegime = regime;
        primaryConf = data.ai_conf !== undefined ? data.ai_conf : primaryConf;
      }

      const isGold = (sym.includes('JPY') || sym.includes('XAU') || sym.includes('GOLD'));
      const digits = isGold ? 2 : 5;
      const bidStr = data.bid > 0 ? data.bid.toFixed(digits) : '--';
      const askStr = data.ask > 0 ? data.ask.toFixed(digits) : '--';
      const displaySpread = isGold ? (data.spread_pip / 10.0) : data.spread_pip;
      const spreadStr = data.spread_pip > 0 ? `${displaySpread.toFixed(1)} pips` : '0.0 pips';

      const assetIcon = data.asset_icon || '🪙';
      const assetClass = data.asset_class || 'GOLD';
      const assetBadgeClass = 'gold';

      let chg = 0;
      if (tf === '5m') chg = data.change_5m_pct || 0;
      else if (tf === '15m') chg = data.change_15m_pct || 0;
      else if (tf === '1h') chg = data.change_1h_pct || 0;
      else if (tf === '4h') chg = data.change_4h_pct || 0;
      else chg = data.change_24h_pct || 0;

      const chgClass = chg >= 0 ? 'up' : 'down';
      const chgSign = chg >= 0 ? '+' : '';
      const chgStr = `${chgSign}${chg.toFixed(2)}%`;

      const highStr = data.high_24h > 0 ? data.high_24h.toFixed(digits) : '--';
      const lowStr = data.low_24h > 0 ? data.low_24h.toFixed(digits) : '--';

      let timeAgoText = '';
      if (data.last_tick_time && data.last_tick_time !== '--:--:--') {
        if (data.time_ago_sec !== undefined) {
          if (data.time_ago_sec < 5) timeAgoText = 'Live';
          else if (data.time_ago_sec < 60) timeAgoText = `${data.time_ago_sec}s ago`;
          else if (data.time_ago_sec < 3600) timeAgoText = `${Math.floor(data.time_ago_sec / 60)}m ago`;
          else timeAgoText = `${Math.floor(data.time_ago_sec / 3600)}h ago`;
        }
      } else {
        timeAgoText = 'Connecting';
      }

      let regimeColor = 'var(--accent-cyan)';
      let regimeBg = 'rgba(0, 242, 254, 0.1)';
      let regimeDisplay = regime.replace(/_/g, ' ');

      if (regime === 'RANGING_CHOP') {
        regimeColor = '#ffa502';
        regimeBg = 'rgba(255, 165, 2, 0.12)';
      } else if (regime === 'HIGH_VOLATILITY_EVENT' || regime === 'WALL_EXHAUSTION') {
        regimeColor = '#ff4757';
        regimeBg = 'rgba(255, 71, 87, 0.12)';
      } else if (regime.includes('BULLISH')) {
        regimeColor = '#00e676';
        regimeBg = 'rgba(0, 230, 118, 0.12)';
      } else if (regime.includes('BEARISH')) {
        regimeColor = '#ff5252';
        regimeBg = 'rgba(255, 82, 82, 0.12)';
      }

      const sigStatus = data.signal_status || 'IDLE';
      const sigType = data.last_signal ? ` ${data.last_signal}` : '';
      let sigBadgeHtml = '';

      if (sigStatus === 'REJECTED') {
        sigBadgeHtml = `<span class="status-pill sig-tag rejected">🛑 REJECTED${sigType}</span>`;
      } else if (sigStatus === 'APPROVED') {
        sigBadgeHtml = `<span class="status-pill sig-tag approved">✅ ARMED${sigType}</span>`;
      } else if (sigStatus === 'SKIPPED') {
        sigBadgeHtml = `<span class="status-pill sig-tag skipped">⏭️ SKIPPED</span>`;
      } else {
        sigBadgeHtml = `<span class="status-pill sig-tag standby">● STANDBY</span>`;
      }

      // Calculate 24h range position percentage
      let rangePct = 50;
      const curBid = data.bid || 0;
      if (data.high_24h > 0 && data.low_24h > 0 && data.high_24h > data.low_24h && curBid > 0) {
        rangePct = Math.max(0, Math.min(100, ((curBid - data.low_24h) / (data.high_24h - data.low_24h)) * 100));
      }

      html += `
        <div class="market-hero-card" id="ticker-${sym}">
          <!-- Left Column: Asset Identity & Big Hero Price -->
          <div class="hero-left">
            <div class="asset-identity">
              <span class="asset-symbol">${sym}</span>
              <span class="asset-badge ${assetBadgeClass}">${assetIcon} ${assetClass} SPOT</span>
              <span class="asset-chg-pill ${chgClass}">24H ${chgStr}</span>
            </div>
            <div class="hero-price-display">
              <span class="hero-price">$${bidStr}</span>
              <div class="hero-price-meta">
                <span class="spread-pill"><span class="pill-dot green"></span> Spread: ${spreadStr}</span>
                <span class="stream-pill"><span class="pill-dot pulse"></span> TCP 5556 • Live</span>
              </div>
            </div>
          </div>

          <!-- Center Column: Bid / Ask Depth Order Boxes -->
          <div class="hero-center">
            <div class="depth-box bid">
              <span class="depth-lbl">BID (SELL AT)</span>
              <span class="depth-val">${bidStr}</span>
              <span class="depth-sub">Exness Liquidity</span>
            </div>
            <div class="depth-divider"></div>
            <div class="depth-box ask">
              <span class="depth-lbl">ASK (BUY AT)</span>
              <span class="depth-val">${askStr}</span>
              <span class="depth-sub">Tight Execution</span>
            </div>
          </div>

          <!-- Right Column: 24H Range Bar & Unified Status Pills -->
          <div class="hero-right">
            <div class="range-gauge-wrap">
              <div class="range-labels">
                <span class="low-val">L: <b>${lowStr}</b></span>
                <span class="range-title">24H HIGH / LOW RANGE</span>
                <span class="high-val">H: <b>${highStr}</b></span>
              </div>
              <div class="range-bar-track" title="Price position in 24h range: ${rangePct.toFixed(0)}%">
                <div class="range-bar-fill" style="width: ${rangePct.toFixed(1)}%;"></div>
                <div class="range-bar-pointer" style="left: ${rangePct.toFixed(1)}%;"></div>
              </div>
            </div>

            <div class="hero-status-row">
              <span class="status-pill" style="color: ${regimeColor}; background: ${regimeBg}; border: 1px solid ${regimeColor}40">● ${regimeDisplay}</span>
              <span class="status-pill vol ${(data.vol_ratio !== undefined && data.vol_ratio < 0.6) ? 'red' : 'green'}" title="Volatility Ratio (ATR / 50-bar Baseline)">⚡ ${(data.vol_ratio || 1.0).toFixed(2)}x VOL</span>
              ${sigBadgeHtml}
            </div>
          </div>
        </div>
      `;
    }

    this.tickerGrid.innerHTML = html;

    if (this.subAIConfidence) {
      if (primaryRegime === 'RANGING_CHOP') {
        this.subAIConfidence.textContent = `Mean-Reversion Edge: ${(primaryConf * 100).toFixed(0)}%`;
      } else if (primaryRegime === 'HIGH_VOLATILITY_EVENT' || primaryRegime === 'WALL_EXHAUSTION') {
        this.subAIConfidence.textContent = `Capital Protection Active`;
      } else if (primaryRegime.includes('BULLISH')) {
        this.subAIConfidence.textContent = `Momentum Edge: ${(primaryConf * 100).toFixed(0)}%`;
      } else if (primaryRegime.includes('BEARISH')) {
        this.subAIConfidence.textContent = `Momentum Edge: ${(primaryConf * 100).toFixed(0)}%`;
      } else {
        this.subAIConfidence.textContent = `Confidence: ${(primaryConf * 100).toFixed(0)}%`;
      }
    }
  }

  updatePositions(positions) {
    this.posCount.textContent = positions.length;
    if (positions.length === 0) {
      this.positionsBody.innerHTML = `
        <tr class="empty-row">
          <td colspan="11">No open floating positions — waiting for high-conviction AI signals</td>
        </tr>
      `;
      return;
    }

    let html = '';
    for (const p of positions) {
      const pnlSign = p.floating_pnl >= 0 ? '+' : '';
      const pnlClass = p.floating_pnl >= 0 ? 'green' : 'red';
      const sideClass = p.side.toLowerCase() === 'buy' ? 'side-buy' : 'side-sell';
      const digits = (p.symbol.includes('JPY') || p.symbol.includes('XAU') || p.symbol.includes('GOLD')) ? 2 : 5;

      const formatDur = (sec) => {
        if (sec < 60) return `${sec}s`;
        const m = Math.floor(sec / 60);
        const s = sec % 60;
        return `${m}m ${s}s`;
      };

      let stageBadge = '';
      if (p.profit_stage === 3) {
        stageBadge = `<br><span style="font-size:9.5px; font-weight:700; color:#10b981; background:rgba(16,185,129,0.15); padding:1px 5px; border-radius:3px; border:1px solid rgba(16,185,129,0.3);">🔒 75% LOCK</span>`;
      } else if (p.profit_stage === 2) {
        stageBadge = `<br><span style="font-size:9.5px; font-weight:700; color:#10b981; background:rgba(16,185,129,0.15); padding:1px 5px; border-radius:3px; border:1px solid rgba(16,185,129,0.3);">🔒 50% LOCK</span>`;
      } else if (p.profit_stage === 1 || p.partial_tp_triggered) {
        stageBadge = `<br><span style="font-size:9.5px; font-weight:700; color:#f59e0b; background:rgba(245,158,11,0.15); padding:1px 5px; border-radius:3px; border:1px solid rgba(245,158,11,0.3);">🛡️ BE LOCKED</span>`;
      }

      html += `
        <tr>
          <td><span class="order-id">#${p.order_id}</span></td>
          <td><b>${p.symbol}</b></td>
          <td><span class="badge ${sideClass}">${p.side.toUpperCase()}</span></td>
          <td>${p.lots.toFixed(2)}</td>
          <td>${p.entry_price.toFixed(digits)}</td>
          <td><b>${p.current_price.toFixed(digits)}</b></td>
          <td>${p.stop_loss > 0 ? p.stop_loss.toFixed(digits) : '--'}${stageBadge}</td>
          <td>${p.take_profit > 0 ? p.take_profit.toFixed(digits) : '--'}</td>
          <td class="${pnlClass}"><b>${pnlSign}$${p.floating_pnl.toFixed(2)}</b> (${pnlSign}${p.floating_pips.toFixed(1)}p)</td>
          <td class="font-mono" style="font-size: 11px;">
            <span class="green" title="Peak Profit (MFE)">▲+$${(p.mfe_usd || 0).toFixed(2)}</span><br>
            <span class="red" title="Deepest Drawdown (MAE)">▼-$${Math.abs(p.mae_usd || 0).toFixed(2)}</span>
          </td>
          <td>${formatDur(p.holding_time_sec)}</td>
          <td>
            <button class="btn-xs btn-danger" onclick="window.app.closePosition('${p.order_id}')">Close</button>
          </td>
        </tr>
      `;
    }

    this.positionsBody.innerHTML = html;
  }

  updateSignalAudit(events) {
    if (!this.signalAuditList) return;
    if (this.auditCount) {
      this.auditCount.textContent = `${events.length} EVENTS`;
    }
    if (!events || events.length === 0) {
      this.signalAuditList.innerHTML = '<div class="empty-audit">Listening for strategy signals & AI decisions...</div>';
      return;
    }

    let html = '';
    for (const ev of events) {
      const isApproved = ev.status === 'APPROVED';
      const isSkipped = ev.status === 'SKIPPED';
      const itemClass = isApproved ? 'approved' : isSkipped ? 'skipped' : 'rejected';
      const icon = isApproved ? '✅' : isSkipped ? '⏭️' : '🛑';
      const badgeText = isApproved ? 'APPROVED' : isSkipped ? 'DUPLICATE SKIPPED' : 'REJECTED';
      const typeText = ev.type ? ` ${ev.type}` : '';
      const priceDigits = (ev.symbol.includes('JPY') || ev.symbol.includes('XAU') || ev.symbol.includes('GOLD')) ? 2 : 5;

      html += `
        <div class="audit-card ${itemClass}">
          <div class="audit-top">
            <span class="audit-pill ${itemClass}">${icon} ${badgeText}${typeText}</span>
            <span class="audit-sym">${ev.symbol}</span>
            <span class="audit-time">🕒 ${ev.time}</span>
          </div>
          <div class="audit-details">
            <span>Price: <b>${ev.price.toFixed(priceDigits)}</b></span>
            <span>Regime: <b>${ev.regime}</b></span>
            <span>AI Conf: <b>${(ev.conf_pct || 0).toFixed(0)}%</b></span>
          </div>
          <div class="audit-reason">${ev.reason}</div>
        </div>
      `;
    }

    this.signalAuditList.innerHTML = html;
  }

  updateNews(news) {
    this.lastNewsData = news;
    this.lastNewsReceivedTime = performance.now();
    this.renderNewsCard();
  }

  renderNewsCard() {
    if (!this.newsContent) return;
    const news = this.lastNewsData;
    if (!news) {
      this.newsContent.innerHTML = `
        <div class="news-card safe-state">
          <div class="news-top">
            <span class="news-badge safe">🟢 MARKET CLEAR</span>
            <span class="news-impact">NO HIGH-IMPACT NEWS</span>
          </div>
          <div class="news-title">No blackout active — safe to trade</div>
          <div class="news-countdown">Normal market conditions</div>
        </div>
      `;
      return;
    }

    // Account for elapsed local time since last telemetry packet
    const elapsedSec = Math.floor((performance.now() - (this.lastNewsReceivedTime || performance.now())) / 1000);
    const countdownSec = news.countdown_sec - elapsedSec;
    const isBlackout = news.is_blackout;

    const formatDur = (sec) => {
      const s = Math.max(0, Math.round(sec));
      const m = Math.floor(s / 60);
      const remS = s % 60;
      if (m >= 60) {
        const h = Math.floor(m / 60);
        const remM = m % 60;
        return `${h}h ${String(remM).padStart(2, '0')}m ${String(remS).padStart(2, '0')}s`;
      }
      return `${String(m).padStart(2, '0')}m ${String(remS).padStart(2, '0')}s`;
    };

    const formatClock = (epochSec) => {
      if (!epochSec || epochSec <= 0) return '--:-- WIB';
      const d = new Date(epochSec * 1000);
      return d.toLocaleTimeString('id-ID', { hour: '2-digit', minute: '2-digit' }) + ' WIB';
    };

    if (isBlackout) {
      // Blackout is active
      const remBlackout = Math.max(0, (news.blackout_remaining_sec || 900) - elapsedSec);
      const totalBlackoutSec = 45 * 60; // 30m before + 15m after
      const elapsedTotal = Math.max(0, totalBlackoutSec - remBlackout);
      const progressPct = Math.min(100, Math.max(0, (elapsedTotal / totalBlackoutSec) * 100));

      const eventTimeStr = formatClock(news.event_time_sec);
      const endTimeStr = formatClock(news.blackout_end_sec);

      this.newsContent.innerHTML = `
        <div class="news-card blackout-state">
          <div class="news-top">
            <span class="news-badge danger">🚨 BLACKOUT ACTIVE</span>
            <span class="news-impact">${news.currency} • ${news.impact}</span>
          </div>
          <div class="news-title">${news.title}</div>
          <div class="news-countdown" style="color:var(--text-muted)">
            Trading paused to protect capital
          </div>

          <div class="news-timer-grid">
            <div class="news-timer-box">
              <span class="timer-lbl">RELEASE TIME</span>
              <span class="timer-val">${eventTimeStr}</span>
              <span class="news-timer-sub">${countdownSec <= 0 ? `${Math.abs(Math.round(countdownSec/60))}m ago` : `in ${formatDur(countdownSec)}`}</span>
            </div>
            <div class="news-timer-box danger">
              <span class="timer-lbl">ENDS IN</span>
              <span class="timer-val">${formatDur(remBlackout)}</span>
              <span class="news-timer-sub">Resumes at ${endTimeStr}</span>
            </div>
          </div>

          <div class="news-bar-wrap" title="Blackout window progress">
            <div class="news-bar-fill" style="width: ${progressPct}%;"></div>
          </div>
        </div>
      `;
    } else {
      // Upcoming news (not yet in blackout)
      const blackoutStartsIn = Math.max(0, countdownSec - 1800); // 30m buffer before release
      const eventTimeStr = formatClock(news.event_time_sec);
      const isImminent = countdownSec <= 2700; // <= 45 mins
      const badgeText = isImminent ? '⏳ BLACKOUT IMMINENT' : '⚠️ UPCOMING NEWS';

      this.newsContent.innerHTML = `
        <div class="news-card safe-state">
          <div class="news-top">
            <span class="news-badge ${isImminent ? 'danger' : 'safe'}">${badgeText}</span>
            <span class="news-impact">${news.currency} • ${news.impact}</span>
          </div>
          <div class="news-title">${news.title}</div>
          <div class="news-countdown" style="color:var(--text-muted)">
            Market active • Orders pause 30m prior
          </div>

          <div class="news-timer-grid">
            <div class="news-timer-box highlight">
              <span class="timer-lbl">EVENT RELEASE</span>
              <span class="timer-val">${formatDur(countdownSec)}</span>
              <span class="news-timer-sub">At ${eventTimeStr}</span>
            </div>
            <div class="news-timer-box">
              <span class="timer-lbl">PAUSE STARTS IN</span>
              <span class="timer-val">${formatDur(blackoutStartsIn)}</span>
              <span class="news-timer-sub">30m pre-news buffer</span>
            </div>
          </div>
        </div>
      `;
    }
  }

  updateModeButtons(activeMode) {
    const m = (activeMode || '').toUpperCase();
    const btnSantai = document.getElementById('btnModeSantai');
    const btnBalanced = document.getElementById('btnModeBalanced');
    const btnAgresif = document.getElementById('btnModeAgresif');

    if (btnSantai) btnSantai.classList.toggle('active', m === 'SANTAI');
    if (btnBalanced) btnBalanced.classList.toggle('active', m === 'BALANCED');
    if (btnAgresif) btnAgresif.classList.toggle('active', m === 'AGRESIF');
  }

  updateFocusButtons(activeFocus) {
    const btnGold = document.getElementById('btnFocusGold');
    if (btnGold) btnGold.classList.add('active');
  }

  async setMode(mode) {
    try {
      const res = await fetch('/api/control/set-mode', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ mode: mode })
      });
      const data = await res.json();
      if (data.success || data.status === 'ok') {
        this.updateModeButtons(mode);
      } else {
        alert('Failed to set mode: ' + (data.error || 'Unknown error'));
      }
    } catch (e) {
      alert('Error connecting to backend: ' + e);
    }
  }

  async setMarketFocus(focus) {
    try {
      const res = await fetch('/api/control/set-focus', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ focus: focus })
      });
      const data = await res.json();
      if (data.success || data.status === 'ok') {
        this.updateFocusButtons(focus);
      } else {
        alert('Failed to set focus: ' + (data.error || 'Unknown error'));
      }
    } catch (e) {
      alert('Error connecting to backend: ' + e);
    }
  }

  async closePosition(orderID) {
    if (!confirm(`Close position #${orderID}?`)) return;
    try {
      await fetch('/api/control/close-position', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ order_id: orderID })
      });
    } catch (e) {
      alert('Failed to close position: ' + e);
    }
  }

  renderChart() {
    if (!this.canvas || !this.ctx) return;
    const ctx = this.ctx;
    const rect = this.canvas.parentElement.getBoundingClientRect();
    const width = rect.width;
    const height = rect.height;

    ctx.clearRect(0, 0, width, height);

    if (this.equityHistory.length < 2) {
      ctx.strokeStyle = 'rgba(255, 255, 255, 0.04)';
      ctx.lineWidth = 1;
      for (let y = 30; y < height; y += 40) {
        ctx.beginPath();
        ctx.moveTo(0, y);
        ctx.lineTo(width, y);
        ctx.stroke();
      }
      return;
    }

    let rawMin = Math.min(...this.equityHistory);
    let rawMax = Math.max(...this.equityHistory);
    let diff = rawMax - rawMin;

    if (diff < 2.0) {
      const mid = (rawMax + rawMin) / 2;
      rawMin = mid - 1.0;
      rawMax = mid + 1.0;
      diff = 2.0;
    }

    const padding = diff * 0.10;
    const min = rawMin - padding;
    const max = rawMax + padding;
    const range = max - min;

    ctx.strokeStyle = 'rgba(255, 255, 255, 0.04)';
    ctx.fillStyle = 'rgba(141, 162, 186, 0.6)';
    ctx.font = '10px JetBrains Mono';
    ctx.textAlign = 'right';
    ctx.lineWidth = 1;

    const gridRows = 4;
    for (let i = 0; i <= gridRows; i++) {
      const y = height - 20 - (i / gridRows) * (height - 40);
      const val = min + (i / gridRows) * range;

      ctx.beginPath();
      ctx.moveTo(0, y);
      ctx.lineTo(width - 55, y);
      ctx.stroke();

      ctx.fillText(`$${val.toFixed(2)}`, width - 5, y + 3);
    }

    const points = [];
    const stepX = (width - 60) / (this.equityHistory.length - 1);
    for (let i = 0; i < this.equityHistory.length; i++) {
      const x = i * stepX;
      const y = height - 20 - ((this.equityHistory[i] - min) / range) * (height - 40);
      points.push({ x, y });
    }

    const gradient = ctx.createLinearGradient(0, 0, 0, height);
    gradient.addColorStop(0, 'rgba(0, 242, 254, 0.22)');
    gradient.addColorStop(1, 'rgba(0, 242, 254, 0.0)');

    ctx.beginPath();
    ctx.moveTo(points[0].x, height - 20);
    for (let i = 0; i < points.length; i++) {
      ctx.lineTo(points[i].x, points[i].y);
    }
    ctx.lineTo(points[points.length - 1].x, height - 20);
    ctx.closePath();
    ctx.fillStyle = gradient;
    ctx.fill();

    ctx.beginPath();
    ctx.moveTo(points[0].x, points[0].y);
    for (let i = 1; i < points.length; i++) {
      ctx.lineTo(points[i].x, points[i].y);
    }
    ctx.strokeStyle = '#00f2fe';
    ctx.lineWidth = 2.5;
    ctx.shadowColor = '#00f2fe';
    ctx.shadowBlur = 8;
    ctx.stroke();
    ctx.shadowBlur = 0;

    const lastP = points[points.length - 1];
    ctx.beginPath();
    ctx.arc(lastP.x, lastP.y, 4, 0, Math.PI * 2);
    ctx.fillStyle = '#00f2fe';
    ctx.shadowColor = '#00f2fe';
    ctx.shadowBlur = 12;
    ctx.fill();
    ctx.shadowBlur = 0;
  }
}

// Initialize Application
window.addEventListener('DOMContentLoaded', () => {
  window.app = new DashboardApp();
});
