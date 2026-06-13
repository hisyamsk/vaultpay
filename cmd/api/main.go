package main

import (
	"context"
	"fmt"
	"log"
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

	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	pool, err := db.ConnectDB(dbCtx, cfg.DBUrl)
	if err != nil {
		log.Fatal(fmt.Sprintf("error connecting db: %s", err.Error()))
	}
	defer pool.Close()
}
