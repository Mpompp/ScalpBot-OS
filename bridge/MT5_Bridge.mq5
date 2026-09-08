//+------------------------------------------------------------------+
//|                                                   MT5_Bridge.mq5 |
//|                                  Copyright 2026, ScalpBot Engine |
//|                                      Low-Latency Go MT5 Bridge   |
//+------------------------------------------------------------------+
#property copyright "ScalpBot Engine"
#property link      "https://github.com/pompbot/scalpbot"
#property version   "2.20"
#property description "Sub-millisecond Native TCP IPC Bridge connecting MetaTrader 5 to Go ScalpBot"
#property strict

#include <Trade\Trade.mqh>

//--- Input parameters
input group "=== Connection Settings ==="
input string   InpHost        = "127.0.0.1"; // Go ScalpBot Server Host
input ushort   InpCommandPort = 5555;        // Command Port (REQ-REP)
input ushort   InpStreamPort  = 5556;        // Tick Stream Port (PUB)
input ulong    InpMagicNumber = 123456;      // EA Magic Number
input ulong    InpSlippage    = 10;          // Slippage in points
input int      InpTimerMs     = 50;          // Polling timer interval (ms)
input string   InpCustomSuffix= "";          // Optional Symbol Suffix override (e.g. 'c', '.m', '.pro')
input string   InpSymbolsToStream = "";      // Comma-separated symbols (blank = auto-stream Gold symbols: XAUUSD, XAUUSDc, etc.)

//--- Global socket handles
int       g_cmdSocket    = INVALID_HANDLE;
int       g_streamSocket = INVALID_HANDLE;

CTrade    g_trade;

//+------------------------------------------------------------------+
//| Helper: Normalize Volume to Broker Lot Step & Min/Max Bounds     |
//+------------------------------------------------------------------+
double NormalizeLot(const string symbol, double requestedLot)
{
   double minLot  = SymbolInfoDouble(symbol, SYMBOL_VOLUME_MIN);
   double maxLot  = SymbolInfoDouble(symbol, SYMBOL_VOLUME_MAX);
   double lotStep = SymbolInfoDouble(symbol, SYMBOL_VOLUME_STEP);

   if(minLot <= 0)  minLot  = 0.01;
   if(maxLot <= 0)  maxLot  = 100.0;
   if(lotStep <= 0) lotStep = 0.01;

   double lot = MathFloor(requestedLot / lotStep + 0.000001) * lotStep;

   if(lot < minLot) lot = minLot;
   if(lot > maxLot) lot = maxLot;

   int digits = 2;
   if(lotStep == 0.1) digits = 1;
   else if(lotStep == 1.0) digits = 0;
   else if(lotStep == 0.001) digits = 3;

   return NormalizeDouble(lot, digits);
}

//+------------------------------------------------------------------+
//| Helper: Resolve Symbol matching broker suffix/prefix             |
//+------------------------------------------------------------------+
string ResolveSymbol(const string requestedSymbol)
{
   if(requestedSymbol == "")
      return _Symbol;

   // Auto select into MarketWatch if available
   SymbolSelect(requestedSymbol, true);
   if(SymbolInfoDouble(requestedSymbol, SYMBOL_BID) > 0)
      return requestedSymbol;

   if(InpCustomSuffix != "")
   {
      string testSym = requestedSymbol + InpCustomSuffix;
      SymbolSelect(testSym, true);
      if(SymbolInfoDouble(testSym, SYMBOL_BID) > 0)
         return testSym;
   }

   if(StringFind(_Symbol, requestedSymbol) >= 0)
      return _Symbol;

   // Handle XAUUSD / GOLD aliases
   if(requestedSymbol == "XAUUSD" || requestedSymbol == "GOLD")
   {
      string goldAliases[] = {"XAUUSD", "GOLD", "XAUUSD.m", "XAUUSDm", "XAUUSD.pro", "XAUUSD.r", "GOLD.m"};
      for(int i = 0; i < ArraySize(goldAliases); i++)
      {
         SymbolSelect(goldAliases[i], true);
         if(SymbolInfoDouble(goldAliases[i], SYMBOL_BID) > 0)
            return goldAliases[i];
      }
   }

   string commonSuffixes[] = {"c", "m", ".m", ".r", ".pro", ".ecn", ".raw", "_i", ".a"};
   for(int i = 0; i < ArraySize(commonSuffixes); i++)
   {
      string candidate = requestedSymbol + commonSuffixes[i];
      SymbolSelect(candidate, true);
      if(SymbolInfoDouble(candidate, SYMBOL_BID) > 0)
         return candidate;
   }

   return requestedSymbol;
}

//+------------------------------------------------------------------+
//| Helper: Configure appropriate order filling type for symbol      |
//+------------------------------------------------------------------+
void SetBrokerFillingType(const string symbol)
{
   uint filling = (uint)SymbolInfoInteger(symbol, SYMBOL_FILLING_MODE);
   if((filling & SYMBOL_FILLING_IOC) != 0)
      g_trade.SetTypeFilling(ORDER_FILLING_IOC);
   else if((filling & SYMBOL_FILLING_FOK) != 0)
      g_trade.SetTypeFilling(ORDER_FILLING_FOK);
   else
      g_trade.SetTypeFilling(ORDER_FILLING_RETURN);
}

