package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"time"
	"vaultpay/db"
	"vaultpay/internal/config"
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatal(fmt.Sprintf("error loading config: %s", err.Error()))
	}

	ctx := context.Background()
	db, err := db.ConnectDB(ctx, cfg.DBUrl)
	if err != nil {
		log.Fatal(fmt.Sprintf("error connecting db: %s", err.Error()))
	}

	defer db.Close()
	ctxPing, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := db.Ping(ctxPing); err != nil {
		logger.Error("ping db failed", "error", err)
		os.Exit(1)
	}
}
