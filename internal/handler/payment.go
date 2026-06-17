package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/hisyamsk/vaultpay/internal/service"
)

func NewPaymentHandler(service *service.PaymentService, logger *slog.Logger) *paymentHandler {
	return &paymentHandler{
		service: service,
		logger:  logger,
	}
}

func (h *paymentHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc(handle(http.MethodPost, "/api/v1/payments"), h.CreatePayment)
}

func (h *paymentHandler) CreatePayment(w http.ResponseWriter, r *http.Request) {
	ct := r.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(ct)
	if err != nil || mediaType != "application/json" {
		sendErrorResponse(w, http.StatusUnsupportedMediaType, errUnsupportedMediaType)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	defer r.Body.Close()

	var req createPaymentRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(&req); err != nil {
		var maxBytesErr *http.MaxBytesError
		statusCode := http.StatusBadRequest
		if errors.As(err, &maxBytesErr) {
			statusCode = http.StatusRequestEntityTooLarge
		}
		sendErrorResponse(w, statusCode, err)
		return
	}

	if dec.Decode(&struct{}{}) != io.EOF {
		sendErrorResponse(w, http.StatusBadRequest, errMultipleBodyJsonObject)
		return
	}

	senderID, err := uuid.Parse(req.SenderID)
	if err != nil {
		sendErrorResponse(w, http.StatusBadRequest, errInvalidSenderID)
		return
	}

	receiverID, err := uuid.Parse(req.ReceiverID)
	if err != nil {
		sendErrorResponse(w, http.StatusBadRequest, errInvalidReceiverID)
		return
	}

	if len(req.IdempotencyKey) > 100 {
		sendErrorResponse(w, http.StatusBadRequest, errInvalidIdempotencyKeyLength)
		return
	}

	if req.Amount <= 0 {
		sendErrorResponse(w, http.StatusBadRequest, errInvalidAmount)
		return
	}

	serviceReq := service.CreatePaymentRequest{
		Amount:         req.Amount,
		SenderID:       senderID,
		ReceiverID:     receiverID,
		IdempotencyKey: req.IdempotencyKey,
		Description:    req.Description,
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	p, err := h.service.CreatePayment(ctx, serviceReq)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrIdempotencyKeyConflict):
			sendErrorResponse(w, http.StatusConflict, err)

		case service.IsInvalidCreatePaymentRequest(err):
			sendErrorResponse(w, http.StatusBadRequest, err)

		default:
			h.logger.ErrorContext(ctx, "create payment failed",
				"error", err,
				"sender_id", senderID,
				"receiver_id", receiverID,
				"idempotency_key", req.IdempotencyKey,
			)

			sendErrorResponse(w, http.StatusInternalServerError, errors.New("internal server error"))
		}
		return
	}

	sendSuccessResponse(w, http.StatusCreated, createPaymentResponse{
		PaymentID: p.ID.String(),
		Status:    string(p.Status),
	})
}
