package exchange

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/vitos/crypto_trade_level/internal/domain"
)

const (
	BybitBaseURL = "https://api.bybit.com"
	BybitWSURL   = "wss://stream.bybit.com/v5/public/linear"
)

type BybitAdapter struct {
	apiKey         string
	apiSecret      string
	baseURL        string
	wsURL          string
	client         *http.Client
	wsConn         *websocket.Conn
	wsDone         chan struct{}
	callbacks      []func(symbol string, price float64)
	tradeCallbacks []func(symbol string, side string, size float64, price float64)
	mu             sync.Mutex
}

func NewBybitAdapter(apiKey, apiSecret, baseURL, wsURL string) *BybitAdapter {
	return &BybitAdapter{
		apiKey:    apiKey,
		apiSecret: apiSecret,
		baseURL:   baseURL,
		wsURL:     wsURL,
		client:    &http.Client{Timeout: 10 * time.Second},
		wsDone:    make(chan struct{}),
	}
}

// --- REST API ---

func (b *BybitAdapter) sign(params string, timestamp int64, recvWindow int) string {
	// timestamp + apiKey + recvWindow + params
	toSign := fmt.Sprintf("%d%s%d%s", timestamp, b.apiKey, recvWindow, params)
	h := hmac.New(sha256.New, []byte(b.apiSecret))
	h.Write([]byte(toSign))
	return hex.EncodeToString(h.Sum(nil))
}

func (b *BybitAdapter) sendRequest(ctx context.Context, method, path string, payload map[string]interface{}) ([]byte, error) {
	timestamp := time.Now().UnixMilli()
	recvWindow := 5000

	var body []byte
	var paramsStr string

	if payload != nil {
		jsonBody, _ := json.Marshal(payload)
		body = jsonBody
		paramsStr = string(jsonBody)
	} else if method == "GET" {
		// For GET, params are in the query string
		// Extract query params from path
		if idx := strings.Index(path, "?"); idx != -1 {
			paramsStr = path[idx+1:]
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, b.baseURL+path, bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}

	signature := b.sign(paramsStr, timestamp, recvWindow)

	req.Header.Set("X-BAPI-API-KEY", b.apiKey)
	req.Header.Set("X-BAPI-TIMESTAMP", strconv.FormatInt(timestamp, 10))
	req.Header.Set("X-BAPI-SIGN", signature)
	req.Header.Set("X-BAPI-RECV-WINDOW", strconv.Itoa(recvWindow))
	req.Header.Set("Content-Type", "application/json")

	resp, err := b.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API error: %s", string(respBody))
	}

	return respBody, nil
}

