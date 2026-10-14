// Package db stores every diagnosis in Postgres so a store's score can be tracked over time.
package db

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Anirudh-Nadipally/store-doctor/internal/rules"
)

const schema = `
CREATE TABLE IF NOT EXISTS diagnoses (
  id            BIGSERIAL PRIMARY KEY,
  handle        TEXT        NOT NULL,
  score         INT         NOT NULL,
  rules_version TEXT        NOT NULL,
  report        JSONB       NOT NULL,
  advice        TEXT,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS diagnoses_handle_created ON diagnoses (handle, created_at DESC);`

type Store struct{ pool *pgxpool.Pool }

func Open(ctx context.Context, url string) (*Store, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	if _, err := pool.Exec(ctx, schema); err != nil {
		pool.Close()
		return nil, err
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() { s.pool.Close() }

func (s *Store) Save(ctx context.Context, r rules.Report, advice string) error {
	js, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO diagnoses (handle, score, rules_version, report, advice) VALUES ($1,$2,$3,$4,$5)`,
		r.Handle, r.Score, r.RulesVersion, js, advice)
	return err
}

type Point struct {
	Score int       `json:"score"`
	At    time.Time `json:"at"`
}

// History returns a store's past scores, newest first.
func (s *Store) History(ctx context.Context, handle string, limit int) ([]Point, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT score, created_at FROM diagnoses WHERE handle=$1 ORDER BY created_at DESC LIMIT $2`, handle, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Point
	for rows.Next() {
		var p Point
		if err := rows.Scan(&p.Score, &p.At); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
