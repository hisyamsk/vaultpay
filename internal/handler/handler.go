package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/hisyamsk/vaultpay/internal/repository"
	"github.com/hisyamsk/vaultpay/internal/service"
	"github.com/jackc/pgx/v5/pgxpool"
)

func GetHandlers(pool *pgxpool.Pool, logger *slog.Logger) []Handler {
	paymentRepo := repository.NewPaymentRepository(pool)
	paymentService := service.NewPaymentService(paymentRepo)

	return []Handler{
		NewHealthHandler(logger),
		NewPaymentHandler(paymentService, logger),
	}
}

func handle(httpMethod string, path string) string {
	return fmt.Sprintf("%s %s", httpMethod, path)
}

func sendErrorResponse(w http.ResponseWriter, statusCode int, err error) {
	body := errorResponse{
		Message: err.Error(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(body)
}

func sendSuccessResponse(w http.ResponseWriter, statusCode int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(body)
}