//+------------------------------------------------------------------+
//| Helper: Maintain Connection to Go Server                         |
//+------------------------------------------------------------------+
void EnsureConnections()
{
   // 1. Connect Command Socket
   if(g_cmdSocket == INVALID_HANDLE || !SocketIsConnected(g_cmdSocket))
   {
      if(g_cmdSocket != INVALID_HANDLE)
         SocketClose(g_cmdSocket);

      g_cmdSocket = SocketCreate();
      if(g_cmdSocket != INVALID_HANDLE)
      {
         ResetLastError();
         if(SocketConnect(g_cmdSocket, InpHost, InpCommandPort, 1000))
         {
            PrintFormat("[MT5_Bridge] SUCCESS: Connected to Go Command Server (%s:%d)", InpHost, InpCommandPort);
         }
         else
         {
            int err = GetLastError();
            PrintFormat("[MT5_Bridge] Command SocketConnect to %s:%d failed, error=%d", InpHost, InpCommandPort, err);
            SocketClose(g_cmdSocket);
            g_cmdSocket = INVALID_HANDLE;
         }
      }
      else
      {
         PrintFormat("[MT5_Bridge] SocketCreate failed, error=%d", GetLastError());
      }
   }

   // 2. Connect Stream Socket
   if(g_streamSocket == INVALID_HANDLE || !SocketIsConnected(g_streamSocket))
   {
      if(g_streamSocket != INVALID_HANDLE)
         SocketClose(g_streamSocket);

      g_streamSocket = SocketCreate();
      if(g_streamSocket != INVALID_HANDLE)
      {
         ResetLastError();
         if(SocketConnect(g_streamSocket, InpHost, InpStreamPort, 1000))
         {
            PrintFormat("[MT5_Bridge] SUCCESS: Connected to Go Stream Server (%s:%d)", InpHost, InpStreamPort);
         }
         else
         {
            int err = GetLastError();
            PrintFormat("[MT5_Bridge] Stream SocketConnect to %s:%d failed, error=%d", InpHost, InpStreamPort, err);
            SocketClose(g_streamSocket);
            g_streamSocket = INVALID_HANDLE;
         }
      }
   }
}

//+------------------------------------------------------------------+
//| Expert initialization function                                   |
//+------------------------------------------------------------------+
int OnInit()
{
   g_trade.SetExpertMagicNumber(InpMagicNumber);
   g_trade.SetDeviationInPoints(InpSlippage);
   SetBrokerFillingType(_Symbol);

   PrintFormat("[MT5_Bridge] Initializing Bridge Client for Go ScalpBot (%s:%d, %d)...", 
               InpHost, InpCommandPort, InpStreamPort);

   EnsureConnections();
   EventSetMillisecondTimer(InpTimerMs);

   return INIT_SUCCEEDED;
}

//+------------------------------------------------------------------+
//| Expert deinitialization function                                 |
//+------------------------------------------------------------------+
void OnDeinit(const int reason)
{
   EventKillTimer();

   if(g_cmdSocket != INVALID_HANDLE)    { SocketClose(g_cmdSocket);    g_cmdSocket = INVALID_HANDLE; }
   if(g_streamSocket != INVALID_HANDLE) { SocketClose(g_streamSocket); g_streamSocket = INVALID_HANDLE; }

   PrintFormat("[MT5_Bridge] Deinitialized (reason=%d).", reason);
}

//+------------------------------------------------------------------+
//| Push tick for a specific symbol to Go Stream Socket              |
//+------------------------------------------------------------------+
void PushSymbolTick(const string symbol)
{
   if(g_streamSocket == INVALID_HANDLE || !SocketIsConnected(g_streamSocket))
      return;

   MqlTick lastTick;
   if(!SymbolInfoTick(symbol, lastTick) || lastTick.bid <= 0)
      return;

   long timestampNs = (lastTick.time_msc > 0) ? (long)lastTick.time_msc * 1000000 : (long)TimeCurrent() * 1000000000;
   int digits = (int)SymbolInfoInteger(symbol, SYMBOL_DIGITS);

   string tickJson = StringFormat("{\"symbol\":\"%s\",\"bid\":%s,\"ask\":%s,\"timestamp_ns\":%I64d}\n",
                                  symbol, DoubleToString(lastTick.bid, digits), DoubleToString(lastTick.ask, digits), timestampNs);

   int tickLen = StringLen(tickJson);
   uchar data[];
   StringToCharArray(tickJson, data, 0, tickLen);

   int sent = SocketSend(g_streamSocket, data, tickLen);
   if(sent <= 0)
   {
      int err = GetLastError();
      if(err != 0 && err != 5273)
      {
         SocketClose(g_streamSocket);
         g_streamSocket = INVALID_HANDLE;
      }
   }
}