func (b *BybitAdapter) GetCurrentPrice(ctx context.Context, symbol string) (float64, error) {
	// For MVP, we might not use REST for price if we have WS.
	// But implementing it for initial fetch or fallback.
	// V5 Ticker
	path := "/v5/market/tickers?category=linear&symbol=" + symbol
	resp, err := http.Get(b.baseURL + path)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var result struct {
		RetCode int `json:"retCode"`
		Result  struct {
			List []struct {
				LastPrice string `json:"lastPrice"`
			} `json:"list"`
		} `json:"result"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return 0, err
	}

	if len(result.Result.List) == 0 {
		return 0, fmt.Errorf("symbol not found")
	}

	return strconv.ParseFloat(result.Result.List[0].LastPrice, 64)
}

func (b *BybitAdapter) placeOrder(ctx context.Context, symbol string, side string, size float64, leverage int, marginType string, stopLoss float64) error {
	// 1. Set Margin Mode (isolated/cross)
	b.setMarginMode(ctx, symbol, marginType)

	// 2. Set Leverage
	b.setLeverage(ctx, symbol, leverage)

	// 3. Place Order
	payload := map[string]interface{}{
		"category":    "linear",
		"symbol":      symbol,
		"side":        side,
		"orderType":   "Market",
		"qty":         fmt.Sprintf("%f", size),
		"timeInForce": "GTC",
	}

	// Add Stop Loss if provided
	if stopLoss > 0 {
		payload["stopLoss"] = fmt.Sprintf("%f", stopLoss)
	}

	resp, err := b.sendRequest(ctx, "POST", "/v5/order/create", payload)
	if err != nil {
		return err
	}

	// Check retCode in response
	var result struct {
		RetCode int    `json:"retCode"`
		RetMsg  string `json:"retMsg"`
	}
	json.Unmarshal(resp, &result)
	if result.RetCode != 0 {
		return fmt.Errorf("bybit order error: %s", result.RetMsg)
	}

	return nil
}

func (b *BybitAdapter) setLeverage(ctx context.Context, symbol string, leverage int) {
	payload := map[string]interface{}{
		"category":     "linear",
		"symbol":       symbol,
		"buyLeverage":  fmt.Sprintf("%d", leverage),
		"sellLeverage": fmt.Sprintf("%d", leverage),
	}
	// This often fails if leverage is already set, so we just log and ignore
	_, _ = b.sendRequest(ctx, "POST", "/v5/position/set-leverage", payload)
}

func (b *BybitAdapter) setMarginMode(ctx context.Context, symbol string, marginMode string) {
	// For Bybit V5: 0 = cross margin, 1 = isolated margin
	mode := 0 // cross
	if marginMode == "isolated" {
		mode = 1
	}

	log.Printf("DEBUG: Setting margin mode for %s: %s (mode=%d)", symbol, marginMode, mode)

	// Use the correct V5 endpoint for switching isolated margin
	// This endpoint sets the margin mode for a specific symbol
	payload := map[string]interface{}{
		"category":     "linear",
		"symbol":       symbol,
		"tradeMode":    mode, // 0: cross margin, 1: isolated margin
		"buyLeverage":  "10", // Required by API
		"sellLeverage": "10", // Required by API
	}

	resp, err := b.sendRequest(ctx, "POST", "/v5/position/switch-mode", payload)
	if err != nil {
		log.Printf("WARNING: Failed to set margin mode for %s: %v", symbol, err)
		log.Printf("DEBUG: This might be because the mode is already set or position already exists")
	} else {
		log.Printf("DEBUG: Margin mode API response for %s: %s", symbol, string(resp))
	}
}

func (b *BybitAdapter) MarketBuy(ctx context.Context, symbol string, size float64, leverage int, marginType string, stopLoss float64) error {
	return b.placeOrder(ctx, symbol, "Buy", size, leverage, marginType, stopLoss)
}

func (b *BybitAdapter) MarketSell(ctx context.Context, symbol string, size float64, leverage int, marginType string, stopLoss float64) error {
	return b.placeOrder(ctx, symbol, "Sell", size, leverage, marginType, stopLoss)
}

func (b *BybitAdapter) ClosePosition(ctx context.Context, symbol string) error {
	// Get position to know size and side
	pos, err := b.GetPosition(ctx, symbol)
	if err != nil {
		return err
	}
	if pos.Size == 0 {
		return nil
	}

	closeSide := "Sell"
	if pos.Side == domain.SideShort {
		closeSide = "Buy"
	}

	payload := map[string]interface{}{
		"category":   "linear",
		"symbol":     symbol,
		"side":       closeSide,
		"orderType":  "Market",
		"qty":        fmt.Sprintf("%f", pos.Size),
		"reduceOnly": true,
	}

	resp, err := b.sendRequest(ctx, "POST", "/v5/order/create", payload)
	if err != nil {
		return err
	}

	var result struct {
		RetCode int    `json:"retCode"`
		RetMsg  string `json:"retMsg"`
	}
	json.Unmarshal(resp, &result)
	if result.RetCode != 0 {
		return fmt.Errorf("bybit close error: %s", result.RetMsg)
	}
	return nil
}

func (b *BybitAdapter) GetPosition(ctx context.Context, symbol string) (*domain.Position, error) {
	path := "/v5/position/list?category=linear&symbol=" + symbol
	resp, err := b.sendRequest(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}

	var result struct {
		RetCode int `json:"retCode"`
		Result  struct {
			List []struct {
				Symbol        string `json:"symbol"`
				Side          string `json:"side"`
				Size          string `json:"size"`
				AvgPrice      string `json:"avgPrice"`
				MarkPrice     string `json:"markPrice"`
				UnrealisedPnl string `json:"unrealisedPnl"`
				Leverage      string `json:"leverage"`
				TradeMode     int    `json:"tradeMode"` // 0: cross margin, 1: isolated margin
			} `json:"list"`
		} `json:"result"`
	}

	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, err
	}

	// Debug: Print raw response if empty
	if len(result.Result.List) == 0 {
		fmt.Printf("DEBUG: Position List Empty. Raw: %s\n", string(resp))
		return &domain.Position{Symbol: symbol}, nil
	}

	raw := result.Result.List[0]
	size, _ := strconv.ParseFloat(raw.Size, 64)
	entry, _ := strconv.ParseFloat(raw.AvgPrice, 64)
	curr, _ := strconv.ParseFloat(raw.MarkPrice, 64)
	pnl, _ := strconv.ParseFloat(raw.UnrealisedPnl, 64)
	lev, _ := strconv.Atoi(raw.Leverage)

	side := domain.SideLong
	if raw.Side == "Sell" {
		side = domain.SideShort
	}

	// Convert tradeMode to margin type string
	marginType := "cross"
	if raw.TradeMode == 1 {
		marginType = "isolated"
	}

	return &domain.Position{
		Exchange:      "bybit",
		Symbol:        raw.Symbol,
		Side:          side,
		Size:          size,
		EntryPrice:    entry,
		CurrentPrice:  curr,
		UnrealizedPnL: pnl,
		Leverage:      lev,
		MarginType:    marginType,
	}, nil
}

// --- WebSocket ---

func (b *BybitAdapter) OnPriceUpdate(callback func(symbol string, price float64)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.callbacks = append(b.callbacks, callback)
}

func (b *BybitAdapter) OnTradeUpdate(callback func(symbol string, side string, size float64, price float64)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.tradeCallbacks = append(b.tradeCallbacks, callback)
}

func (b *BybitAdapter) ConnectWS(symbols []string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.wsConn != nil {
		// Already connected, just subscribe
		return b.subscribe(symbols)
	}

	c, _, err := websocket.DefaultDialer.Dial(b.wsURL, nil)
	if err != nil {
		return err
	}
	b.wsConn = c

	go b.readLoop()

	return b.subscribe(symbols)
}

func (b *BybitAdapter) Subscribe(symbols []string) error {
	b.mu.Lock()
	if b.wsConn == nil {
		b.mu.Unlock()
		// Not connected yet, ConnectWS will handle it
		return b.ConnectWS(symbols)
	}
	defer b.mu.Unlock()
	return b.subscribe(symbols)
}

func (b *BybitAdapter) subscribe(symbols []string) error {
	if len(symbols) == 0 {
		return nil
	}
	args := make([]interface{}, len(symbols))
	for i, s := range symbols {
		args[i] = "orderbook.1." + s
	}
	// Also subscribe to publicTrade
	tradeArgs := make([]interface{}, len(symbols))
	for i, s := range symbols {
		tradeArgs[i] = "publicTrade." + s
	}
	args = append(args, tradeArgs...)

	subMsg := map[string]interface{}{
		"op":   "subscribe",
		"args": args,
	}
	if err := b.wsConn.WriteJSON(subMsg); err != nil {
		return err
	}
	return nil
}

func (b *BybitAdapter) readLoop() {
	defer func() {
		b.wsConn.Close()
		b.mu.Lock()
		b.wsConn = nil
		b.mu.Unlock()
	}()

	for {
		_, message, err := b.wsConn.ReadMessage()
		if err != nil {
			log.Println("WS Read error:", err)
			close(b.wsDone)
			return
		}

		// log.Printf("WS Received: %s", string(message)) // Very verbose, maybe just topic?

		var event map[string]interface{}
		if err := json.Unmarshal(message, &event); err != nil {
			log.Println("WS Unmarshal error:", err)
			continue
		}

		topic, ok := event["topic"].(string)
		if !ok {
			// log.Println("WS No topic in message")
			continue
		}

		if strings.HasPrefix(topic, "orderbook.1.") {
			data, ok := event["data"].(map[string]interface{})
			if !ok {
				continue
			}

			symbol := strings.TrimPrefix(topic, "orderbook.1.")

			// Parse Ask
			a, ok := data["a"].([]interface{})
			if !ok || len(a) == 0 {
				continue
			}
			askEntry, ok := a[0].([]interface{})
			if !ok || len(askEntry) < 1 {
				continue
			}
			askStr, ok := askEntry[0].(string)
			if !ok {
				continue
			}
			ask, _ := strconv.ParseFloat(askStr, 64)

			// Parse Bid
			bidList, ok := data["b"].([]interface{})
			if !ok || len(bidList) == 0 {
				continue
			}
			bidEntry, ok := bidList[0].([]interface{})
			if !ok || len(bidEntry) < 1 {
				continue
			}
			bidStr, ok := bidEntry[0].(string)
			if !ok {
				continue
			}
			bid, _ := strconv.ParseFloat(bidStr, 64)

			// Use mid price
			price := (ask + bid) / 2

			b.mu.Lock()
			callbacks := make([]func(string, float64), len(b.callbacks))
			copy(callbacks, b.callbacks)
			b.mu.Unlock()

			for _, cb := range callbacks {
				cb(symbol, price)
			}
		} else if strings.HasPrefix(topic, "publicTrade.") {
			data, ok := event["data"].([]interface{})
			if !ok {
				continue
			}
			symbol := strings.TrimPrefix(topic, "publicTrade.")

			for _, item := range data {
				trade, ok := item.(map[string]interface{})
				if !ok {
					continue
				}

				// Parse Trade
				side, _ := trade["S"].(string)
				sizeStr, _ := trade["v"].(string)
				priceStr, _ := trade["p"].(string)

				size, _ := strconv.ParseFloat(sizeStr, 64)
				price, _ := strconv.ParseFloat(priceStr, 64)

				b.mu.Lock()
				tradeCallbacks := make([]func(string, string, float64, float64), len(b.tradeCallbacks))
				copy(tradeCallbacks, b.tradeCallbacks)
				b.mu.Unlock()

				for _, cb := range tradeCallbacks {
					cb(symbol, side, size, price)
				}
			}
		}
	}
}

func (b *BybitAdapter) GetCandles(ctx context.Context, symbol, interval string, limit int) ([]domain.Candle, error) {
	// V5 Kline Endpoint
	path := fmt.Sprintf("/v5/market/kline?category=linear&symbol=%s&interval=%s&limit=%d", symbol, interval, limit)
	resp, err := b.sendRequest(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}

	var result struct {
		RetCode int `json:"retCode"`
		Result  struct {
			List [][]string `json:"list"`
		} `json:"result"`
	}

	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, err
	}

	if result.RetCode != 0 {
		return nil, fmt.Errorf("bybit kline error: %d", result.RetCode)
	}

	var candles []domain.Candle
	for _, raw := range result.Result.List {
		// Format: [startTime, open, high, low, close, volume, turnover]
		if len(raw) < 6 {
			continue
		}

		ts, _ := strconv.ParseInt(raw[0], 10, 64)
		open, _ := strconv.ParseFloat(raw[1], 64)
		high, _ := strconv.ParseFloat(raw[2], 64)
		low, _ := strconv.ParseFloat(raw[3], 64)
		closePrice, _ := strconv.ParseFloat(raw[4], 64)
		volume, _ := strconv.ParseFloat(raw[5], 64)

		// Bybit returns candles in reverse chronological order (newest first)
		// We usually want oldest first for charts, but let's check what lightweight-charts expects.
		// Lightweight charts expects ascending order (oldest first).
		// So we should prepend or reverse the list.
		// Let's append and then reverse at the end.

		candles = append(candles, domain.Candle{
			Time:   ts / 1000, // Convert ms to seconds for lightweight-charts
			Open:   open,
			High:   high,
			Low:    low,
			Close:  closePrice,
			Volume: volume,
		})
	}

	// Reverse candles to be chronological (Oldest -> Newest)
	for i, j := 0, len(candles)-1; i < j; i, j = i+1, j-1 {
		candles[i], candles[j] = candles[j], candles[i]
	}

	return candles, nil
}

func (b *BybitAdapter) GetRecentTrades(ctx context.Context, symbol string, limit int) ([]domain.PublicTrade, error) {
	params := map[string]interface{}{
		"category": "linear",
		"symbol":   symbol,
		"limit":    limit,
	}

	resp, err := b.sendRequest(ctx, "GET", "/v5/market/recent-trade", params)
	if err != nil {
		return nil, err
	}

	var result struct {
		RetCode int    `json:"retCode"`
		RetMsg  string `json:"retMsg"`
		Result  struct {
			List []struct {
				Symbol string `json:"symbol"`
				Side   string `json:"side"`
				Size   string `json:"size"`
				Price  string `json:"price"`
				Time   string `json:"time"`
			} `json:"list"`
		} `json:"result"`
	}

	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, err
	}

	if result.RetCode != 0 {
		return nil, fmt.Errorf("bybit api error: %s", result.RetMsg)
	}

	var trades []domain.PublicTrade
	for _, t := range result.Result.List {
		price, _ := strconv.ParseFloat(t.Price, 64)
		size, _ := strconv.ParseFloat(t.Size, 64)
		timeMs, _ := strconv.ParseInt(t.Time, 10, 64)

		trades = append(trades, domain.PublicTrade{
			Symbol: t.Symbol,
			Side:   t.Side,
			Size:   size,
			Price:  price,
			Time:   timeMs,
		})
	}

	return trades, nil
}

func (b *BybitAdapter) GetOrderBook(ctx context.Context, symbol string, category string) (*domain.OrderBook, error) {
	// category: "linear" (futures) or "spot"
	if category == "" {
		category = "linear"
	}

	limit := 50
	if category == "linear" {
		limit = 500
	}

	path := fmt.Sprintf("/v5/market/orderbook?category=%s&symbol=%s&limit=%d", category, symbol, limit)
	resp, err := b.sendRequest(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}

	var result struct {
		RetCode int `json:"retCode"`
		Result  struct {
			S string     `json:"s"`
			B [][]string `json:"b"`
			A [][]string `json:"a"`
		} `json:"result"`
	}

	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, err
	}

	if result.RetCode != 0 {
		return nil, fmt.Errorf("bybit orderbook error: %d", result.RetCode)
	}

	ob := &domain.OrderBook{
		Symbol: result.Result.S,
		Bids:   make([]domain.OrderBookEntry, 0, len(result.Result.B)),
		Asks:   make([]domain.OrderBookEntry, 0, len(result.Result.A)),
	}

	for _, bid := range result.Result.B {
		if len(bid) < 2 {
			continue
		}
		price, _ := strconv.ParseFloat(bid[0], 64)
		size, _ := strconv.ParseFloat(bid[1], 64)
		ob.Bids = append(ob.Bids, domain.OrderBookEntry{Price: price, Size: size})
	}

	for _, ask := range result.Result.A {
		if len(ask) < 2 {
			continue
		}
		price, _ := strconv.ParseFloat(ask[0], 64)
		size, _ := strconv.ParseFloat(ask[1], 64)
		ob.Asks = append(ob.Asks, domain.OrderBookEntry{Price: price, Size: size})
	}

	return ob, nil
}

func (b *BybitAdapter) GetInstruments(ctx context.Context, category string) ([]domain.Instrument, error) {
	if category == "" {
		category = "linear"
	}

	path := fmt.Sprintf("/v5/market/instruments-info?category=%s", category)
	resp, err := b.sendRequest(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}

	var result struct {
		RetCode int    `json:"retCode"`
		RetMsg  string `json:"retMsg"`
		Result  struct {
			List []struct {
				Symbol     string `json:"symbol"`
				BaseCoin   string `json:"baseCoin"`
				QuoteCoin  string `json:"quoteCoin"`
				Status     string `json:"status"`
				LaunchTime string `json:"launchTime"`
			} `json:"list"`
		} `json:"result"`
	}

	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, err
	}

	if result.RetCode != 0 {
		return nil, fmt.Errorf("bybit api error: %s", result.RetMsg)
	}

	var instruments []domain.Instrument
	for _, item := range result.Result.List {
		launchTime, _ := strconv.ParseInt(item.LaunchTime, 10, 64)
		instruments = append(instruments, domain.Instrument{
			Symbol:     item.Symbol,
			BaseCoin:   item.BaseCoin,
			QuoteCoin:  item.QuoteCoin,
			Status:     item.Status,
			LaunchTime: launchTime,
		})
	}

	return instruments, nil
}
