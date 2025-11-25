package domain

type Instrument struct {
	Symbol     string `json:"symbol"`
	BaseCoin   string `json:"base_coin"`
	QuoteCoin  string `json:"quote_coin"`
	Status     string `json:"status"`
	LaunchTime int64  `json:"launch_time"`
}
