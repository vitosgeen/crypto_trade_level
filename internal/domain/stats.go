package domain

type CoinData struct {
	Symbol            string
	BaseCoin          string
	QuoteCoin         string
	Status            string
	LastPrice         float64
	Price24hPcnt      float64
	Volume24h         float64
	OpenInterest      float64
	OpenInterestValue float64
	Range10m          float64
	Range1h           float64
	Range4h           float64
	Trend10m          string // "up", "down", or ""
	Trend1h           string
	Trend4h           string
	Trend24h          string
	FundingRate       float64
	Max10m            float64
	Min10m            float64
	Max1h             float64
	Min1h             float64
	Max4h             float64
	Min4h             float64
	Range24h          float64
	Max24h            float64
	Min24h            float64
	Near4hMax         bool
	Near4hMin         bool
	Near1hMax         bool
	Near1hMin         bool
	Near24hMax        bool
	Near24hMin        bool
}
