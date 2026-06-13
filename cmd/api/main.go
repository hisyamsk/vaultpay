package main

import (
	"context"
	"log"
	"log/slog"
	"os"

	"github.com/hisyamsk/vaultpay/internal/app"
	"github.com/hisyamsk/vaultpay/internal/config"
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatal(err)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx := context.Background()

	api, err := app.NewAPI(ctx, cfg, logger)
	if err != nil {
		log.Fatal(err)
	}

	defer api.Close()
	if err := api.Run(); err != nil {
		log.Fatal(err)
	}
}
