package usecase

import (
	"context"
	"fmt"
	"log"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/vitos/crypto_trade_level/internal/domain"
)

type CachedLiquidity struct {
	Data     []LiquidityCluster
	TotalBid float64
	TotalAsk float64
	Expiry   time.Time
}

type Trade struct {
	Symbol string
	Side   string
	Size   float64
	Price  float64
	Time   time.Time
}

type PricePoint struct {
	Price float64
	Time  time.Time
}

type MarketService struct {
	exchange         domain.Exchange
	repo             domain.LevelRepository
	cache            map[string]CachedLiquidity
	trades           map[string][]Trade // Symbol -> Trades
	depthHistory     map[string][]DepthSnapshot
	priceHistory     map[string][]PricePoint // Symbol -> Price Points
	cvdAccumulator   map[string]float64      // Symbol -> Cumulative Volume Delta
	liquidityHistory map[string][]domain.LiquiditySnapshot
	subscribed       map[string]bool // Symbol -> Subscribed
	mu               sync.Mutex
	timeNow          func() time.Time // For testing
}

type DepthSnapshot struct {
	Time     time.Time
	TotalBid float64
	TotalAsk float64
}

const MaxGLI = 10.0

func NewMarketService(exchange domain.Exchange, repo domain.LevelRepository) *MarketService {
	s := &MarketService{
		exchange:         exchange,
		repo:             repo,
		cache:            make(map[string]CachedLiquidity),
		trades:           make(map[string][]Trade),
		depthHistory:     make(map[string][]DepthSnapshot),
		priceHistory:     make(map[string][]PricePoint),
		cvdAccumulator:   make(map[string]float64),
		liquidityHistory: make(map[string][]domain.LiquiditySnapshot),
		subscribed:       make(map[string]bool),
		timeNow:          time.Now,
	}

	// Subscribe to trades
	exchange.OnTradeUpdate(s.handleTrade)

	s.startHealthCheck()

	return s
}

func (s *MarketService) startHealthCheck() {
	ticker := time.NewTicker(30 * time.Second)
	go func() {
		for range ticker.C {
			status := s.exchange.GetWSStatus()
			if !status.Connected {
				log.Println("MarketService: WS disconnected, attempting reconnect...")
				s.mu.Lock()
				symbols := make([]string, 0, len(s.subscribed))
				for sym := range s.subscribed {
					symbols = append(symbols, sym)
				}
				s.mu.Unlock()

				if len(symbols) > 0 {
					if err := s.exchange.Subscribe(symbols); err != nil {
						log.Printf("MarketService: Reconnect failed: %v", err)
					} else {
						log.Println("MarketService: Reconnect successful")
					}
				}
			}
		}
	}()
}

func (s *MarketService) handleTrade(symbol, side string, size, price float64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.timeNow()
	// Add new trade
	s.trades[symbol] = append(s.trades[symbol], Trade{
		Symbol: symbol,
		Side:   side,
		Size:   size,
		Price:  price,
		Time:   now,
	})

	// Update Cumulative Volume Delta (CVD)
	// We no longer accumulate indefinitely. CVD is calculated on the fly for the window.
	// Kept comment for reference if we want to revert to lifetime accumulation.

	// Track price point
	s.priceHistory[symbol] = append(s.priceHistory[symbol], PricePoint{
		Price: price,
		Time:  now,
	})

	// Prune old trades and prices (> 60s)
	cutoff := now.Add(-60 * time.Second)
	validTrades := s.trades[symbol][:0]
	for _, t := range s.trades[symbol] {
		if t.Time.After(cutoff) {
			validTrades = append(validTrades, t)
		}
	}
	s.trades[symbol] = validTrades

	// Prune old prices
	validPrices := s.priceHistory[symbol][:0]
	for _, p := range s.priceHistory[symbol] {
		if p.Time.After(cutoff) {
			validPrices = append(validPrices, p)
		}
	}
	s.priceHistory[symbol] = validPrices
}

type MarketStats struct {
	SpeedBuy           float64         `json:"speed_buy"`
	SpeedSell          float64         `json:"speed_sell"`
	SpeedBuy30s        float64         `json:"speed_buy_30s"`
	SpeedSell30s       float64         `json:"speed_sell_30s"`
	SpeedBuy10s        float64         `json:"speed_buy_10s"`
	SpeedSell10s       float64         `json:"speed_sell_10s"`
	DepthBid           float64         `json:"depth_bid"`
	DepthAsk           float64         `json:"depth_ask"`
	PriceChange60s     float64         `json:"price_change_60s"`
	PriceChange30s     float64         `json:"price_change_30s"`
	PriceChange10s     float64         `json:"price_change_10s"`
	OBI                float64         `json:"obi"`
	CVD                float64         `json:"cvd"`
	TSI                float64         `json:"tsi"`
	GLI                float64         `json:"gli"`
	TradeVelocity      float64         `json:"trade_velocity"`
	ConclusionScore    float64         `json:"conclusion_score"`
	ConclusionScore30s float64         `json:"conclusion_score_30s"`
	ConclusionScore10s float64         `json:"conclusion_score_10s"`
	LastPrice          float64         `json:"last_price"`
	RSI                float64         `json:"rsi"`
	MACD               float64         `json:"macd"`
	MACDSignal         float64         `json:"macd_signal"`
	MACDHist           float64         `json:"macd_hist"`
	WSStatus           domain.WSStatus `json:"ws_status"`
}