//+------------------------------------------------------------------+
//| Stream ticks for all active or specified MarketWatch symbols     |
//+------------------------------------------------------------------+
void StreamAllConfiguredTicks()
{
   if(g_streamSocket == INVALID_HANDLE || !SocketIsConnected(g_streamSocket))
      return;

   if(InpSymbolsToStream != "")
   {
      string syms[];
      int count = StringSplit(InpSymbolsToStream, ',', syms);
      for(int i = 0; i < count; i++)
      {
         string s = ResolveSymbol(syms[i]);
         PushSymbolTick(s);
      }
      return;
   }

   // Always push the current chart symbol
   PushSymbolTick(_Symbol);

   // Auto-stream Gold commodity instruments (XAUUSD and cent/micro variants)
   string defaultPairs[] = {"XAUUSD", "GOLD"};
   for(int i = 0; i < ArraySize(defaultPairs); i++)
   {
      string sym = ResolveSymbol(defaultPairs[i]);
      if(sym != _Symbol && sym != "")
      {
         PushSymbolTick(sym);
      }
   }

   // Also stream any active MarketWatch symbols dedicated to Gold
   int total = SymbolsTotal(true);
   for(int i = 0; i < total; i++)
   {
      string sym = SymbolName(i, true);
      string symUpper = sym;
      StringToUpper(symUpper);
      if(sym != _Symbol && (StringFind(symUpper, "XAU") >= 0 || StringFind(symUpper, "GOLD") >= 0))
      {
         PushSymbolTick(sym);
      }
   }
}

//+------------------------------------------------------------------+
//| OnTick handler: Push tick stream to Go client                    |
//+------------------------------------------------------------------+
void OnTick()
{
   StreamAllConfiguredTicks();
}

//+------------------------------------------------------------------+
//| Robust JSON Field Parsers                                        |
//+------------------------------------------------------------------+
string ExtractJsonString(const string json, const string key)
{
   string pattern = "\"" + key + "\"";
   int keyPos = StringFind(json, pattern);
   if(keyPos < 0) return "";

   int colonPos = StringFind(json, ":", keyPos + StringLen(pattern));
   if(colonPos < 0) return "";

   int quoteStart = StringFind(json, "\"", colonPos);
   if(quoteStart < 0) return "";

   int quoteEnd = StringFind(json, "\"", quoteStart + 1);
   if(quoteEnd < 0) return "";

   return StringSubstr(json, quoteStart + 1, quoteEnd - quoteStart - 1);
}

double ExtractJsonDouble(const string json, const string key)
{
   string pattern = "\"" + key + "\"";
   int keyPos = StringFind(json, pattern);
   if(keyPos < 0) return 0.0;

   int colonPos = StringFind(json, ":", keyPos + StringLen(pattern));
   if(colonPos < 0) return 0.0;

   int start = colonPos + 1;
   while(start < StringLen(json))
   {
      ushort ch = StringGetCharacter(json, start);
      if(ch != ' ' && ch != '\t' && ch != '\r' && ch != '\n')
         break;
      start++;
   }

   int end = start;
   while(end < StringLen(json))
   {
      ushort ch = StringGetCharacter(json, end);
      if(ch == ',' || ch == '}' || ch == ' ' || ch == '\r' || ch == '\n' || ch == '\t')
         break;
      end++;
   }

   if(end <= start) return 0.0;
   return StringToDouble(StringSubstr(json, start, end - start));
}

