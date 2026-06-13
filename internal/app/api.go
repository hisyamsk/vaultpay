package app

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/hisyamsk/vaultpay/db"
	"github.com/hisyamsk/vaultpay/internal/config"
	"github.com/hisyamsk/vaultpay/internal/handler"
	"github.com/jackc/pgx/v5/pgxpool"
)

type API struct {
	server *http.Server
	db     *pgxpool.Pool
	logger *slog.Logger
}

func NewAPI(ctx context.Context, cfg config.Config, logger *slog.Logger) (*API, error) {
	pool, err := db.ConnectDB(ctx, cfg.DBUrl)
	if err != nil {
		return nil, err
	}

	handlers := []handler.Handler{
		handler.NewHealthHandler(logger),
	}

	server := &http.Server{
		Addr:    cfg.HttpAddr,
		Handler: newRouter(handlers...),
	}

	return &API{
		server: server,
		db:     pool,
		logger: logger,
	}, nil
}

func (a *API) Run() error {
	err := a.server.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (a *API) Close() {
	if a.db != nil {
		a.db.Close()
	}
}

func newRouter(handlers ...handler.Handler) http.Handler {
	mux := http.NewServeMux()

	for _, h := range handlers {
		h.RegisterRoutes(mux)
	}

	return mux
}