func (s *MarketService) GetMarketStats(ctx context.Context, symbol string) (*MarketStats, error) {
	s.mu.Lock()
	needsSubscribe := !s.subscribed[symbol]
	s.mu.Unlock()

	if needsSubscribe {
		// Subscribe to real-time updates
		if err := s.exchange.Subscribe([]string{symbol}); err == nil {
			s.mu.Lock()
			s.subscribed[symbol] = true
			s.mu.Unlock()
		} else {
			log.Printf("Error subscribing to %s: %v", symbol, err)
		}
	}

	s.mu.Lock()

	// Check if we need to hydrate/refresh trades
	// Refresh if: 1) No trades at all, OR 2) No trades in last 60 seconds
	needsRefresh := len(s.trades[symbol]) == 0
	if !needsRefresh {
		// Check if we have any trades in the last 60 seconds
		cutoff := s.timeNow().Add(-60 * time.Second)
		hasRecentTrades := false
		for _, t := range s.trades[symbol] {
			if t.Time.After(cutoff) {
				hasRecentTrades = true
				break
			}
		}
		needsRefresh = !hasRecentTrades
	}

	if needsRefresh {
		s.mu.Unlock() // Release lock for network call

		recentTrades, err := s.exchange.GetRecentTrades(ctx, symbol, 1000)

		s.mu.Lock() // Re-acquire lock
		if err == nil {
			// Double-check if we still need to refresh (another goroutine might have done it)
			shouldUpdate := true
			if len(s.trades[symbol]) > 0 {
				// Check timestamp of latest trade
				lastTradeTime := s.trades[symbol][len(s.trades[symbol])-1].Time
				if s.timeNow().Sub(lastTradeTime) < 60*time.Second {
					shouldUpdate = false
				}
			}

			if shouldUpdate {
				// Replace old trades with fresh ones
				s.trades[symbol] = nil
				s.priceHistory[symbol] = nil
				// iterate in reverse (oldest first) because GetRecentTrades returns newest first
				for i := len(recentTrades) - 1; i >= 0; i-- {
					t := recentTrades[i]
					tradeTime := time.UnixMilli(t.Time)
					s.trades[symbol] = append(s.trades[symbol], Trade{
						Symbol: t.Symbol,
						Side:   t.Side,
						Size:   t.Size,
						Price:  t.Price,
						Time:   tradeTime,
					})
					s.priceHistory[symbol] = append(s.priceHistory[symbol], PricePoint{
						Price: t.Price,
						Time:  tradeTime,
					})
				}
			}
		}
	}
	defer s.mu.Unlock()

	// 1. Calculate Speed (from trades)
	var speedBuy, speedSell float64
	var speedBuy30s, speedSell30s float64
	var speedBuy10s, speedSell10s float64
	var tradeCount int
	if trades, ok := s.trades[symbol]; ok {
		now := s.timeNow()
		cutoff60 := now.Add(-60 * time.Second)
		cutoff30 := now.Add(-30 * time.Second)
		cutoff10 := now.Add(-10 * time.Second)

		// Prune again just in case (lazy pruning)
		validTrades := trades[:0]
		for _, t := range trades {
			if t.Time.After(cutoff60) {
				validTrades = append(validTrades, t)
				tradeCount++
				if t.Side == "Buy" {
					speedBuy += t.Size * t.Price
				} else {
					speedSell += t.Size * t.Price
				}

				if t.Time.After(cutoff30) {
					if t.Side == "Buy" {
						speedBuy30s += t.Size * t.Price
					} else {
						speedSell30s += t.Size * t.Price
					}
				}

				if t.Time.After(cutoff10) {
					if t.Side == "Buy" {
						speedBuy10s += t.Size * t.Price
					} else {
						speedSell10s += t.Size * t.Price
					}
				}
			}
		}
		s.trades[symbol] = validTrades
	}

	// 2. Get Depth (Moving Average)
	// Check if we need to update depth history (lazy fetch if stale)
	s.updateOrderBook(ctx, symbol)

	var avgBid60, avgAsk60 float64
	var avgBid30, avgAsk30 float64
	var avgBid10, avgAsk10 float64

	if history, ok := s.depthHistory[symbol]; ok && len(history) > 0 {
		var sumBid60, sumAsk60 float64
		var sumBid30, sumAsk30 float64
		var count30 int
		var sumBid10, sumAsk10 float64
		var count10 int

		now := s.timeNow()
		cutoff30 := now.Add(-30 * time.Second)
		cutoff10 := now.Add(-10 * time.Second)

		for _, snapshot := range history {
			sumBid60 += snapshot.TotalBid
			sumAsk60 += snapshot.TotalAsk

			if snapshot.Time.After(cutoff30) {
				sumBid30 += snapshot.TotalBid
				sumAsk30 += snapshot.TotalAsk
				count30++
			}
			if snapshot.Time.After(cutoff10) {
				sumBid10 += snapshot.TotalBid
				sumAsk10 += snapshot.TotalAsk
				count10++
			}
		}
		avgBid60 = sumBid60 / float64(len(history))
		avgAsk60 = sumAsk60 / float64(len(history))

		if count30 > 0 {
			avgBid30 = sumBid30 / float64(count30)
			avgAsk30 = sumAsk30 / float64(count30)
		} else {
			avgBid30 = avgBid60
			avgAsk30 = avgAsk60
		}

		if count10 > 0 {
			avgBid10 = sumBid10 / float64(count10)
			avgAsk10 = sumAsk10 / float64(count10)
		} else {
			avgBid10 = avgBid60
			avgAsk10 = avgAsk60
		}
	}

	// 3. Calculate price change percentage
	var priceChange60s, priceChange30s, priceChange10s float64
	if prices, ok := s.priceHistory[symbol]; ok && len(prices) >= 2 {
		now := s.timeNow()
		cutoff30 := now.Add(-30 * time.Second)
		cutoff10 := now.Add(-10 * time.Second)

		// Get oldest and newest price in the 60s window
		oldestPrice60 := prices[0].Price
		newestPrice := prices[len(prices)-1].Price

		if oldestPrice60 > 0 {
			priceChange60s = ((newestPrice - oldestPrice60) / oldestPrice60) * 100
		}

		// Find oldest price in 30s window
		for _, p := range prices {
			if p.Time.After(cutoff30) {
				if p.Price > 0 {
					priceChange30s = ((newestPrice - p.Price) / p.Price) * 100
				}
				break
			}
		}

		// Find oldest price in 10s window
		for _, p := range prices {
			if p.Time.After(cutoff10) {
				if p.Price > 0 {
					priceChange10s = ((newestPrice - p.Price) / p.Price) * 100
				}
				break
			}
		}
	}

	// 4. Calculate Indicators
	// OBI = (BidDepth - AskDepth) / (BidDepth + AskDepth)
	var obi float64
	if avgBid60+avgAsk60 > 0 {
		obi = (avgBid60 - avgAsk60) / (avgBid60 + avgAsk60)
	}

	// CVD = Cumulative Volume Delta (Net Volume in 60s window)
	// Calculated as SpeedBuy - SpeedSell to match test expectations and avoid unbounded growth.
	cvd := speedBuy - speedSell

	// TSI = Trade Speed Index (Trades per Second)
	// NumberOfTrades / TimeWindow (60s)
	tsi := float64(tradeCount) / 60.0

	// GLI = ExecutedVolumeAtBid / ExecutedVolumeAtAsk
	// ExecutedVolumeAtBid: volume executed at bid price (sellers hitting bids) = speedSell
	// ExecutedVolumeAtAsk: volume executed at ask price (buyers lifting asks) = speedBuy
	// Therefore, GLI = speedSell / speedBuy
	var gli float64 = 1.0
	if speedBuy > 0 {
		gli = speedSell / speedBuy
	} else if speedSell > 0 {
		// MaxGLI caps the ratio when there is no buy volume to avoid infinity.
		// 10.0 is chosen as a reasonable upper bound for "extreme bearishness".
		gli = MaxGLI
	}

	// TradeVelocity = TotalVolume / TimeWindow (60s)
	tradeVelocity := (speedBuy + speedSell) / 60.0

	// 5. Calculate Conclusion Score (Market Sentiment)
	calcConclusion := func(buy, sell, bid, ask float64) float64 {
		var o, gScore, cScore float64
		if bid+ask > 0 {
			o = (bid - ask) / (bid + ask)
		}

		var g float64 = 1.0
		if buy > 0 {
			g = sell / buy
		} else if sell > 0 {
			g = MaxGLI
		}

		if g > 1 {
			gScore = -(g - 1)
			if gScore < -1 {
				gScore = -1
			}
		} else {
			gScore = 1 - g
		}

		c := buy - sell
		maxC := 10000.0
		cScore = c / maxC
		if cScore > 1 {
			cScore = 1
		} else if cScore < -1 {
			cScore = -1
		}

		return (o + gScore + cScore) / 3.0
	}

	conclusionScore := calcConclusion(speedBuy, speedSell, avgBid60, avgAsk60)
	conclusionScore30s := calcConclusion(speedBuy30s, speedSell30s, avgBid30, avgAsk30)
	conclusionScore10s := calcConclusion(speedBuy10s, speedSell10s, avgBid10, avgAsk10)

	// 6. Get Latest Price for UI display
	var lastPrice float64
	if prices, ok := s.priceHistory[symbol]; ok && len(prices) > 0 {
		lastPrice = prices[len(prices)-1].Price
	}

	if lastPrice == 0 {
		// Fallback to simpler ticker fetch to ensure we have a price for the UI
		if price, err := s.exchange.GetCurrentPrice(ctx, symbol); err == nil {
			lastPrice = price
		}
	}

	// 7. RSI (1m)
	rsi, _ := s.GetRSI(ctx, symbol, "1", 14)

	// 8. MACD (1m)
	macd, signal, hist, _ := s.GetMACD(ctx, symbol, "1", 12, 26, 9)

	return &MarketStats{
		SpeedBuy:           speedBuy,
		SpeedSell:          speedSell,
		SpeedBuy30s:        speedBuy30s,
		SpeedSell30s:       speedSell30s,
		SpeedBuy10s:        speedBuy10s,
		SpeedSell10s:       speedSell10s,
		DepthBid:           avgBid60,
		DepthAsk:           avgAsk60,
		PriceChange60s:     priceChange60s,
		PriceChange30s:     priceChange30s,
		PriceChange10s:     priceChange10s,
		OBI:                obi,
		CVD:                cvd,
		TSI:                tsi,
		GLI:                gli,
		TradeVelocity:      tradeVelocity,
		ConclusionScore:    conclusionScore,
		ConclusionScore30s: conclusionScore30s,
		ConclusionScore10s: conclusionScore10s,
		LastPrice:          lastPrice,
		RSI:                rsi,
		MACD:               macd,
		MACDSignal:         signal,
		MACDHist:           hist,
		WSStatus:           s.exchange.GetWSStatus(),
	}, nil
}