//+------------------------------------------------------------------+
//| Execute a Single Order with Robust Stops & Market Mode Fallback  |
//+------------------------------------------------------------------+
bool ExecuteOrderSingle(bool isBuy, const string symbol, double lots, double sl, double tp, ulong &outTicket, double &outFillPrice, uint &outRetcode, string &outErrMsg)
{
   int digits = (int)SymbolInfoInteger(symbol, SYMBOL_DIGITS);
   double point = SymbolInfoDouble(symbol, SYMBOL_POINT);
   double ask = SymbolInfoDouble(symbol, SYMBOL_ASK);
   double bid = SymbolInfoDouble(symbol, SYMBOL_BID);
   double price = isBuy ? ask : bid;

   long stopsLevel = SymbolInfoInteger(symbol, SYMBOL_TRADE_STOPS_LEVEL);
   long freezeLevel = SymbolInfoInteger(symbol, SYMBOL_TRADE_FREEZE_LEVEL);
   double minDistance = MathMax((double)stopsLevel, (double)freezeLevel) * point;
   if(minDistance <= 0) minDistance = 15 * point;

   // Sanitize & Clamp StopLoss and TakeProfit against Broker StopsLevel
   if(isBuy)
   {
      if(sl > 0)
      {
         if(sl > bid - minDistance) sl = bid - minDistance;
         sl = NormalizeDouble(sl, digits);
      }
      if(tp > 0)
      {
         if(tp < ask + minDistance) tp = ask + minDistance;
         tp = NormalizeDouble(tp, digits);
      }
   }
   else
   {
      if(sl > 0)
      {
         if(sl < ask + minDistance) sl = ask + minDistance;
         sl = NormalizeDouble(sl, digits);
      }
      if(tp > 0)
      {
         if(tp > bid - minDistance) tp = bid - minDistance;
         tp = NormalizeDouble(tp, digits);
      }
   }

   SetBrokerFillingType(symbol);

   // Attempt 1: Standard PositionOpen with SL/TP
   bool res = isBuy ? g_trade.Buy(lots, symbol, 0.0, sl, tp, "GoScalpBot") : g_trade.Sell(lots, symbol, 0.0, sl, tp, "GoScalpBot");
   outRetcode = g_trade.ResultRetcode();
   ulong dealTicket = g_trade.ResultDeal();
   ulong orderTicket = g_trade.ResultOrder();

   // Attempt 2 (Fallback): If rejected due to Invalid Stops (10016 / Market Execution Broker)
   if(!res && outRetcode == 10016)
   {
      PrintFormat("[MT5_Bridge] retcode=10016 detected (Market Execution mode) — opening at market and setting stops via PositionModify");
      res = isBuy ? g_trade.Buy(lots, symbol, 0.0, 0.0, 0.0, "GoScalpBot") : g_trade.Sell(lots, symbol, 0.0, 0.0, 0.0, "GoScalpBot");
      outRetcode = g_trade.ResultRetcode();
      dealTicket = g_trade.ResultDeal();
      orderTicket = g_trade.ResultOrder();

      if(res || dealTicket > 0 || orderTicket > 0 || outRetcode == TRADE_RETCODE_DONE || outRetcode == TRADE_RETCODE_PLACED)
      {
         ulong filledTicket = (dealTicket > 0) ? dealTicket : orderTicket;
         if(sl > 0 || tp > 0)
         {
            Sleep(30);
            g_trade.PositionModify(filledTicket, sl, tp);
         }
      }
   }

   // Check if filled
   if(res || dealTicket > 0 || orderTicket > 0 || outRetcode == TRADE_RETCODE_DONE || outRetcode == TRADE_RETCODE_PLACED)
   {
      ulong posTicket = 0;
      // Search open positions for the newly opened position on this symbol and magic number
      int totalPos = PositionsTotal();
      for(int i = totalPos - 1; i >= 0; i--)
      {
         ulong t = PositionGetTicket(i);
         if(t > 0 && PositionGetString(POSITION_SYMBOL) == symbol)
         {
            long magic = PositionGetInteger(POSITION_MAGIC);
            if(magic == (long)InpMagicNumber || InpMagicNumber == 0)
            {
               posTicket = t;
               break;
            }
         }
      }

      if(posTicket > 0)
         outTicket = posTicket;
      else
         outTicket = (dealTicket > 0) ? dealTicket : orderTicket;

      outFillPrice = g_trade.ResultPrice();
      if(outFillPrice <= 0) outFillPrice = price;
      return true;
   }

   // Execution failed
   outErrMsg = g_trade.ResultRetcodeDescription();
   if(outErrMsg == "") outErrMsg = StringFormat("Error %u", outRetcode);
   PrintFormat("[MT5_Bridge] Single order execution rejected %s %s %.2f lots: %s (retcode=%u)",
               isBuy ? "BUY" : "SELL", symbol, lots, outErrMsg, outRetcode);
   return false;
}

//+------------------------------------------------------------------+
//| Enforce StopLoss / TakeProfit on all open positions               |
//+------------------------------------------------------------------+
void EnforceStopsOnOpenPositions()
{
   int total = PositionsTotal();
   for(int i = 0; i < total; i++)
   {
      ulong ticket = PositionGetTicket(i);
      if(ticket > 0)
      {
         long magic = PositionGetInteger(POSITION_MAGIC);
         if(magic == (long)InpMagicNumber || InpMagicNumber == 0)
         {
            double curSL = PositionGetDouble(POSITION_SL);
            double curTP = PositionGetDouble(POSITION_TP);
            
            if(curSL == 0.0 || curTP == 0.0)
            {
               string pSym = PositionGetString(POSITION_SYMBOL);
               int pDigits = (int)SymbolInfoInteger(pSym, SYMBOL_DIGITS);
               ENUM_POSITION_TYPE pType = (ENUM_POSITION_TYPE)PositionGetInteger(POSITION_TYPE);
               double openPrice = PositionGetDouble(POSITION_PRICE_OPEN);

               double dist = 0;
               int handle = iATR(pSym, PERIOD_M5, 14);
               if(handle != INVALID_HANDLE)
               {
                  double atrVal[];
                  ArraySetAsSeries(atrVal, true);
                  if(CopyBuffer(handle, 0, 0, 1, atrVal) > 0)
                  {
                     dist = atrVal[0] * 1.5;
                  }
                  IndicatorRelease(handle);
               }
               
               if(StringFind(pSym, "XAU") >= 0 || StringFind(pSym, "GOLD") >= 0)
               {
                  if(dist < 3.50) dist = 3.50; // Minimum $3.50 SL on Gold
               }
               else if(dist <= 0)
               {
                  double point = SymbolInfoDouble(pSym, SYMBOL_POINT);
                  dist = 50 * point; // ~5 pips default fallback
                  if(StringFind(pSym, "JPY") >= 0) dist = 0.05;
               }

               double tpDist = dist * 2.0; // 1:2 RRR target
               double targetSL = (pType == POSITION_TYPE_BUY) ? openPrice - dist : openPrice + dist;
               double targetTP = (pType == POSITION_TYPE_BUY) ? openPrice + tpDist : openPrice - tpDist;

               g_trade.PositionModify(ticket, NormalizeDouble(targetSL, pDigits), NormalizeDouble(targetTP, pDigits));
            }
         }
      }
   }
}

