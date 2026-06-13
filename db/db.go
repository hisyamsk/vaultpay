package db

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
)

func ConnectDB(ctx context.Context, dbURL string) (*pgxpool.Pool, error) {
	db, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return nil, errors.New(fmt.Sprintf("create db pool failed: %s", err.Error()))
	}
	if db == nil {
		return nil, errors.New("create db failed: db is nil")
	}

	if err := db.Ping(ctx); err != nil {
		log.Fatal("ping db failed", "error", err)
	}

	return db, nil
}