func (s *MarketService) updateOrderBook(ctx context.Context, symbol string) {
	// Check if latest snapshot is fresh (< 5s)
	if history, ok := s.depthHistory[symbol]; ok && len(history) > 0 {
		last := history[len(history)-1]
		if s.timeNow().Sub(last.Time) < 5*time.Second {
			return // Fresh enough
		}
	}

	// Fetch Linear (Futures) Order Book
	// Fetch Linear (Futures) Order Book
	// We use a separate context with timeout to avoid blocking too long
	var cancel context.CancelFunc
	timeout := 2 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		// Parent context has a deadline, use the earlier of the two
		earliest := time.Now().Add(timeout)
		if deadline.Before(earliest) {
			ctx, cancel = context.WithDeadline(ctx, deadline)
		} else {
			ctx, cancel = context.WithDeadline(ctx, earliest)
		}
	} else {
		ctx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	linearOB, err := s.exchange.GetOrderBook(ctx, symbol, "linear")
	if err != nil || linearOB == nil {
		return // Skip update on error or nil result
	}

	// Calculate Total Volume (within +/- 0.5% range)
	// 1. Find Mid Price
	if len(linearOB.Bids) == 0 || len(linearOB.Asks) == 0 {
		return
	}
	bestBid := linearOB.Bids[0].Price
	bestAsk := linearOB.Asks[0].Price
	midPrice := (bestBid + bestAsk) / 2

	// 2. Define Range
	rangePct := 0.005 // 0.5%
	minBid := midPrice * (1 - rangePct)
	maxAsk := midPrice * (1 + rangePct)

	var totalBid, totalAsk float64
	for _, e := range linearOB.Bids {
		if e.Price >= minBid {
			totalBid += e.Size
		}
	}
	for _, e := range linearOB.Asks {
		if e.Price <= maxAsk {
			totalAsk += e.Size
		}
	}

	now := s.timeNow()
	// Append to history
	s.depthHistory[symbol] = append(s.depthHistory[symbol], DepthSnapshot{
		Time:     now,
		TotalBid: totalBid,
		TotalAsk: totalAsk,
	})

	// Prune old history (> 60s)
	cutoff := now.Add(-60 * time.Second)
	validHistory := s.depthHistory[symbol][:0]
	for _, snapshot := range s.depthHistory[symbol] {
		if snapshot.Time.After(cutoff) {
			validHistory = append(validHistory, snapshot)
		}
	}
	s.depthHistory[symbol] = validHistory
}

