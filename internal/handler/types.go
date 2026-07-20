package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/hisyamsk/vaultpay/internal/domain"
	"github.com/hisyamsk/vaultpay/internal/service"
)

var errUnsupportedMediaType = errors.New("unsupported media type")
var errMultipleBodyJsonObject = errors.New("body must contain only one json object")
var errInvalidSenderID = errors.New("invalid senderId")
var errInvalidReceiverID = errors.New("invalid receiverId")
var errInvalidAmount = errors.New("amount must be greater than 0")
var errInvalidIdempotencyKeyLength = errors.New("idempotency key length must be less or equal to 100")
var errInvalidPaymentID = errors.New("invalid payment id")
var errPaymentNotFound = errors.New("payment not found")

var errInternalServerError = errors.New("internal server error")

type Handler interface {
	RegisterRoutes(mux *http.ServeMux)
}

type errorResponse struct {
	Message string `json:"message"`
}

type paymentHandler struct {
	service paymentService
	logger  *slog.Logger
}

type createPaymentRequest struct {
	Amount         int64   `json:"amount"`
	SenderID       string  `json:"sender_id"`
	ReceiverID     string  `json:"receiver_id"`
	IdempotencyKey string  `json:"idempotency_key"`
	Description    *string `json:"description,omitempty"`
}

type createPaymentResponse struct {
	PaymentID string `json:"payment_id"`
	Status    string `json:"status"`
}

type getPaymentResponse struct {
	PaymentID   string    `json:"payment_id"`
	Amount      int64     `json:"amount"`
	SenderID    string    `json:"sender_id"`
	ReceiverID  string    `json:"receiver_id"`
	Status      string    `json:"status"`
	ErrorCode   *string   `json:"error_code"`
	Description *string   `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type paymentService interface {
	CreatePayment(ctx context.Context, req service.CreatePaymentRequest) (*domain.Payment, error)
	FindPaymentByID(ctx context.Context, paymentID uuid.UUID) (*domain.Payment, error)
}
