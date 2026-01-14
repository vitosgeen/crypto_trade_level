package domain

type Instrument struct {
	Symbol     string `json:"symbol"`
	BaseCoin   string `json:"base_coin"`
	QuoteCoin  string `json:"quote_coin"`
	Status     string `json:"status"`
	LaunchTime int64  `json:"launch_time"`
}

type Ticker struct {
	Symbol          string  `json:"symbol"`
	LastPrice       float64 `json:"last_price"`
	Price24hPcnt    float64 `json:"price_24h_pcnt"`
	Volume24h       float64 `json:"volume_24h"` // Turnover (USD)
	OpenInterest    float64 `json:"open_interest"`
	FundingRate     float64 `json:"funding_rate"`
	NextFundingTime int64   `json:"next_funding_time"`
}