type LiquidityCluster struct {
	Price  float64 `json:"price"`
	Volume float64 `json:"volume"`
	Type   string  `json:"type"`   // "bid" or "ask"
	Source string  `json:"source"` // "spot", "linear", "combined"
}

func (s *MarketService) GetLiquidityClusters(ctx context.Context, symbol string) ([]LiquidityCluster, error) {
	// Check Cache
	s.mu.Lock()
	if cached, ok := s.cache[symbol]; ok {
		if s.timeNow().Before(cached.Expiry) {
			s.mu.Unlock()
			return cached.Data, nil
		}
	}
	s.mu.Unlock()

	// Fetch Linear (Futures) Order Book
	linearOB, err := s.exchange.GetOrderBook(ctx, symbol, "linear")
	if err != nil {
		return nil, err
	}

	// Fetch Spot Order Book
	// Note: Spot symbols often differ (e.g., BTCUSDT vs BTC/USDT or just BTCUSDT).
	// Bybit Spot usually uses same symbol format for major pairs.
	spotOB, err := s.exchange.GetOrderBook(ctx, symbol, "spot")
	if err != nil {
		// Log error but continue with linear only? Or fail?
		// For now, let's assume if spot fails (maybe symbol doesn't exist), we just use linear.
		spotOB = &domain.OrderBook{}
	}

	clusters := make([]LiquidityCluster, 0)

	// Calculate Total Volume
	var totalBid, totalAsk float64
	for _, e := range linearOB.Bids {
		totalBid += e.Size
	}
	for _, e := range linearOB.Asks {
		totalAsk += e.Size
	}

	// Process Bids
	clusters = append(clusters, s.processSide(linearOB.Bids, spotOB.Bids, "bid")...)

	// Process Asks
	clusters = append(clusters, s.processSide(linearOB.Asks, spotOB.Asks, "ask")...)

	// Update Cache
	s.mu.Lock()
	s.cache[symbol] = CachedLiquidity{
		Data:     clusters,
		TotalBid: totalBid,
		TotalAsk: totalAsk,
		Expiry:   s.timeNow().Add(10 * time.Second),
	}

	// Record for history
	s.recordLiquiditySnapshot(symbol, clusters)
	s.mu.Unlock()

	return clusters, nil
}

func (s *MarketService) recordLiquiditySnapshot(symbol string, clusters []LiquidityCluster) {
	now := s.timeNow().Unix()

	// Avoid recording too frequently (e.g. record at most every 10s per symbol)
	if history, ok := s.liquidityHistory[symbol]; ok && len(history) > 0 {
		if now-history[len(history)-1].Time < 10 {
			return
		}
	}

	snapshot := domain.LiquiditySnapshot{
		Symbol: symbol,
		Time:   now,
	}

	for _, c := range clusters {
		bucket := domain.LiquidityBucket{Price: c.Price, Volume: c.Volume}
		if c.Type == "bid" {
			snapshot.Bids = append(snapshot.Bids, bucket)
		} else {
			snapshot.Asks = append(snapshot.Asks, bucket)
		}
	}

	s.liquidityHistory[symbol] = append(s.liquidityHistory[symbol], snapshot)

	// Persist to DB
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if s.repo != nil {
			s.repo.SaveLiquiditySnapshot(ctx, &snapshot)
		}
	}()

	// Keep last 1 hour of history in memory (approx 360 snapshots if every 10s)
	cutoff := now - 3600
	valid := s.liquidityHistory[symbol][:0]
	for _, snap := range s.liquidityHistory[symbol] {
		if snap.Time > cutoff {
			valid = append(valid, snap)
		}
	}
	s.liquidityHistory[symbol] = valid
}

