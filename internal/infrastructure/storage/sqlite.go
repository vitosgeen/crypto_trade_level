package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
	"github.com/vitos/crypto_trade_level/internal/domain"
)

type SQLiteStore struct {
	db *sql.DB
}

func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}

	store := &SQLiteStore{db: db}
	if err := store.initSchema(); err != nil {
		return nil, err
	}

	return store, nil
}

func (s *SQLiteStore) initSchema() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS levels (
			id TEXT PRIMARY KEY,
			exchange TEXT NOT NULL,
			symbol TEXT NOT NULL,
			level_price REAL NOT NULL,
			base_size REAL NOT NULL,
			leverage INTEGER NOT NULL,
			margin_type TEXT NOT NULL,
			cool_down_ms INTEGER NOT NULL,
			stop_loss_at_base BOOLEAN NOT NULL DEFAULT 0,
			stop_loss_mode TEXT NOT NULL DEFAULT 'exchange',
			disable_speed_close BOOLEAN NOT NULL DEFAULT 0,
			max_consecutive_base_closes INTEGER NOT NULL DEFAULT 0,
			base_close_cooldown_ms INTEGER NOT NULL DEFAULT 0,
			take_profit_pct REAL NOT NULL DEFAULT 0.02,
			take_profit_mode TEXT NOT NULL DEFAULT 'fixed',
			is_auto BOOLEAN NOT NULL DEFAULT 0,
			auto_mode_enabled BOOLEAN NOT NULL DEFAULT 0,
			source TEXT,
			created_at DATETIME NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_levels_exchange_symbol ON levels(exchange, symbol);`,
		`CREATE TABLE IF NOT EXISTS symbol_tiers (
			exchange TEXT NOT NULL,
			symbol TEXT NOT NULL,
			tier1_pct REAL NOT NULL,
			tier2_pct REAL NOT NULL,
			tier3_pct REAL NOT NULL,
			updated_at DATETIME NOT NULL,
			PRIMARY KEY (exchange, symbol)
		);`,
		`CREATE TABLE IF NOT EXISTS trades (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			exchange TEXT NOT NULL,
			symbol TEXT NOT NULL,
			level_id TEXT NOT NULL,
			side TEXT NOT NULL,
			size REAL NOT NULL,
			price REAL NOT NULL,
			realized_pnl REAL NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS position_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			exchange TEXT NOT NULL,
			symbol TEXT NOT NULL,
			side TEXT NOT NULL,
			size REAL NOT NULL,
			entry_price REAL NOT NULL,
			exit_price REAL NOT NULL,
			realized_pnl REAL NOT NULL,
			leverage INTEGER NOT NULL,
			margin_type TEXT NOT NULL,
			closed_at DATETIME NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS liquidity_snapshots (
			symbol TEXT NOT NULL,
			time INTEGER NOT NULL,
			bids_json TEXT NOT NULL,
			asks_json TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_liquidity_symbol_time ON liquidity_snapshots(symbol, time DESC);`,
		`CREATE TABLE IF NOT EXISTS trade_session_logs (
			id TEXT PRIMARY KEY,
			symbol TEXT NOT NULL,
			start_time INTEGER NOT NULL,
			end_time INTEGER NOT NULL,
			ticks_json TEXT NOT NULL
		);`,
	}

	for _, q := range queries {
		if _, err := s.db.Exec(q); err != nil {
			return fmt.Errorf("failed to exec query %s: %w", q, err)
		}
	}

	// Migration: Add stop_loss_at_base column if it doesn't exist
	// We ignore the error if the column already exists
	_, _ = s.db.Exec(`ALTER TABLE levels ADD COLUMN stop_loss_at_base BOOLEAN NOT NULL DEFAULT 0`)
	_, _ = s.db.Exec(`ALTER TABLE levels ADD COLUMN stop_loss_mode TEXT NOT NULL DEFAULT 'exchange'`)
	_, _ = s.db.Exec(`ALTER TABLE levels ADD COLUMN disable_speed_close BOOLEAN NOT NULL DEFAULT 0`)
	_, _ = s.db.Exec(`ALTER TABLE levels ADD COLUMN max_consecutive_base_closes INTEGER NOT NULL DEFAULT 0`)
	_, _ = s.db.Exec(`ALTER TABLE levels ADD COLUMN base_close_cooldown_ms INTEGER NOT NULL DEFAULT 0`)
	_, _ = s.db.Exec(`ALTER TABLE levels ADD COLUMN take_profit_pct REAL NOT NULL DEFAULT 0.02`)
	_, _ = s.db.Exec(`ALTER TABLE levels ADD COLUMN take_profit_mode TEXT NOT NULL DEFAULT 'fixed'`)
	_, _ = s.db.Exec(`ALTER TABLE levels ADD COLUMN is_auto BOOLEAN NOT NULL DEFAULT 0`)
	_, _ = s.db.Exec(`ALTER TABLE levels ADD COLUMN auto_mode_enabled BOOLEAN NOT NULL DEFAULT 0`)
	_, _ = s.db.Exec(`ALTER TABLE trades ADD COLUMN realized_pnl REAL NOT NULL DEFAULT 0`)

	return nil
}

// LevelRepository Implementation

func (s *SQLiteStore) SaveLevel(ctx context.Context, level *domain.Level) error {
	query := `INSERT INTO levels (id, exchange, symbol, level_price, base_size, leverage, margin_type, cool_down_ms, stop_loss_at_base, stop_loss_mode, disable_speed_close, max_consecutive_base_closes, base_close_cooldown_ms, take_profit_pct, take_profit_mode, is_auto, auto_mode_enabled, source, created_at)
			  VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := s.db.ExecContext(ctx, query,
		level.ID, level.Exchange, level.Symbol, level.LevelPrice, level.BaseSize,
		level.Leverage, level.MarginType, level.CoolDownMs, level.StopLossAtBase, level.StopLossMode, level.DisableSpeedClose, level.MaxConsecutiveBaseCloses, level.BaseCloseCooldownMs, level.TakeProfitPct, level.TakeProfitMode, level.IsAuto, level.AutoModeEnabled, level.Source, level.CreatedAt)
	return err
}

func (s *SQLiteStore) GetLevel(ctx context.Context, id string) (*domain.Level, error) {
	query := `SELECT id, exchange, symbol, level_price, base_size, leverage, margin_type, cool_down_ms, stop_loss_at_base, stop_loss_mode, disable_speed_close, max_consecutive_base_closes, base_close_cooldown_ms, take_profit_pct, take_profit_mode, is_auto, auto_mode_enabled, source, created_at FROM levels WHERE id = ?`
	row := s.db.QueryRowContext(ctx, query, id)

	var l domain.Level
	err := row.Scan(&l.ID, &l.Exchange, &l.Symbol, &l.LevelPrice, &l.BaseSize, &l.Leverage, &l.MarginType, &l.CoolDownMs, &l.StopLossAtBase, &l.StopLossMode, &l.DisableSpeedClose, &l.MaxConsecutiveBaseCloses, &l.BaseCloseCooldownMs, &l.TakeProfitPct, &l.TakeProfitMode, &l.IsAuto, &l.AutoModeEnabled, &l.Source, &l.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &l, nil
}

func (s *SQLiteStore) ListLevels(ctx context.Context) ([]*domain.Level, error) {
	query := `SELECT id, exchange, symbol, level_price, base_size, leverage, margin_type, cool_down_ms, stop_loss_at_base, stop_loss_mode, disable_speed_close, max_consecutive_base_closes, base_close_cooldown_ms, take_profit_pct, take_profit_mode, is_auto, auto_mode_enabled, source, created_at FROM levels`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var levels []*domain.Level
	for rows.Next() {
		var l domain.Level
		if err := rows.Scan(&l.ID, &l.Exchange, &l.Symbol, &l.LevelPrice, &l.BaseSize, &l.Leverage, &l.MarginType, &l.CoolDownMs, &l.StopLossAtBase, &l.StopLossMode, &l.DisableSpeedClose, &l.MaxConsecutiveBaseCloses, &l.BaseCloseCooldownMs, &l.TakeProfitPct, &l.TakeProfitMode, &l.IsAuto, &l.AutoModeEnabled, &l.Source, &l.CreatedAt); err != nil {
			return nil, err
		}
		levels = append(levels, &l)
	}
	return levels, nil
}

func (s *SQLiteStore) DeleteLevel(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM levels WHERE id = ?", id)
	return err
}

func (s *SQLiteStore) SaveSymbolTiers(ctx context.Context, tiers *domain.SymbolTiers) error {
	query := `INSERT INTO symbol_tiers (exchange, symbol, tier1_pct, tier2_pct, tier3_pct, updated_at)
			  VALUES (?, ?, ?, ?, ?, ?)
			  ON CONFLICT(exchange, symbol) DO UPDATE SET
			  tier1_pct=excluded.tier1_pct,
			  tier2_pct=excluded.tier2_pct,
			  tier3_pct=excluded.tier3_pct,
			  updated_at=excluded.updated_at`
	_, err := s.db.ExecContext(ctx, query,
		tiers.Exchange, tiers.Symbol, tiers.Tier1Pct, tiers.Tier2Pct, tiers.Tier3Pct, tiers.UpdatedAt)
	return err
}

func (s *SQLiteStore) GetSymbolTiers(ctx context.Context, exchange, symbol string) (*domain.SymbolTiers, error) {
	query := `SELECT exchange, symbol, tier1_pct, tier2_pct, tier3_pct, updated_at FROM symbol_tiers WHERE exchange = ? AND symbol = ?`
	row := s.db.QueryRowContext(ctx, query, exchange, symbol)

	var t domain.SymbolTiers
	err := row.Scan(&t.Exchange, &t.Symbol, &t.Tier1Pct, &t.Tier2Pct, &t.Tier3Pct, &t.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// TradeRepository Implementation

func (s *SQLiteStore) SaveTrade(ctx context.Context, order *domain.Order) error {
	query := `INSERT INTO trades (exchange, symbol, level_id, side, size, price, realized_pnl, created_at)
			  VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := s.db.ExecContext(ctx, query,
		order.Exchange, order.Symbol, order.LevelID, order.Side, order.Size, order.Price, order.RealizedPnL, order.CreatedAt)
	return err
}

func (s *SQLiteStore) ListTrades(ctx context.Context, limit int) ([]*domain.Order, error) {
	query := `SELECT exchange, symbol, level_id, side, size, price, realized_pnl, created_at FROM trades ORDER BY id DESC LIMIT ?`
	rows, err := s.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var trades []*domain.Order
	for rows.Next() {
		var o domain.Order
		if err := rows.Scan(&o.Exchange, &o.Symbol, &o.LevelID, &o.Side, &o.Size, &o.Price, &o.RealizedPnL, &o.CreatedAt); err != nil {
			return nil, err
		}
		trades = append(trades, &o)
	}
	return trades, nil
}

func (s *SQLiteStore) SavePositionHistory(ctx context.Context, history *domain.PositionHistory) error {
	query := `INSERT INTO position_history (exchange, symbol, side, size, entry_price, exit_price, realized_pnl, leverage, margin_type, closed_at)
			  VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := s.db.ExecContext(ctx, query,
		history.Exchange, history.Symbol, history.Side, history.Size, history.EntryPrice, history.ExitPrice, history.RealizedPnL, history.Leverage, history.MarginType, history.ClosedAt)
	return err
}

func (s *SQLiteStore) ListPositionHistory(ctx context.Context, limit int) ([]*domain.PositionHistory, error) {
	query := `SELECT id, exchange, symbol, side, size, entry_price, exit_price, realized_pnl, leverage, margin_type, closed_at FROM position_history ORDER BY id DESC LIMIT ?`
	rows, err := s.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var history []*domain.PositionHistory
	for rows.Next() {
		var h domain.PositionHistory
		if err := rows.Scan(&h.ID, &h.Exchange, &h.Symbol, &h.Side, &h.Size, &h.EntryPrice, &h.ExitPrice, &h.RealizedPnL, &h.Leverage, &h.MarginType, &h.ClosedAt); err != nil {
			return nil, err
		}
		history = append(history, &h)
	}
	return history, nil
}
func (s *SQLiteStore) GetLevelsBySymbol(ctx context.Context, symbol string) ([]*domain.Level, error) {
	query := `SELECT id, exchange, symbol, level_price, base_size, leverage, margin_type, cool_down_ms, stop_loss_at_base, stop_loss_mode, disable_speed_close, max_consecutive_base_closes, base_close_cooldown_ms, take_profit_pct, take_profit_mode, is_auto, auto_mode_enabled, source, created_at FROM levels WHERE symbol = ?`
	rows, err := s.db.QueryContext(ctx, query, symbol)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var levels []*domain.Level
	for rows.Next() {
		var l domain.Level
		if err := rows.Scan(
			&l.ID, &l.Exchange, &l.Symbol, &l.LevelPrice, &l.BaseSize, &l.Leverage, &l.MarginType, &l.CoolDownMs,
			&l.StopLossAtBase, &l.StopLossMode, &l.DisableSpeedClose, &l.MaxConsecutiveBaseCloses, &l.BaseCloseCooldownMs,
			&l.TakeProfitPct, &l.TakeProfitMode, &l.IsAuto, &l.AutoModeEnabled, &l.Source, &l.CreatedAt,
		); err != nil {
			return nil, err
		}
		levels = append(levels, &l)
	}
	return levels, nil
}

func (s *SQLiteStore) CountActiveLevels(ctx context.Context, symbol string) (int, error) {
	query := `SELECT COUNT(*) FROM levels WHERE symbol = ?`
	var count int
	err := s.db.QueryRowContext(ctx, query, symbol).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (s *SQLiteStore) SaveLiquiditySnapshot(ctx context.Context, snap *domain.LiquiditySnapshot) error {
	bidsJSON, _ := json.Marshal(snap.Bids)
	asksJSON, _ := json.Marshal(snap.Asks)

	query := `INSERT INTO liquidity_snapshots (symbol, time, bids_json, asks_json) VALUES (?, ?, ?, ?)`
	_, err := s.db.ExecContext(ctx, query, snap.Symbol, snap.Time, string(bidsJSON), string(asksJSON))
	return err
}

func (s *SQLiteStore) ListLiquiditySnapshots(ctx context.Context, symbol string, limit int) ([]*domain.LiquiditySnapshot, error) {
	query := `SELECT symbol, time, bids_json, asks_json FROM liquidity_snapshots WHERE symbol = ? ORDER BY time DESC LIMIT ?`
	rows, err := s.db.QueryContext(ctx, query, symbol, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var snapshots []*domain.LiquiditySnapshot
	for rows.Next() {
		var snap domain.LiquiditySnapshot
		var bidsJSON, asksJSON string
		if err := rows.Scan(&snap.Symbol, &snap.Time, &bidsJSON, &asksJSON); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(bidsJSON), &snap.Bids)
		json.Unmarshal([]byte(asksJSON), &snap.Asks)
		snapshots = append(snapshots, &snap)
	}
	return snapshots, nil
}

func (s *SQLiteStore) SaveTradeSessionLog(ctx context.Context, log *domain.TradeSessionLog) error {
	ticksJSON, err := json.Marshal(log.Ticks)
	if err != nil {
		return fmt.Errorf("failed to marshal ticks: %w", err)
	}

	query := `INSERT INTO trade_session_logs (id, symbol, start_time, end_time, ticks_json) VALUES (?, ?, ?, ?, ?)`
	_, err = s.db.ExecContext(ctx, query, log.ID, log.Symbol, log.StartTime, log.EndTime, string(ticksJSON))
	return err
}

func (s *SQLiteStore) ListTradeSessionLogs(ctx context.Context, symbol string, limit int) ([]*domain.TradeSessionLog, error) {
	query := `SELECT id, symbol, start_time, end_time, json_array_length(ticks_json) FROM trade_session_logs`
	var args []interface{}
	if symbol != "" {
		query += ` WHERE symbol = ?`
		args = append(args, symbol)
	}
	query += ` ORDER BY start_time DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []*domain.TradeSessionLog
	for rows.Next() {
		var l domain.TradeSessionLog
		var tickCount int
		if err := rows.Scan(&l.ID, &l.Symbol, &l.StartTime, &l.EndTime, &tickCount); err != nil {
			return nil, err
		}
		// We can't strictly set l.Ticks to a slice of that length without data,
		// but we can use an internal field or just let the UI handle it if we return it.
		// Since TradeSessionLog.Ticks is []TickData, we'll just mock it or add a Count field.
		// Let's just add a comment or return it in a way the UI can use.
		// Actually, I'll add a 'TickCount' field to the struct for convenience.
		l.Ticks = make([]domain.TickData, tickCount)
		logs = append(logs, &l)
	}
	return logs, nil
}

func (s *SQLiteStore) GetTradeSessionLog(ctx context.Context, id string) (*domain.TradeSessionLog, error) {
	query := `SELECT id, symbol, start_time, end_time, ticks_json FROM trade_session_logs WHERE id = ?`
	row := s.db.QueryRowContext(ctx, query, id)

	var l domain.TradeSessionLog
	var ticksJSON string
	if err := row.Scan(&l.ID, &l.Symbol, &l.StartTime, &l.EndTime, &ticksJSON); err != nil {
		return nil, err
	}

	if err := json.Unmarshal([]byte(ticksJSON), &l.Ticks); err != nil {
		return nil, fmt.Errorf("failed to unmarshal ticks: %w", err)
	}

	return &l, nil
}
