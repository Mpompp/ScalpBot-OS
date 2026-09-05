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
    this.selectedChartSymbol = 'XAUUSDc';
    this.tvChart = null;
    this.tvCandleSeries = null;
    this.tvFastEMASeries = null;
    this.tvSlowEMASeries = null;
    this.tvPriceLines = [];
    this.tradeHistory = [];

    this.initElements();
    this.initClock();
    this.initEquityChart();
    this.initTradingViewChart();
    this.initEventListeners();
    this.connectWebSocket();
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
    this.telemSlippage = document.getElementById('telemSlippage');
    this.telemLatency = document.getElementById('telemLatency');
    this.wsLatency = document.getElementById('wsLatency');
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
    if (!this.canvas) return;
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
        secondsVisible: true,
        fixLeftEdge: true,
        rightOffset: 6,
        barSpacing: 9,
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
      const cleanSym = sym.replace(/c$/, '');
      const suffixedSym = cleanSym + 'c';
      const candleList = this.lastCandlesData[sym] || this.lastCandlesData[cleanSym] || this.lastCandlesData[suffixedSym];
      if (candleList && candleList.length > 0) {
        this.updateTVChart(sym, candleList);
      }
    }
  }

  updateTVChart(symbol, candles) {
    if (!this.tvCandleSeries || !candles || candles.length === 0) return;

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

      // Fit chart scale automatically on first data feed
      if (!this.tvChartFitted) {
        this.tvChart.timeScale().fitContent();
        this.tvChartFitted = true;
      }
    } catch (err) {
      console.warn('[TVChart] setData warning:', err);
    }

    // Update active position price lines for this symbol
    this.updateTVPriceLines();
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

    // 1. Update MT5 Bridge Connection Status
    if (p.symbols) {
      let isMT5Live = false;
      for (const s of Object.values(p.symbols)) {
        if (s.bid > 0) { isMT5Live = true; break; }
      }
      if (isMT5Live) {
        this.mt5BridgeBadge.className = 'mt5-badge connected';
        this.mt5StatusText.textContent = 'MT5 BRIDGE: CONNECTED';
      } else {
        this.mt5BridgeBadge.className = 'mt5-badge';
        this.mt5StatusText.textContent = 'MT5 BRIDGE: LISTENING';
      }
    }

    // 2. Update KPI Stats
    if (p.balance !== undefined) {
      this.valBalance.textContent = `$${p.balance.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`;
      if (this.subBalance) {
        // Auto-scale IDR equivalent based on Cent vs Regular account
        const isCent = (this.currentAccountType === 'CENT');
        const usdVal = isCent ? (p.balance / 100.0) : p.balance;
        const idrEquivalent = Math.round(usdVal * 16300);
        this.subBalance.textContent = `≈ Rp ${idrEquivalent.toLocaleString('id-ID')}${isCent ? ' (Cent)' : ''}`;
      }
    }
    if (p.equity !== undefined) this.valEquity.textContent = `$${p.equity.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`;
    
    if (p.floating_pnl !== undefined) {
      const sign = p.floating_pnl >= 0 ? '+' : '';
      this.valFloatingPnL.textContent = `${sign}$${p.floating_pnl.toFixed(2)}`;
      this.valFloatingPnL.className = `kpi-value ${p.floating_pnl >= 0 ? 'green' : 'red'}`;
    }

    // Daily Profit Target Goal Update
    this.updateProfitTarget(p);

    // Win Rate & Profit Factor Display (True History)
    if (p.performance) {
      if (p.performance.total_trades === 0) {
        this.valWinRate.textContent = '--%';
        this.subProfitFactor.textContent = 'Trades: 0 (No history)';
      } else {
        this.valWinRate.textContent = `${p.performance.win_rate_pct.toFixed(1)}%`;
        this.subProfitFactor.textContent = `PF: ${p.performance.profit_factor.toFixed(2)} | Trades: ${p.performance.total_trades}`;
      }
    } else if (p.win_rate_pct !== undefined) {
      if (p.win_rate_pct === 0) {
        this.valWinRate.textContent = '--%';
        this.subProfitFactor.textContent = 'Trades: 0 (No history)';
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
    if (p.symbols) {
      this.updateTickers(p.symbols);
      this.updateChartTabs(p.symbols);
    }

    // 5. Update Positions Table & TradingView Price Lines
    this.lastPositionsData = p.active_positions || [];
    this.updatePositions(this.lastPositionsData);

    // 6. Update TradingView Live Candles
    if (p.live_candles) {
      this.lastCandlesData = p.live_candles;
      const curSym = this.selectedChartSymbol;
      const cleanSym = curSym.replace(/[cm]$/i, '');
      const suffixedSymC = cleanSym + 'c';
      const suffixedSymM = cleanSym + 'm';
      const candleList = p.live_candles[curSym] || p.live_candles[cleanSym] || p.live_candles[suffixedSymC] || p.live_candles[suffixedSymM];
      if (candleList && candleList.length > 0) {
        this.updateTVChart(curSym, candleList);
      }
    }

    // 7. Update Signal Audit Feed
    this.updateSignalAudit(p.recent_events || []);

    // 8. Update News Card
    this.updateNews(p.upcoming_news);

    // 9. Update Focus & Mode Buttons
    if (p.trading_mode) this.updateModeButtons(p.trading_mode);
    if (p.market_focus) this.updateFocusButtons(p.market_focus);

    // 10. Update Performance Analytics Tab
    if (p.performance) {
      this.updateAnalytics(p.performance);
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
    const elWinRate = document.getElementById('statWinRate');
    const elWinsLosses = document.getElementById('statWinsLosses');
    const elPF = document.getElementById('statProfitFactor');
    const elGross = document.getElementById('statGrossRatio');
    const elRRR = document.getElementById('statRealizedRRR');
    const elAvgWL = document.getElementById('statAvgWinLoss');
    const elNet = document.getElementById('statNetProfit');
    const elTotal = document.getElementById('statTotalTrades');
    const symGrid = document.getElementById('symbolPerfGrid');

    if (elWinRate) elWinRate.textContent = `${perf.win_rate_pct.toFixed(1)}%`;
    if (elWinsLosses) elWinsLosses.textContent = `${perf.winning_trades} Wins / ${perf.losing_trades} Losses`;
    if (elPF) elPF.textContent = perf.profit_factor.toFixed(2);
    if (elGross) elGross.textContent = `Gross: +$${perf.gross_profit.toFixed(2)} / -$${perf.gross_loss.toFixed(2)}`;
    if (elRRR) elRRR.textContent = `1 : ${perf.realized_rrr.toFixed(2)}`;
    if (elAvgWL) elAvgWL.textContent = `Avg Win: +$${perf.average_win.toFixed(2)} / Avg Loss: -$${perf.average_loss.toFixed(2)}`;
    if (elNet) {
      const sign = perf.total_net_profit >= 0 ? '+' : '';
      elNet.textContent = `${sign}$${perf.total_net_profit.toFixed(2)}`;
      elNet.className = `analytics-val ${perf.total_net_profit >= 0 ? 'green' : 'red'}`;
    }
    if (elTotal) elTotal.textContent = `Total ${perf.total_trades} Completed Trades`;

    // Render Symbol Breakdown Grid
    if (symGrid && perf.symbol_breakdown) {
      let html = '';
      for (const [sym, s] of Object.entries(perf.symbol_breakdown)) {
        const netSign = s.net_profit >= 0 ? '+' : '';
        const netClass = s.net_profit >= 0 ? 'green' : 'red';
        html += `
          <div class="symbol-perf-card">
            <div class="symbol-perf-header">
              <span>${sym}</span>
              <span class="${netClass}">${netSign}$${s.net_profit.toFixed(2)}</span>
            </div>
            <div class="symbol-perf-stats">
              <div>Trades: <b>${s.trades}</b></div>
              <div>Win Rate: <b>${s.win_rate.toFixed(1)}%</b></div>
              <div>Wins: <b style="color:var(--accent-emerald)">${s.wins}</b></div>
              <div>Losses: <b style="color:var(--accent-crimson)">${s.losses}</b></div>
            </div>
          </div>
        `;
      }
      symGrid.innerHTML = html;
    }

    // Render Completed Trades Audit Table
    const histTbody = document.getElementById('historyTableBody');
    if (histTbody && perf.trade_history) {
      this.tradeHistory = perf.trade_history;
      if (perf.trade_history.length === 0) {
        histTbody.innerHTML = `
          <tr class="empty-row">
            <td colspan="11">Recording completed live trades...</td>
          </tr>
        `;
      } else {
        let rows = '';
        for (const t of perf.trade_history) {
          const isWin = t.pnl >= 0;
          const pnlClass = isWin ? 'green' : 'red';
          const pnlSign = isWin ? '+' : '';
          const badgeClass = isWin ? 'res-win' : 'res-loss';
          const badgeText = isWin ? 'WIN' : 'LOSS';
          const pipsSign = t.pips >= 0 ? '+' : '';
          const symStr = t.symbol || '';
          const isJpy = symStr.includes('JPY');
          const isGold = symStr.includes('XAU') || symStr.includes('GOLD');
          const dec = isJpy ? 3 : (isGold ? 3 : 5);

          rows += `
            <tr>
              <td class="font-mono">#${t.ticket}</td>
              <td><b>${t.symbol}</b></td>
              <td><span class="side-badge ${t.side.toLowerCase()}">${t.side}</span></td>
              <td class="font-mono">${t.lots.toFixed(2)}</td>
              <td class="font-mono">${t.entry.toFixed(dec)}</td>
              <td class="font-mono">${t.exit.toFixed(dec)}</td>
              <td class="font-mono ${pnlClass}"><b>${pnlSign}$${t.pnl.toFixed(2)}</b></td>
              <td class="font-mono green">▲+$${(t.mfe_usd || 0).toFixed(2)}</td>
              <td class="font-mono red">▼-$${Math.abs(t.mae_usd || 0).toFixed(2)}</td>
              <td class="font-mono ${pnlClass}">${pipsSign}${t.pips.toFixed(1)} pips</td>
              <td class="font-mono">${t.duration || '--'}</td>
              <td class="font-mono text-muted">${t.time || '--'}</td>
              <td><span class="result-badge ${badgeClass}">${badgeText}</span></td>
            </tr>
          `;
        }
        histTbody.innerHTML = rows;
      }
    }
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

      const digits = (sym.includes('JPY') || sym.includes('XAU') || sym.includes('GOLD')) ? 2 : 5;
      const bidStr = data.bid > 0 ? data.bid.toFixed(digits) : '--';
      const askStr = data.ask > 0 ? data.ask.toFixed(digits) : '--';
      const spreadStr = data.spread_pip > 0 ? `${data.spread_pip.toFixed(1)} pips` : '0.0 pips';

      const assetIcon = data.asset_icon || (sym.includes('XAU') || sym.includes('GOLD') ? '🪙' : '💵');
      const assetClass = data.asset_class || (sym.includes('XAU') || sym.includes('GOLD') ? 'GOLD' : 'FOREX');
      const assetBadgeClass = assetClass.toLowerCase().includes('gold') ? 'gold' : (assetClass.toLowerCase().includes('crypto') ? 'crypto' : 'forex');

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
        sigBadgeHtml = `<span class="ticker-sig-tag rejected">🛑 REJECTED${sigType}</span>`;
      } else if (sigStatus === 'APPROVED') {
        sigBadgeHtml = `<span class="ticker-sig-tag approved">✅ APPROVED${sigType}</span>`;
      } else if (sigStatus === 'SKIPPED') {
        sigBadgeHtml = `<span class="ticker-sig-tag skipped">⏭️ SKIPPED</span>`;
      } else {
        sigBadgeHtml = `<span class="ticker-sig-tag standby">● STANDBY</span>`;
      }

      html += `
        <div class="ticker-card" id="ticker-${sym}">
          <div class="ticker-top">
            <div style="display: flex; align-items: center; gap: 6px;">
              <span class="ticker-sym">${sym}</span>
              <span class="ticker-asset-badge ${assetBadgeClass}">${assetIcon} ${assetClass}</span>
            </div>
            <div class="ticker-top-right">
              <span class="ticker-chg-badge ${chgClass}">${tf.toUpperCase()} ${chgStr}</span>
              <span class="ticker-spread">${spreadStr}</span>
            </div>
          </div>

          <div class="ticker-prices">
            <div class="price-box bid-box"><span class="lbl">BID</span><span class="val">${bidStr}</span></div>
            <div class="price-box ask-box"><span class="lbl">ASK</span><span class="val">${askStr}</span></div>
          </div>

          <div class="ticker-stats-bar">
            <span>H: ${highStr}</span>
            <span>L: ${lowStr}</span>
            <span class="ticker-timestamp">🕒 ${data.last_tick_time || '--:--:--'} (${timeAgoText})</span>
          </div>

          <div class="ticker-footer-row">
            <span class="ticker-regime-pill" style="color: ${regimeColor}; background: ${regimeBg}">● ${regimeDisplay}</span>
            <span class="ticker-vol-badge ${(data.vol_ratio !== undefined && data.vol_ratio < 0.6) ? 'red' : 'green'}" title="Volatility Ratio (ATR / 50-bar Baseline)">⚡ ${(data.vol_ratio || 1.0).toFixed(2)}x VOL</span>
            ${sigBadgeHtml}
          </div>
        </div>
      `;
    }

    this.tickerGrid.innerHTML = html;

    if (primaryRegime === 'RANGING_CHOP') {
      this.valAIRegime.textContent = 'RANGE SCALPER';
      this.valAIRegime.style.color = '#00e676';
      this.subAIConfidence.textContent = `Mean-Reversion Edge: ${(primaryConf * 100).toFixed(0)}%`;
    } else if (primaryRegime === 'HIGH_VOLATILITY_EVENT' || primaryRegime === 'WALL_EXHAUSTION') {
      this.valAIRegime.textContent = 'WALL / CASH DEFENDER';
      this.valAIRegime.style.color = '#ff4757';
      this.subAIConfidence.textContent = `Capital Protection Active`;
    } else if (primaryRegime.includes('BULLISH')) {
      this.valAIRegime.textContent = 'TREND HUNTER (BULL)';
      this.valAIRegime.style.color = '#00e676';
      this.subAIConfidence.textContent = `Momentum Edge: ${(primaryConf * 100).toFixed(0)}%`;
    } else if (primaryRegime.includes('BEARISH')) {
      this.valAIRegime.textContent = 'TREND HUNTER (BEAR)';
      this.valAIRegime.style.color = '#ff5252';
      this.subAIConfidence.textContent = `Momentum Edge: ${(primaryConf * 100).toFixed(0)}%`;
    } else {
      this.valAIRegime.textContent = primaryRegime.replace(/_/g, ' ');
      this.valAIRegime.style.color = 'var(--accent-cyan)';
      this.subAIConfidence.textContent = `Confidence: ${(primaryConf * 100).toFixed(0)}%`;
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

      html += `
        <tr>
          <td><span class="order-id">#${p.order_id}</span></td>
          <td><b>${p.symbol}</b></td>
          <td><span class="badge ${sideClass}">${p.side.toUpperCase()}</span></td>
          <td>${p.lots.toFixed(2)}</td>
          <td>${p.entry_price.toFixed(digits)}</td>
          <td><b>${p.current_price.toFixed(digits)}</b></td>
          <td>${p.stop_loss > 0 ? p.stop_loss.toFixed(digits) : '--'}</td>
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
    if (!this.newsContent) return;
    if (!news) {
      this.newsContent.innerHTML = `
        <div class="news-card safe-state">
          <div class="news-top">
            <span class="news-badge safe">MARKET CLEAR</span>
            <span class="news-impact">NO HIGH-IMPACT NEWS</span>
          </div>
          <div class="news-title">No blackout active — safe to trade</div>
          <div class="news-countdown">Normal market conditions</div>
        </div>
      `;
      return;
    }

    const isBlackout = news.is_blackout;
    const badgeClass = isBlackout ? 'danger' : 'safe';
    const badgeText = isBlackout ? '🚨 BLACKOUT ACTIVE' : '⚠️ UPCOMING NEWS';
    const countdownMin = Math.floor(news.countdown_sec / 60);

    this.newsContent.innerHTML = `
      <div class="news-card ${isBlackout ? 'blackout-state' : 'safe-state'}">
        <div class="news-top">
          <span class="news-badge ${badgeClass}">${badgeText}</span>
          <span class="news-impact">${news.currency} • ${news.impact}</span>
        </div>
        <div class="news-title">${news.title}</div>
        <div class="news-countdown">
          ${isBlackout ? 'Trading paused to protect capital' : `Releasing in ~${countdownMin} minutes`}
        </div>
      </div>
    `;
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
    const f = (activeFocus || '').toUpperCase();
    const btnAll = document.getElementById('btnFocusAll');
    const btnGold = document.getElementById('btnFocusGold');
    const btnForex = document.getElementById('btnFocusForex');

    if (btnAll) btnAll.classList.toggle('active', f === 'ALL' || f === '');
    if (btnGold) btnGold.classList.toggle('active', f === 'GOLD_ONLY');
    if (btnForex) btnForex.classList.toggle('active', f === 'FOREX_ONLY');
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