func (s *MarketService) ForceRecordLiquiditySnapshot(ctx context.Context, symbol string) {
	linearOB, err := s.exchange.GetOrderBook(ctx, symbol, "linear")
	if err != nil {
		return
	}
	spotOB, _ := s.exchange.GetOrderBook(ctx, symbol, "spot")
	if spotOB == nil {
		spotOB = &domain.OrderBook{}
	}

	clusters := make([]LiquidityCluster, 0)
	clusters = append(clusters, s.processSide(linearOB.Bids, spotOB.Bids, "bid")...)
	clusters = append(clusters, s.processSide(linearOB.Asks, spotOB.Asks, "ask")...)

	snapshot := domain.LiquiditySnapshot{
		Symbol: symbol,
		Time:   s.timeNow().Unix(),
	}

	for _, c := range clusters {
		bucket := domain.LiquidityBucket{Price: c.Price, Volume: c.Volume}
		if c.Type == "bid" {
			snapshot.Bids = append(snapshot.Bids, bucket)
		} else {
			snapshot.Asks = append(snapshot.Asks, bucket)
		}
	}

	// Persist to DB immediately
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if s.repo != nil {
			s.repo.SaveLiquiditySnapshot(ctx, &snapshot)
		}
	}()
}

func (s *MarketService) GetLiquidityHistory(symbol string) []domain.LiquiditySnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	// If memory is empty, try to load from repo
	if len(s.liquidityHistory[symbol]) == 0 && s.repo != nil {
		history, err := s.repo.ListLiquiditySnapshots(context.Background(), symbol, 360) // Last 360 ≈ 1 hour
		if err == nil && len(history) > 0 {
			// ListLiquiditySnapshots returns DESC (most recent first), reverse for internal history
			memHistory := make([]domain.LiquiditySnapshot, len(history))
			for i, h := range history {
				memHistory[len(history)-1-i] = *h
			}
			s.liquidityHistory[symbol] = memHistory
		}
	}

	return s.liquidityHistory[symbol]
}

func (s *MarketService) IsWallStable(symbol string, price float64, side string, threshold float64, duration time.Duration) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	history, ok := s.liquidityHistory[symbol]
	if !ok || len(history) == 0 {
		return false
	}

	now := s.timeNow().Unix()
	startTime := now - int64(duration.Seconds())

	count := 0
	matched := 0

	for i := len(history) - 1; i >= 0; i-- {
		snap := history[i]
		if snap.Time < startTime {
			break
		}

		count++
		buckets := snap.Asks
		if side == "bid" {
			buckets = snap.Bids
		}

		found := false
		for _, b := range buckets {
			// Check if price is within 0.1% range of the bucket
			if math.Abs(b.Price-price)/price < 0.001 {
				if b.Volume >= threshold {
					found = true
					break
				}
			}
		}
		if found {
			matched++
		}
	}

	if count == 0 {
		return false
	}

	// Stable if present in more than 70% of snapshots within the duration
	return float64(matched)/float64(count) >= 0.7
}

func (s *MarketService) processSide(linear, spot []domain.OrderBookEntry, side string) []LiquidityCluster {
	// Simple aggregation: Map price -> volume
	// We round price to some precision to group them?
	// For now, let's just take the raw levels and maybe filter top N.

	// Combine
	all := make(map[float64]float64)
	for _, e := range linear {
		all[e.Price] += e.Size // Size in contracts/coins
	}
	for _, e := range spot {
		all[e.Price] += e.Size
	}

	// 1. Convert Map to Slice for Sliding Window
	var rawPoints []LiquidityCluster
	for p, v := range all {
		rawPoints = append(rawPoints, LiquidityCluster{
			Price:  p,
			Volume: v,
			Type:   side,
			Source: "combined",
		})
	}

	// Sort by Price ASC
	sort.Slice(rawPoints, func(i, j int) bool {
		return rawPoints[i].Price < rawPoints[j].Price
	})

	if len(rawPoints) == 0 {
		return nil
	}

	// 2. Calculate Density (Price Zoning) using Sliding Window (O(n))
	// For each point, sum volume of all points within ±0.05% range

	var densityPoints []LiquidityCluster
	left := 0
	right := 0
	currentVol := 0.0

	for i := 0; i < len(rawPoints); i++ {
		centerP := rawPoints[i].Price
		delta := centerP * 0.0005 // 0.05%
		minP := centerP - delta
		maxP := centerP + delta

		// Adjust right
		for right < len(rawPoints) && rawPoints[right].Price <= maxP {
			currentVol += rawPoints[right].Volume
			right++
		}

		// Adjust left
		for left < right && rawPoints[left].Price < minP {
			currentVol -= rawPoints[left].Volume
			left++
		}

		densityPoints = append(densityPoints, LiquidityCluster{
			Price:  centerP,
			Volume: currentVol,
			Type:   side,
			Source: "combined",
		})
	}

	// 3. Find Local Peaks in Density
	// We only want to show the "center" of these dense zones.
	var peaks []LiquidityCluster
	n := len(densityPoints)
	if n < 3 {
		return densityPoints
	}

	for i := 0; i < n; i++ {
		isPeak := true
		vol := densityPoints[i].Volume

		// Check Prev
		if i > 0 {
			if vol < densityPoints[i-1].Volume {
				isPeak = false
			}
		}
		// Check Next
		if i < n-1 {
			if vol < densityPoints[i+1].Volume {
				isPeak = false
			}
		}

		if isPeak {
			peaks = append(peaks, densityPoints[i])
		}
	}

	// Sort peaks by Volume DESC for display priority
	sort.Slice(peaks, func(i, j int) bool {
		return peaks[i].Volume > peaks[j].Volume
	})

	// Limit to top 200 peaks
	if len(peaks) > 200 {
		peaks = peaks[:200]
	}

	return peaks
}

