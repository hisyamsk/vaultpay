package handler

import (
	"log/slog"
	"net/http"
)

type Handler interface {
	RegisterRoutes(mux *http.ServeMux)
}

func GetHandlers(logger *slog.Logger) []Handler {
	return []Handler{
		NewHealthHandler(logger),
	}
}
