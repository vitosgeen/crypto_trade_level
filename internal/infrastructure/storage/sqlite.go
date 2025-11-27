package storage

import (
	"context"
	"database/sql"
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
			take_profit_pct REAL NOT NULL DEFAULT 2.0,
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
			created_at DATETIME NOT NULL
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
	_, _ = s.db.Exec(`ALTER TABLE levels ADD COLUMN take_profit_pct REAL NOT NULL DEFAULT 2.0`)

	return nil
}

// LevelRepository Implementation

func (s *SQLiteStore) SaveLevel(ctx context.Context, level *domain.Level) error {
	query := `INSERT INTO levels (id, exchange, symbol, level_price, base_size, leverage, margin_type, cool_down_ms, stop_loss_at_base, stop_loss_mode, disable_speed_close, take_profit_pct, source, created_at)
			  VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := s.db.ExecContext(ctx, query,
		level.ID, level.Exchange, level.Symbol, level.LevelPrice, level.BaseSize,
		level.Leverage, level.MarginType, level.CoolDownMs, level.StopLossAtBase, level.StopLossMode, level.DisableSpeedClose, level.TakeProfitPct, level.Source, level.CreatedAt)
	return err
}

func (s *SQLiteStore) GetLevel(ctx context.Context, id string) (*domain.Level, error) {
	query := `SELECT id, exchange, symbol, level_price, base_size, leverage, margin_type, cool_down_ms, stop_loss_at_base, stop_loss_mode, disable_speed_close, take_profit_pct, source, created_at FROM levels WHERE id = ?`
	row := s.db.QueryRowContext(ctx, query, id)

	var l domain.Level
	err := row.Scan(&l.ID, &l.Exchange, &l.Symbol, &l.LevelPrice, &l.BaseSize, &l.Leverage, &l.MarginType, &l.CoolDownMs, &l.StopLossAtBase, &l.StopLossMode, &l.DisableSpeedClose, &l.TakeProfitPct, &l.Source, &l.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &l, nil
}

func (s *SQLiteStore) ListLevels(ctx context.Context) ([]*domain.Level, error) {
	query := `SELECT id, exchange, symbol, level_price, base_size, leverage, margin_type, cool_down_ms, stop_loss_at_base, stop_loss_mode, disable_speed_close, take_profit_pct, source, created_at FROM levels`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var levels []*domain.Level
	for rows.Next() {
		var l domain.Level
		if err := rows.Scan(&l.ID, &l.Exchange, &l.Symbol, &l.LevelPrice, &l.BaseSize, &l.Leverage, &l.MarginType, &l.CoolDownMs, &l.StopLossAtBase, &l.StopLossMode, &l.DisableSpeedClose, &l.TakeProfitPct, &l.Source, &l.CreatedAt); err != nil {
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
	query := `INSERT INTO trades (exchange, symbol, level_id, side, size, price, created_at)
			  VALUES (?, ?, ?, ?, ?, ?, ?)`
	_, err := s.db.ExecContext(ctx, query,
		order.Exchange, order.Symbol, order.LevelID, order.Side, order.Size, order.Price, order.CreatedAt)
	return err
}

func (s *SQLiteStore) ListTrades(ctx context.Context, limit int) ([]*domain.Order, error) {
	query := `SELECT exchange, symbol, level_id, side, size, price, created_at FROM trades ORDER BY id DESC LIMIT ?`
	rows, err := s.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var trades []*domain.Order
	for rows.Next() {
		var o domain.Order
		if err := rows.Scan(&o.Exchange, &o.Symbol, &o.LevelID, &o.Side, &o.Size, &o.Price, &o.CreatedAt); err != nil {
			return nil, err
		}
		trades = append(trades, &o)
	}
	return trades, nil
}