// GetTradeSentiment returns a score from -1.0 (Strong Sell) to 1.0 (Strong Buy)
// based on the last 60 seconds of trade volume.
func (s *MarketService) GetTradeSentiment(ctx context.Context, symbol string) (float64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	trades, ok := s.trades[symbol]
	if !ok || len(trades) == 0 {
		return 0, nil
	}

	now := s.timeNow()
	cutoff := now.Add(-60 * time.Second)

	var buyVol, sellVol float64
	for _, t := range trades {
		if t.Time.After(cutoff) {
			if t.Side == "Buy" {
				buyVol += t.Size * t.Price
			} else {
				sellVol += t.Size * t.Price
			}
		}
	}

	totalVol := buyVol + sellVol
	if totalVol == 0 {
		return 0, nil
	}

	return (buyVol - sellVol) / totalVol, nil
}

func (s *MarketService) GetCandles(ctx context.Context, symbol, interval string, limit int) ([]domain.Candle, error) {
	return s.exchange.GetCandles(ctx, symbol, interval, limit)
}

func (s *MarketService) GetRSI(ctx context.Context, symbol, interval string, period int) (float64, error) {
	history, err := s.GetRSIHistory(ctx, symbol, interval, period, 1)
	if err != nil {
		return 0, err
	}
	if len(history) == 0 {
		return 0, fmt.Errorf("no rsi data")
	}
	return history[len(history)-1], nil
}

func (s *MarketService) GetRSIHistory(ctx context.Context, symbol, interval string, period int, limit RSIHistoryLimit) ([]float64, error) {
	// To get 'limit' RSI values, we need:
	// 1 initial candle for first price change
	// 'period' candles for first average
	// 'limit-1' additional candles for subsequent averages
	// Total: period + limit
	needed := period + int(limit) + 1
	candles, err := s.exchange.GetCandles(ctx, symbol, interval, needed)
	if err != nil {
		return nil, err
	}
	if len(candles) < period+1 {
		return nil, fmt.Errorf("not enough candles for RSI (got %d, need %d)", len(candles), period+1)
	}

	return s.CalculateRSI(candles, period), nil
}

type RSIHistoryLimit int

func (s *MarketService) CalculateRSI(candles []domain.Candle, period int) []float64 {
	if len(candles) < period+1 {
		return nil
	}

	n := len(candles)
	rsiValues := make([]float64, 0, n-period)

	var gains, losses float64
	// First RSI calculation (SMA)
	for i := 1; i <= period; i++ {
		change := candles[i].Close - candles[i-1].Close
		if change > 0 {
			gains += change
		} else {
			losses += -change
		}
	}

	avgGain := gains / float64(period)
	avgLoss := losses / float64(period)

	calcRSI := func(g, l float64) float64 {
		if l == 0 {
			if g == 0 {
				return 50
			}
			return 100
		}
		rs := g / l
		return 100.0 - (100.0 / (1.0 + rs))
	}

	rsiValues = append(rsiValues, calcRSI(avgGain, avgLoss))

	// Wilder's Smoothing / Exponential Moving Average for subsequent values
	for i := period + 1; i < n; i++ {
		change := candles[i].Close - candles[i-1].Close
		currentGain := 0.0
		currentLoss := 0.0
		if change > 0 {
			currentGain = change
		} else {
			currentLoss = -change
		}

		avgGain = (avgGain*float64(period-1) + currentGain) / float64(period)
		avgLoss = (avgLoss*float64(period-1) + currentLoss) / float64(period)

		rsiValues = append(rsiValues, calcRSI(avgGain, avgLoss))
	}

	return rsiValues
}

func (s *MarketService) GetMACD(ctx context.Context, symbol, interval string, fastPeriod, slowPeriod, signalPeriod int) (macd, signal, hist float64, err error) {
	// Needed candles: slowPeriod (for first EMA) + signalPeriod (for signal line) + some warmup
	needed := slowPeriod + signalPeriod + 50
	candles, err := s.exchange.GetCandles(ctx, symbol, interval, needed)
	if err != nil {
		return 0, 0, 0, err
	}
	if len(candles) < slowPeriod {
		return 0, 0, 0, fmt.Errorf("not enough candles for MACD (got %d, need %d)", len(candles), slowPeriod)
	}

	macdValues, signalValues, histValues := s.CalculateMACD(candles, fastPeriod, slowPeriod, signalPeriod)
	if len(macdValues) == 0 {
		return 0, 0, 0, fmt.Errorf("not enough data for MACD calculation")
	}

	return macdValues[len(macdValues)-1], signalValues[len(signalValues)-1], histValues[len(histValues)-1], nil
}

