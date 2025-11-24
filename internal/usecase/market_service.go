package usecase

import (
	"context"
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

type MarketService struct {
	exchange     domain.Exchange
	cache        map[string]CachedLiquidity
	trades       map[string][]Trade // Symbol -> Trades
	depthHistory map[string][]DepthSnapshot
	mu           sync.Mutex
	timeNow      func() time.Time // For testing
}

type DepthSnapshot struct {
	Time     time.Time
	TotalBid float64
	TotalAsk float64
}

func NewMarketService(exchange domain.Exchange) *MarketService {
	s := &MarketService{
		exchange:     exchange,
		cache:        make(map[string]CachedLiquidity),
		trades:       make(map[string][]Trade),
		depthHistory: make(map[string][]DepthSnapshot),
		timeNow:      time.Now,
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

	// Prune old trades (> 60s)
	cutoff := now.Add(-60 * time.Second)
	validTrades := s.trades[symbol][:0]
	for _, t := range s.trades[symbol] {
		if t.Time.After(cutoff) {
			validTrades = append(validTrades, t)
		}
	}
	s.trades[symbol] = validTrades
}

type MarketStats struct {
	SpeedBuy  float64 `json:"speed_buy"`
	SpeedSell float64 `json:"speed_sell"`
	DepthBid  float64 `json:"depth_bid"`
	DepthAsk  float64 `json:"depth_ask"`
}

func (s *MarketService) GetMarketStats(ctx context.Context, symbol string) (*MarketStats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 1. Calculate Speed (from trades)
	var speedBuy, speedSell float64
	if trades, ok := s.trades[symbol]; ok {
		now := s.timeNow()
		cutoff := now.Add(-60 * time.Second)
		// Prune again just in case (lazy pruning)
		validTrades := trades[:0]
		for _, t := range trades {
			if t.Time.After(cutoff) {
				validTrades = append(validTrades, t)
				if t.Side == "Buy" {
					speedBuy += t.Size
				} else {
					speedSell += t.Size
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

	return &MarketStats{
		SpeedBuy:  speedBuy,
		SpeedSell: speedSell,
		DepthBid:  avgBid,
		DepthAsk:  avgAsk,
	}, nil
}

func (s *MarketService) updateOrderBook(ctx context.Context, symbol string) {
	// Check if latest snapshot is fresh (< 5s)
	if history, ok := s.depthHistory[symbol]; ok && len(history) > 0 {
		last := history[len(history)-1]
		if time.Since(last.Time) < 5*time.Second {
			return // Fresh enough
		}
	}

	// Fetch Linear (Futures) Order Book
	// We use a separate context with timeout to avoid blocking too long
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	linearOB, err := s.exchange.GetOrderBook(ctx, symbol, "linear")
	if err != nil {
		return // Skip update on error
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
		if time.Now().Before(cached.Expiry) {
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
		Expiry:   time.Now().Add(10 * time.Second),
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

	// 2. Calculate Density (Price Zoning)
	// For each point, sum volume of all points within ±0.05% range
	// Optimization: Use sliding window since points are sorted

	// Define Zone Delta (0.05% of current price)
	// Since price changes, delta changes slightly, but we can use the point's own price.

	var densityPoints []LiquidityCluster

	for i := 0; i < len(rawPoints); i++ {
		centerP := rawPoints[i].Price
		delta := centerP * 0.0005 // 0.05%

		minP := centerP - delta
		maxP := centerP + delta

		var sumVol float64

		// Simple look-around (can be optimized but N=200 is small enough for brute-ish force)
		// Look backwards
		for k := i; k >= 0; k-- {
			if rawPoints[k].Price < minP {
				break
			}
			sumVol += rawPoints[k].Volume
		}
		// Look forwards
		for k := i + 1; k < len(rawPoints); k++ {
			if rawPoints[k].Price > maxP {
				break
			}
			sumVol += rawPoints[k].Volume
		}

		densityPoints = append(densityPoints, LiquidityCluster{
			Price:  centerP,
			Volume: sumVol,
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

	// Limit to top 50 peaks
	if len(peaks) > 50 {
		peaks = peaks[:50]
	}

	return peaks
}