//+------------------------------------------------------------------+
//| Dispatch command received from Go ScalpBot                       |
//+------------------------------------------------------------------+
string HandleSingleCommand(string jsonReq)
{
   StringReplace(jsonReq, "\r", "");
   StringReplace(jsonReq, "\n", "");
   StringReplace(jsonReq, "\t", "");
   StringTrimLeft(jsonReq);
   StringTrimRight(jsonReq);

   if(StringLen(jsonReq) == 0 || StringFind(jsonReq, "{") < 0)
      return "";

   string action    = ExtractJsonString(jsonReq, "action");
   string reqId     = ExtractJsonString(jsonReq, "request_id");
   string rawSymbol = ExtractJsonString(jsonReq, "symbol");
   double rawLots   = ExtractJsonDouble(jsonReq, "lots");
   double sl        = ExtractJsonDouble(jsonReq, "stop_loss");
   double tp        = ExtractJsonDouble(jsonReq, "take_profit");
   string ticketStr = ExtractJsonString(jsonReq, "ticket");

   string symbol    = ResolveSymbol(rawSymbol);
   double lots      = (rawLots > 0) ? NormalizeLot(symbol, rawLots) : 0;
   long   nowNs     = (long)TimeCurrent() * 1000000000;
   int    digits    = (int)SymbolInfoInteger(symbol, SYMBOL_DIGITS);

   // --- PING ---
   if(action == "PING")
   {
      return StringFormat("{\"request_id\":\"%s\",\"success\":true,\"timestamp_ns\":%I64d}", reqId, nowNs);
   }

   // --- BUY ---
   if(action == "BUY")
   {
      if(!MQLInfoInteger(MQL_TRADE_ALLOWED))
      {
         return StringFormat("{\"request_id\":\"%s\",\"success\":false,\"retcode\":4756,\"error_msg\":\"Algo Trading Disabled for EA! Press F7 on Chart -> Common -> Check 'Allow Algo Trading'\",\"timestamp_ns\":%I64d}",
                             reqId, nowNs);
      }

      double ask = SymbolInfoDouble(symbol, SYMBOL_ASK);
      if(sl > 0) sl = NormalizeDouble(sl, digits);
      if(tp > 0) tp = NormalizeDouble(tp, digits);

      ulong ticket = 0;
      double fillPrice = 0;
      uint retcode = 0;
      string errMsg = "";

      if(ExecuteOrderSingle(true, symbol, lots, sl, tp, ticket, fillPrice, retcode, errMsg))
      {
         if(fillPrice <= 0) fillPrice = ask;
         return StringFormat("{\"request_id\":\"%s\",\"success\":true,\"retcode\":%u,\"ticket\":\"%I64u\",\"fill_price\":%s,\"lots\":%.2f,\"timestamp_ns\":%I64d}",
                             reqId, retcode, ticket, DoubleToString(fillPrice, digits), lots, nowNs);
      }
      else
      {
         return StringFormat("{\"request_id\":\"%s\",\"success\":false,\"retcode\":%u,\"error_msg\":\"%s\",\"timestamp_ns\":%I64d}",
                             reqId, retcode, errMsg, nowNs);
      }
   }

   // --- SELL ---
   if(action == "SELL")
   {
      if(!MQLInfoInteger(MQL_TRADE_ALLOWED))
      {
         return StringFormat("{\"request_id\":\"%s\",\"success\":false,\"retcode\":4756,\"error_msg\":\"Algo Trading Disabled for EA! Press F7 on Chart -> Common -> Check 'Allow Algo Trading'\",\"timestamp_ns\":%I64d}",
                             reqId, nowNs);
      }

      double bid = SymbolInfoDouble(symbol, SYMBOL_BID);
      if(sl > 0) sl = NormalizeDouble(sl, digits);
      if(tp > 0) tp = NormalizeDouble(tp, digits);

      ulong ticket = 0;
      double fillPrice = 0;
      uint retcode = 0;
      string errMsg = "";

      if(ExecuteOrderSingle(false, symbol, lots, sl, tp, ticket, fillPrice, retcode, errMsg))
      {
         if(fillPrice <= 0) fillPrice = bid;
         return StringFormat("{\"request_id\":\"%s\",\"success\":true,\"retcode\":%u,\"ticket\":\"%I64u\",\"fill_price\":%s,\"lots\":%.2f,\"timestamp_ns\":%I64d}",
                             reqId, retcode, ticket, DoubleToString(fillPrice, digits), lots, nowNs);
      }
      else
      {
         return StringFormat("{\"request_id\":\"%s\",\"success\":false,\"retcode\":%u,\"error_msg\":\"%s\",\"timestamp_ns\":%I64d}",
                             reqId, retcode, errMsg, nowNs);
      }
   }

   // --- CLOSE ---
   if(action == "CLOSE")
   {
      ulong ticket = (ulong)StringToInteger(ticketStr);
      bool res = false;
      if(lots > 0 && PositionSelectByTicket(ticket))
      {
         double curVol = PositionGetDouble(POSITION_VOLUME);
         if(lots < curVol - 0.0001)
         {
            res = g_trade.PositionClosePartial(ticket, lots);
         }
         else
         {
            res = g_trade.PositionClose(ticket);
         }
      }
      else
      {
         res = g_trade.PositionClose(ticket);
      }
      uint retcode = g_trade.ResultRetcode();
      if(res || retcode == TRADE_RETCODE_DONE)
      {
         return StringFormat("{\"request_id\":\"%s\",\"success\":true,\"retcode\":%u,\"ticket\":\"%I64u\",\"timestamp_ns\":%I64d}",
                             reqId, retcode, ticket, nowNs);
      }
      else
      {
         return StringFormat("{\"request_id\":\"%s\",\"success\":false,\"retcode\":%u,\"error_msg\":\"%s\",\"timestamp_ns\":%I64d}",
                             reqId, retcode, g_trade.ResultRetcodeDescription(), nowNs);
      }
   }

   // --- MODIFY (SL/TP) ---
   if(action == "MODIFY")
   {
      ulong ticket = (ulong)StringToInteger(ticketStr);
      if(sl > 0) sl = NormalizeDouble(sl, digits);
      if(tp > 0) tp = NormalizeDouble(tp, digits);
      bool res = g_trade.PositionModify(ticket, sl, tp);
      uint retcode = g_trade.ResultRetcode();
      if(res || retcode == TRADE_RETCODE_DONE)
      {
         return StringFormat("{\"request_id\":\"%s\",\"success\":true,\"retcode\":%u,\"ticket\":\"%I64u\",\"timestamp_ns\":%I64d}",
                             reqId, retcode, ticket, nowNs);
      }
      else
      {
         return StringFormat("{\"request_id\":\"%s\",\"success\":false,\"retcode\":%u,\"error_msg\":\"%s\",\"timestamp_ns\":%I64d}",
                             reqId, retcode, g_trade.ResultRetcodeDescription(), nowNs);
      }
   }

   // --- POSITIONS ---
   if(action == "POSITIONS")
   {
      double reqMagic = ExtractJsonDouble(jsonReq, "magic_number");
      string posJson = "[";
      int total = PositionsTotal();
      bool first = true;
      for(int i = 0; i < total; i++)
      {
         ulong ticket = PositionGetTicket(i);
         if(ticket > 0)
         {
            long magic = PositionGetInteger(POSITION_MAGIC);
            if(reqMagic == 0 || magic == (long)reqMagic || magic == (long)InpMagicNumber || InpMagicNumber == 0)
            {
               string pSym = PositionGetString(POSITION_SYMBOL);
               ENUM_POSITION_TYPE pType = (ENUM_POSITION_TYPE)PositionGetInteger(POSITION_TYPE);
               string side = (pType == POSITION_TYPE_BUY) ? "BUY" : "SELL";
               double pLots = PositionGetDouble(POSITION_VOLUME);
               double pOpen = PositionGetDouble(POSITION_PRICE_OPEN);
               double pSL   = PositionGetDouble(POSITION_SL);
               double pTP   = PositionGetDouble(POSITION_TP);
               double pPnL  = PositionGetDouble(POSITION_PROFIT);
               long   pTime = PositionGetInteger(POSITION_TIME_MSC) * 1000000;
               if(pTime <= 0) pTime = PositionGetInteger(POSITION_TIME) * 1000000000;
               if(pTime <= 0) pTime = nowNs;
               int    pDig  = (int)SymbolInfoInteger(pSym, SYMBOL_DIGITS);

               if(!first) posJson += ",";
               posJson += StringFormat("{\"ticket\":\"%I64u\",\"symbol\":\"%s\",\"side\":\"%s\",\"lots\":%.2f,\"open_price\":%s,\"stop_loss\":%s,\"take_profit\":%s,\"profit\":%.2f,\"open_time_ns\":%I64d}",
                                       ticket, pSym, side, pLots, DoubleToString(pOpen, pDig), DoubleToString(pSL, pDig), DoubleToString(pTP, pDig), pPnL, pTime);
               first = false;
            }
         }
      }
      posJson += "]";

      return StringFormat("{\"request_id\":\"%s\",\"success\":true,\"positions\":%s,\"timestamp_ns\":%I64d}", reqId, posJson, nowNs);
   }

   // --- ACCOUNT ---
   if(action == "ACCOUNT")
   {
      double balance = AccountInfoDouble(ACCOUNT_BALANCE);
      double equity  = AccountInfoDouble(ACCOUNT_EQUITY);
      double margin  = AccountInfoDouble(ACCOUNT_MARGIN);
      double freeMgn = AccountInfoDouble(ACCOUNT_MARGIN_FREE);
      double level   = AccountInfoDouble(ACCOUNT_MARGIN_LEVEL);
      long   lev     = AccountInfoInteger(ACCOUNT_LEVERAGE);
      string curr    = AccountInfoString(ACCOUNT_CURRENCY);

      string accJson = StringFormat("{\"balance\":%.2f,\"equity\":%.2f,\"margin\":%.2f,\"free_margin\":%.2f,\"margin_level\":%.2f,\"leverage\":%d,\"currency\":\"%s\"}",
                                    balance, equity, margin, freeMgn, level, (int)lev, curr);

      return StringFormat("{\"request_id\":\"%s\",\"success\":true,\"account\":%s,\"timestamp_ns\":%I64d}", reqId, accJson, nowNs);
   }

   // --- HISTORY ---
   if(action == "HISTORY")
   {
      double days = ExtractJsonDouble(jsonReq, "days");
      if(days <= 0) days = 7.0; // Default past 7 days
      datetime fromTime = (datetime)(TimeCurrent() - (days * 86400));
      datetime toTime   = TimeCurrent() + 60;

      HistorySelect(fromTime, toTime);
      int totalDeals = HistoryDealsTotal();

      string dealsJson = "[";
      bool first = true;
      for(int i = 0; i < totalDeals; i++)
      {
         ulong dealTicket = HistoryDealGetTicket(i);
         if(dealTicket > 0)
         {
            ENUM_DEAL_ENTRY entryType = (ENUM_DEAL_ENTRY)HistoryDealGetInteger(dealTicket, DEAL_ENTRY);
            // Only deal OUT (close of position) represents a completed trade with realized profit!
            if(entryType == DEAL_ENTRY_OUT || entryType == DEAL_ENTRY_INOUT || entryType == DEAL_ENTRY_OUT_BY)
            {
               long magic = HistoryDealGetInteger(dealTicket, DEAL_MAGIC);
               if(magic == (long)InpMagicNumber || InpMagicNumber == 0)
               {
                  string dSym = HistoryDealGetString(dealTicket, DEAL_SYMBOL);
                  ENUM_DEAL_TYPE dType = (ENUM_DEAL_TYPE)HistoryDealGetInteger(dealTicket, DEAL_TYPE);
                  // If closing deal was SELL, the opened position was BUY. If closing deal was BUY, opened position was SELL.
                  string side = (dType == DEAL_TYPE_SELL) ? "BUY" : "SELL";
                  double dLots   = HistoryDealGetDouble(dealTicket, DEAL_VOLUME);
                  double dPrice  = HistoryDealGetDouble(dealTicket, DEAL_PRICE); // Exit Price
                  double dProfit = HistoryDealGetDouble(dealTicket, DEAL_PROFIT);
                  long   dTime   = HistoryDealGetInteger(dealTicket, DEAL_TIME_MSC) * 1000000;
                  if(dTime <= 0) dTime = HistoryDealGetInteger(dealTicket, DEAL_TIME) * 1000000000;
                  ulong  posId   = (ulong)HistoryDealGetInteger(dealTicket, DEAL_POSITION_ID);
                  int    dDig    = (int)SymbolInfoInteger(dSym, SYMBOL_DIGITS);

                  // Find Entry Price from matching DEAL_ENTRY_IN on the same position ID
                  double openPrice = dPrice;
                  int openTimeSec = (int)HistoryDealGetInteger(dealTicket, DEAL_TIME);
                  for(int j = 0; j < totalDeals; j++)
                  {
                     ulong inTicket = HistoryDealGetTicket(j);
                     if(inTicket > 0 && (ulong)HistoryDealGetInteger(inTicket, DEAL_POSITION_ID) == posId)
                     {
                        if((ENUM_DEAL_ENTRY)HistoryDealGetInteger(inTicket, DEAL_ENTRY) == DEAL_ENTRY_IN)
                        {
                           openPrice = HistoryDealGetDouble(inTicket, DEAL_PRICE);
                           openTimeSec = (int)HistoryDealGetInteger(inTicket, DEAL_TIME);
                           break;
                        }
                     }
                  }

                  long closeTimeSec = HistoryDealGetInteger(dealTicket, DEAL_TIME);
                  long durationSec = closeTimeSec - openTimeSec;
                  if(durationSec < 0) durationSec = 0;

                  if(!first) dealsJson += ",";
                  dealsJson += StringFormat("{\"ticket\":\"%I64u\",\"symbol\":\"%s\",\"side\":\"%s\",\"lots\":%.2f,\"entry\":%s,\"exit\":%s,\"net_pnl\":%.2f,\"duration_s\":%I64d,\"close_time\":%I64d}",
                                            posId > 0 ? posId : dealTicket, dSym, side, dLots,
                                            DoubleToString(openPrice, dDig), DoubleToString(dPrice, dDig),
                                            dProfit, durationSec, closeTimeSec);
                  first = false;
               }
            }
         }
      }
      dealsJson += "]";
      return StringFormat("{\"request_id\":\"%s\",\"success\":true,\"history\":%s,\"timestamp_ns\":%I64d}", reqId, dealsJson, nowNs);
   }

   // --- CANDLES / RATES ---
   if(action == "CANDLES" || action == "RATES")
   {
      string reqTf = ExtractJsonString(jsonReq, "timeframe");
      double countDbl = ExtractJsonDouble(jsonReq, "count");
      int count = (int)countDbl;
      if(count <= 0) count = 100;
      if(count > 300) count = 300;

      ENUM_TIMEFRAMES tf = PERIOD_M5;
      if(reqTf == "M1") tf = PERIOD_M1;
      else if(reqTf == "M5") tf = PERIOD_M5;
      else if(reqTf == "M15") tf = PERIOD_M15;
      else if(reqTf == "H1") tf = PERIOD_H1;

      MqlRates rates[];
      ArraySetAsSeries(rates, false); // 0 is oldest, count-1 is newest
      int copied = CopyRates(symbol, tf, 0, count, rates);
      if(copied <= 0)
      {
         return StringFormat("{\"request_id\":\"%s\",\"success\":false,\"error_msg\":\"Failed to copy rates for %s\",\"timestamp_ns\":%I64d}",
                             reqId, symbol, nowNs);
      }

      string candlesJson = "[";
      for(int i = 0; i < copied; i++)
      {
         if(i > 0) candlesJson += ",";
         candlesJson += StringFormat("{\"time\":%I64d,\"open\":%s,\"high\":%s,\"low\":%s,\"close\":%s,\"volume\":%I64d}",
                                     (long)rates[i].time,
                                     DoubleToString(rates[i].open, digits),
                                     DoubleToString(rates[i].high, digits),
                                     DoubleToString(rates[i].low, digits),
                                     DoubleToString(rates[i].close, digits),
                                     (long)rates[i].tick_volume);
      }
      candlesJson += "]";

      return StringFormat("{\"request_id\":\"%s\",\"success\":true,\"symbol\":\"%s\",\"timeframe\":\"%s\",\"count\":%d,\"candles\":%s,\"timestamp_ns\":%I64d}",
                          reqId, symbol, reqTf, copied, candlesJson, nowNs);
   }

   if(StringLen(action) == 0)
      return "";

   return StringFormat("{\"request_id\":\"%s\",\"success\":false,\"error_msg\":\"unknown action '%s'\",\"timestamp_ns\":%I64d}",
                       reqId, action, nowNs);
}