func (s *MarketService) CalculateMACD(candles []domain.Candle, fastPeriod, slowPeriod, signalPeriod int) (macd, signal, hist []float64) {
	if len(candles) < slowPeriod {
		return nil, nil, nil
	}

	prices := make([]float64, len(candles))
	for i, c := range candles {
		prices[i] = c.Close
	}

	fastEMA := s.CalculateEMA(prices, fastPeriod)
	slowEMA := s.CalculateEMA(prices, slowPeriod)

	// Since slowEMA starts at index slowPeriod-1, and fastEMA starts at fastPeriod-1,
	// we need to align them. Both will have a value at index i >= slowPeriod-1.

	macdLine := make([]float64, 0)
	for i := slowPeriod - 1; i < len(prices); i++ {
		macdLine = append(macdLine, fastEMA[i]-slowEMA[i])
	}

	if len(macdLine) < signalPeriod {
		return macdLine, nil, nil
	}

	signalLine := s.CalculateEMA(macdLine, signalPeriod)

	// Align MACD and Signal
	startIdx := signalPeriod - 1
	macdRet := macdLine[startIdx:]
	signalRet := signalLine[startIdx:]
	histRet := make([]float64, len(macdRet))

	for i := 0; i < len(macdRet); i++ {
		histRet[i] = macdRet[i] - signalRet[i]
	}

	return macdRet, signalRet, histRet
}

func (s *MarketService) CalculateEMA(values []float64, period int) []float64 {
	if len(values) < period {
		return nil
	}
	ema := make([]float64, len(values))
	k := 2.0 / float64(period+1)

	// SMA for first value
	var sum float64
	for i := 0; i < period; i++ {
		sum += values[i]
	}
	ema[period-1] = sum / float64(period)

	for i := period; i < len(values); i++ {
		ema[i] = values[i]*k + ema[i-1]*(1-k)
	}
	return ema
}

func (s *MarketService) GetBiggestOrderBookPrice(ctx context.Context, symbol string, currentPrice float64, side string) (float64, error) {
	clusters, err := s.GetLiquidityClusters(ctx, symbol)
	if err != nil {
		return 0, err
	}

	findIn := func(limit float64) (float64, float64, float64, int) {
		var mVol, bPrice, sVol float64
		var cnt int
		for _, c := range clusters {
			// Filter by side if provided
			if side != "" && c.Type != side {
				continue
			}

			// Distance in percentage
			dist := math.Abs(c.Price-currentPrice) / currentPrice

			// Filter: Near but not too close
			// Near: limit (2% or 5%)
			// Too close: 0.15% (to avoid immediate entry/noise)
			if dist >= 0.0015 && dist <= limit {
				sVol += c.Volume
				cnt++
				if c.Volume > mVol {
					mVol = c.Volume
					bPrice = c.Price
				}
			}
		}
		return bPrice, mVol, sVol, cnt
	}

	bestPrice, maxVol, sumVol, count := findIn(0.02)
	if bestPrice == 0 {
		// Fallback: try a wider range if nothing found in 2%
		bestPrice, maxVol, sumVol, count = findIn(0.05)
	}

	if bestPrice == 0 {
		return 0, fmt.Errorf("no suitable order book level found for %s", symbol)
	}

	// Structure Check: "No structure in the book"
	// Ensure the biggest wall is distinctively larger than the average noise.
	if count > 1 {
		avgOthers := (sumVol - maxVol) / float64(count-1)
		// Threshold: Max must be at least 30% larger than the average of the rest
		if maxVol < avgOthers*1.3 {
			return 0, fmt.Errorf("No structure in the book")
		}
	}

	return bestPrice, nil
}

func (s *MarketService) GetStrongSidePrice(ctx context.Context, symbol string, currentPrice float64) (float64, string, error) {
	clusters, err := s.GetLiquidityClusters(ctx, symbol)
	if err != nil {
		return 0, "", err
	}

	// Determine strong side in 2% range
	rangeLimit := 0.02
	var bidVol, askVol float64
	for _, c := range clusters {
		dist := math.Abs(c.Price-currentPrice) / currentPrice
		if dist <= rangeLimit {
			if c.Type == "bid" {
				bidVol += c.Volume
			} else {
				askVol += c.Volume
			}
		}
	}

	strongSide := "bid"
	if askVol > bidVol {
		strongSide = "ask"
	}

	// Use GetBiggestOrderBookPrice but force the side to strongSide
	price, err := s.GetBiggestOrderBookPrice(ctx, symbol, currentPrice, strongSide)
	return price, strongSide, err
}

func (s *MarketService) HasNearbySignificantWall(ctx context.Context, symbol string, targetPrice float64, maxDevPct float64) (float64, bool, error) {
	clusters, err := s.GetLiquidityClusters(ctx, symbol)
	if err != nil {
		return 0, false, err
	}

	if len(clusters) == 0 {
		return 0, false, fmt.Errorf("no clusters found")
	}

	// 1. Find the Absolute Biggest Wall for scale
	var globalMaxVol float64
	for _, c := range clusters {
		if c.Volume > globalMaxVol {
			globalMaxVol = c.Volume
		}
	}

	if globalMaxVol == 0 {
		return 0, false, fmt.Errorf("no walls found")
	}

	// 2. Search for any "big enough" wall within maxDevPct
	// "Big Enough" = at least 30% of the biggest wall?
	// Actually, let's use a threshold.
	threshold := globalMaxVol * 0.3

	var closestPrice float64
	minDist := 100.0 // large number

	for _, c := range clusters {
		dist := math.Abs(c.Price-targetPrice) / targetPrice * 100
		if dist <= maxDevPct && c.Volume >= threshold {
			if dist < minDist {
				minDist = dist
				closestPrice = c.Price
			}
		}
	}

	if closestPrice > 0 {
		return closestPrice, true, nil
	}

	return 0, false, nil
}

type LiquidityAnalysis struct {
	IsImbalanced      bool    `json:"is_imbalanced"`
	WeakSide          string  `json:"weak_side"`
	Ratio             float64 `json:"ratio"`
	Clusters          int     `json:"clusters"` // Legacy/Weak side
	BidClusters       int     `json:"bid_clusters"`
	AskClusters       int     `json:"ask_clusters"`
	WallPct           float64 `json:"wall_pct"`
	GLI               float64 `json:"gli"`
	Reason            string  `json:"reason"`
	DominantMaxVolume float64 `json:"dominant_max_volume"`
	WeakMaxVolume     float64 `json:"weak_max_volume"`
}

