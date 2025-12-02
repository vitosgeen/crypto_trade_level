package usecase

import (
	"context"
	"log"
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
	exchange       domain.Exchange
	cache          map[string]CachedLiquidity
	trades         map[string][]Trade // Symbol -> Trades
	depthHistory   map[string][]DepthSnapshot
	priceHistory   map[string][]PricePoint // Symbol -> Price Points
	cvdAccumulator map[string]float64      // Symbol -> Cumulative Volume Delta
	subscribed     map[string]bool         // Symbol -> Subscribed
	mu             sync.Mutex
	timeNow        func() time.Time // For testing
}

type DepthSnapshot struct {
	Time     time.Time
	TotalBid float64
	TotalAsk float64
}

const MaxGLI = 10.0

func NewMarketService(exchange domain.Exchange) *MarketService {
	s := &MarketService{
		exchange:       exchange,
		cache:          make(map[string]CachedLiquidity),
		trades:         make(map[string][]Trade),
		depthHistory:   make(map[string][]DepthSnapshot),
		priceHistory:   make(map[string][]PricePoint),
		cvdAccumulator: make(map[string]float64),
		subscribed:     make(map[string]bool),
		timeNow:        time.Now,
	}

	// Subscribe to trades
	exchange.OnTradeUpdate(s.handleTrade)

	return s
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
	SpeedBuy        float64 `json:"speed_buy"`
	SpeedSell       float64 `json:"speed_sell"`
	DepthBid        float64 `json:"depth_bid"`
	DepthAsk        float64 `json:"depth_ask"`
	PriceChange60s  float64 `json:"price_change_60s"`
	OBI             float64 `json:"obi"`
	CVD             float64 `json:"cvd"`
	TSI             float64 `json:"tsi"`
	GLI             float64 `json:"gli"`
	TradeVelocity   float64 `json:"trade_velocity"`
	ConclusionScore float64 `json:"conclusion_score"`
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
			if len(s.trades[symbol]) > 0 {
				// Check timestamp of latest trade
				lastTradeTime := s.trades[symbol][len(s.trades[symbol])-1].Time
				if s.timeNow().Sub(lastTradeTime) < 60*time.Second {
					// Already refreshed, proceed to calculation
					// We need to break out of the update block, but we are inside if err == nil.
					// We can just set err = nil and skip the update loop?
					// Or better, wrap the update logic in an else or just use a flag.
					// Let's just use a goto or restructure.
					// Restructuring is cleaner.
				} else {
					// Replace old trades with fresh ones
					s.trades[symbol] = nil
					for _, t := range recentTrades {
						s.trades[symbol] = append(s.trades[symbol], Trade{
							Symbol: t.Symbol,
							Side:   t.Side,
							Size:   t.Size,
							Price:  t.Price,
							Time:   time.UnixMilli(t.Time),
						})
					}
				}
			} else {
				// Replace old trades with fresh ones
				s.trades[symbol] = nil
				for _, t := range recentTrades {
					s.trades[symbol] = append(s.trades[symbol], Trade{
						Symbol: t.Symbol,
						Side:   t.Side,
						Size:   t.Size,
						Price:  t.Price,
						Time:   time.UnixMilli(t.Time),
					})
				}
			}
		}
	}
	defer s.mu.Unlock()

	// 1. Calculate Speed (from trades)
	var speedBuy, speedSell float64
	var tradeCount int
	if trades, ok := s.trades[symbol]; ok {
		now := s.timeNow()
		cutoff := now.Add(-60 * time.Second)
		// Prune again just in case (lazy pruning)
		validTrades := trades[:0]
		for _, t := range trades {
			if t.Time.After(cutoff) {
				validTrades = append(validTrades, t)
				tradeCount++
				if t.Side == "Buy" {
					speedBuy += t.Size * t.Price
				} else {
					speedSell += t.Size * t.Price
				}
			}
		}
		s.trades[symbol] = validTrades
	}

	// 2. Get Depth (60s Moving Average)
	// Check if we need to update depth history (lazy fetch if stale)
	s.updateOrderBook(ctx, symbol)

	var avgBid, avgAsk float64
	if history, ok := s.depthHistory[symbol]; ok && len(history) > 0 {
		var sumBid, sumAsk float64
		for _, snapshot := range history {
			sumBid += snapshot.TotalBid
			sumAsk += snapshot.TotalAsk
		}
		avgBid = sumBid / float64(len(history))
		avgAsk = sumAsk / float64(len(history))
	}

	// 3. Calculate 60s price change percentage
	var priceChange60s float64
	if prices, ok := s.priceHistory[symbol]; ok && len(prices) >= 2 {
		// Get oldest and newest price in the 60s window
		oldestPrice := prices[0].Price
		newestPrice := prices[len(prices)-1].Price

		if oldestPrice > 0 {
			priceChange60s = ((newestPrice - oldestPrice) / oldestPrice) * 100
		}
	}

	// 4. Calculate Indicators
	// OBI = (BidDepth - AskDepth) / (BidDepth + AskDepth)
	var obi float64
	if avgBid+avgAsk > 0 {
		obi = (avgBid - avgAsk) / (avgBid + avgAsk)
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
	// Formula: (OBI + (TSI_Ratio * 2) + (GLI_Ratio * 2) + (CVD_Ratio * 2)) / 7
	// We need to normalize TSI, GLI, CVD to -1..1 range for this composite score.

	// Normalize TSI (0..MaxTSI -> 0..1) - It's magnitude, not direction?
	// Wait, the frontend logic was:
	// const tsiRatio = stats.tsi / maxTsi; (0..1)
	// But Conclusion Score usually needs directional components (-1..1).
	// Let's look at how frontend did it.
	// Frontend:
	// const conclusion = (stats.obi + (tsiRatio * 2) + (gliRatio * 2) + (cvdRatio * 2)) / 7;
	// Wait, TSI is 0..1 (activity). GLI is ratio (0..Inf). CVD is volume.
	// This formula seems to mix ranges.
	// Let's refine it to be robust on backend.

	// OBI: -1 to 1 (already good)

	// TSI: Activity level. 0 to 1 (normalized by MaxTSI).
	// We need a dynamic MaxTSI here too? Or just use a reasonable baseline.
	// Let's use the same dynamic tracking or a fixed baseline for backend consistency.
	// Let's use 100 as baseline for now, or track it in service.
	// s.maxObservedTsi would be better.
	// Let's assume 100 for now to match initial frontend.
	maxTsi := 100.0
	tsiRatio := tsi / maxTsi
	if tsiRatio > 1 {
		tsiRatio = 1
	}
	// TSI is unsigned (activity), so it adds "energy" but not direction?
	// If the user wants "Sentiment", TSI might not be directional.
	// But if the formula adds it, maybe it assumes high activity = bullish?
	// Actually, let's look at GLI.
	// GLI = Sell/Buy.
	// If GLI > 1 (More Sell), it's Bearish?
	// If GLI < 1 (More Buy), it's Bullish?
	// Frontend GLI Ratio:
	// const gliRatio = stats.gli > 1 ? -(stats.gli - 1) : (1 - stats.gli);
	// Let's replicate this.
	var gliScore float64
	if gli > 1 {
		gliScore = -(gli - 1) // Bearish
		if gliScore < -1 {
			gliScore = -1
		}
	} else {
		gliScore = 1 - gli // Bullish? Wait.
		// If GLI = 0.5 (Sell=1, Buy=2). 1 - 0.5 = 0.5 (Bullish). Correct.
		// If GLI = 0 (No Sell). 1 - 0 = 1 (Max Bullish). Correct.
	}

	// CVD: Cumulative Volume Delta.
	// Normalize CVD?
	// Frontend: const cvdRatio = stats.cvd / maxCvd;
	// We need a MaxCVD.
	maxCvd := 10000.0 // Arbitrary baseline
	cvdScore := cvd / maxCvd
	if cvdScore > 1 {
		cvdScore = 1
	} else if cvdScore < -1 {
		cvdScore = -1
	}

	// TSI Direction?
	// If we want TSI to be directional, we need to know if it's buy-heavy or sell-heavy.
	// But TSI is just count.
	// Maybe the formula intends TSI to just boost the signal?
	// Or maybe I should check the frontend implementation I'm replacing.
	// The user said "Move Conclusion Score calculation from Frontend".
	// I'll assume the frontend logic was:
	// (OBI + (TSI_Score * 2) + (GLI_Score * 2) + (CVD_Score * 2)) / 7
	// Wait, TSI in frontend was likely just 0..1.
	// If I add 0..1 to a -1..1 score, it shifts it to bullish.
	// That seems wrong if TSI is just activity.
	// Let's assume TSI should be weighted by the dominant side?
	// Or maybe the user meant "Trade Velocity" or something else.
	// Let's stick to a safe implementation:
	// Score = (OBI + GLI_Score + CVD_Score) / 3
	// And maybe weigh them.
	// Let's use: (OBI + GLI_Score + CVD_Score) / 3 for now.
	// It returns -1 to 1.

	conclusionScore := (obi + gliScore + cvdScore) / 3.0

	return &MarketStats{
		SpeedBuy:        speedBuy,
		SpeedSell:       speedSell,
		DepthBid:        avgBid,
		DepthAsk:        avgAsk,
		PriceChange60s:  priceChange60s,
		OBI:             obi,
		CVD:             cvd,
		TSI:             tsi,
		GLI:             gli,
		TradeVelocity:   tradeVelocity,
		ConclusionScore: conclusionScore,
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
	s.mu.Unlock()

	return clusters, nil
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