string g_cmdBuffer = "";

//+------------------------------------------------------------------+
//| Timer handler: maintain connection & process trade commands      |
//+------------------------------------------------------------------+
void OnTimer()
{
   EnsureConnections();

   // 1. Process incoming commands
   if(g_cmdSocket != INVALID_HANDLE && SocketIsConnected(g_cmdSocket))
   {
      uint readable = SocketIsReadable(g_cmdSocket);
      if(readable > 0)
      {
         uchar buffer[];
         ArrayResize(buffer, (int)readable);
         int received = SocketRead(g_cmdSocket, buffer, (int)readable, 10);
         if(received > 0)
         {
            g_cmdBuffer += CharArrayToString(buffer, 0, received);
            int nlPos = StringFind(g_cmdBuffer, "\n");
            while(nlPos >= 0)
            {
               string line = StringSubstr(g_cmdBuffer, 0, nlPos);
               g_cmdBuffer = StringSubstr(g_cmdBuffer, nlPos + 1);
               
               StringReplace(line, "\r", "");
               StringReplace(line, "\t", "");
               StringTrimLeft(line);
               StringTrimRight(line);
               if(StringLen(line) > 0 && StringFind(line, "{") >= 0)
               {
                  string resp = HandleSingleCommand(line);
                  if(StringLen(resp) > 0)
                  {
                     resp += "\n";
                     int respLen = StringLen(resp);
                     uchar respData[];
                     StringToCharArray(resp, respData, 0, respLen);
                     SocketSend(g_cmdSocket, respData, respLen);
                  }
               }
               
               nlPos = StringFind(g_cmdBuffer, "\n");
            }
         }
      }
   }

   // 2. Enforce StopLoss and TakeProfit on any open positions missing stops
   EnforceStopsOnOpenPositions();

   // 3. Continuous multi-symbol tick push to Go stream
   StreamAllConfiguredTicks();
}
//+------------------------------------------------------------------+