func (s *MarketService) AnalyzeLiquidityImbalance(ctx context.Context, symbol string, currentPrice float64, minRatio float64, maxClusters int, wallThresholdPct float64) (*LiquidityAnalysis, error) {
	clusters, err := s.GetLiquidityClusters(ctx, symbol)
	if err != nil {
		return nil, err
	}

	analysis := &LiquidityAnalysis{
		IsImbalanced: false,
	}

	// Fetch GLI from MarketStats
	stats, err := s.GetMarketStats(ctx, symbol)
	if err == nil && stats != nil {
		analysis.GLI = stats.GLI
	}

	// Range for cumulative volume (e.g., 2%)
	rangeLimit := 0.02
	var bidVol, askVol float64
	var bidClusters, askClusters []LiquidityCluster
	var maxBidVol, maxAskVol float64

	for _, c := range clusters {
		dist := math.Abs(c.Price-currentPrice) / currentPrice
		if dist <= rangeLimit {
			if c.Type == "bid" {
				bidVol += c.Volume
				bidClusters = append(bidClusters, c)
				if c.Volume > maxBidVol {
					maxBidVol = c.Volume
				}
			} else {
				askVol += c.Volume
				askClusters = append(askClusters, c)
				if c.Volume > maxAskVol {
					maxAskVol = c.Volume
				}
			}
		}
	}

	if bidVol == 0 || askVol == 0 {
		analysis.Reason = "One side has zero liquidity in range"
		return analysis, nil
	}

	// Determine Weak/Dominant side
	var dominantVol, weakVol float64
	var weakSide string
	var weakClusters []LiquidityCluster
	var dominantMaxVol, weakMaxVol float64

	if bidVol > askVol {
		dominantVol = bidVol
		weakVol = askVol
		weakSide = "ask"
		weakClusters = askClusters
		dominantMaxVol = maxBidVol
		weakMaxVol = maxAskVol
	} else {
		dominantVol = askVol
		weakVol = bidVol
		weakSide = "bid"
		weakClusters = bidClusters
		dominantMaxVol = maxAskVol
		weakMaxVol = maxBidVol
	}

	ratio := dominantVol / weakVol
	analysis.Ratio = ratio
	analysis.WeakSide = weakSide
	analysis.DominantMaxVolume = dominantMaxVol
	analysis.WeakMaxVolume = weakMaxVol
	analysis.Clusters = len(weakClusters)
	analysis.BidClusters = len(bidClusters)
	analysis.AskClusters = len(askClusters)
	if dominantMaxVol > 0 {
		analysis.WallPct = (weakMaxVol / dominantMaxVol) * 100.0
	}

	// Condition 1: Imbalance Ratio
	if ratio < minRatio {
		analysis.Reason = fmt.Sprintf("Imbalance ratio too low: %.2f (min %.1f)", ratio, minRatio)
		return analysis, nil
	}

	// Condition 2: Weaker side thin liquidity distribution (few clusters)
	if len(weakClusters) > maxClusters {
		analysis.Reason = fmt.Sprintf("Weak side too dense: %d clusters (max %d)", len(weakClusters), maxClusters)
		return analysis, nil
	}

	// Condition 3: No strong wall on the weak side within short distance (e.g., 0.5%)
	shortRange := 0.005
	for _, c := range weakClusters {
		dist := math.Abs(c.Price-currentPrice) / currentPrice
		if dist <= shortRange {
			// A "strong wall" is defined relative to dominant side's max volume
			if c.Volume > dominantMaxVol*wallThresholdPct {
				analysis.Reason = fmt.Sprintf("Strong wall found on weak side at %.2f (vol %.2f)", c.Price, c.Volume)
				return analysis, nil
			}
		}
	}

	// Condition 4: Largest order on weak side relatively small compared to dominant
	// We use wallThresholdPct * 1.5 as a heuristic for "The largest order overall" vs "Nearby wall"
	if weakMaxVol > dominantMaxVol*(wallThresholdPct*1.6) {
		analysis.Reason = fmt.Sprintf("Weak side max volume too large: %.2f vs Dominant %.2f (threshold %.1f%%)", weakMaxVol, dominantMaxVol, wallThresholdPct*160)
		return analysis, nil
	}

	// Condition 5: Liquidity on the weak side is not increasing rapidly
	// Check history (last 1 minute)
	history := s.GetLiquidityHistory(symbol)
	if len(history) > 2 {
		cutoff := s.timeNow().Unix() - 60
		var initialWeakVol float64
		for _, snap := range history {
			if snap.Time >= cutoff {
				var snapWeakVol float64
				buckets := snap.Asks
				if weakSide == "bid" {
					buckets = snap.Bids
				}
				for _, b := range buckets {
					dist := math.Abs(b.Price-currentPrice) / currentPrice
					if dist <= rangeLimit {
						snapWeakVol += b.Volume
					}
				}
				if initialWeakVol == 0 {
					initialWeakVol = snapWeakVol
				} else {
					// Compare with initial (from 1 min ago or earliest in window)
					if snapWeakVol > initialWeakVol*1.5 {
						analysis.IsImbalanced = false
						analysis.Reason = "Weak side liquidity increasing rapidly (reinforcement)"
						return analysis, nil
					}
				}
			}
		}
	}

	analysis.IsImbalanced = true
	analysis.Reason = "Clear structural weakness on " + weakSide + " side"
	return analysis, nil
}
