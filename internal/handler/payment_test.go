package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/hisyamsk/vaultpay/internal/domain"
	"github.com/hisyamsk/vaultpay/internal/service"
)

type fakePaymentService struct {
	createPaymentFn func(ctx context.Context, req service.CreatePaymentRequest) (*domain.Payment, error)
}

func (f fakePaymentService) CreatePayment(ctx context.Context, req service.CreatePaymentRequest) (*domain.Payment, error) {
	if f.createPaymentFn == nil {
		return nil, errors.New("unexpected create payment call")
	}
	return f.createPaymentFn(ctx, req)
}

func TestCreatePaymentSuccess(t *testing.T) {
	senderID := uuid.New()
	receiverID := uuid.New()
	paymentID := uuid.New()
	description := "rent"

	handler := newTestPaymentHandler(fakePaymentService{
		createPaymentFn: func(ctx context.Context, req service.CreatePaymentRequest) (*domain.Payment, error) {
			if req.Amount != 1000 {
				t.Fatalf("expected amount 1000, got %d", req.Amount)
			}
			if req.SenderID != senderID {
				t.Fatalf("expected sender ID %s, got %s", senderID, req.SenderID)
			}
			if req.ReceiverID != receiverID {
				t.Fatalf("expected receiver ID %s, got %s", receiverID, req.ReceiverID)
			}
			if req.IdempotencyKey != "idem-1" {
				t.Fatalf("expected idempotency key idem-1, got %q", req.IdempotencyKey)
			}
			if req.Description == nil || *req.Description != description {
				t.Fatalf("expected description %q, got %#v", description, req.Description)
			}

			return &domain.Payment{ID: paymentID, Status: domain.PaymentStatusPending}, nil
		},
	})

	body := map[string]any{
		"amount":          1000,
		"sender_id":       senderID.String(),
		"receiver_id":     receiverID.String(),
		"idempotency_key": "idem-1",
		"description":     description,
	}

	rr := performCreatePaymentRequest(handler, "application/json; charset=utf-8", mustJSON(t, body))

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusCreated, rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("expected content type application/json, got %q", got)
	}

	var resp createPaymentResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.PaymentID != paymentID.String() {
		t.Fatalf("expected payment ID %s, got %s", paymentID, resp.PaymentID)
	}
	if resp.Status != string(domain.PaymentStatusPending) {
		t.Fatalf("expected status %s, got %s", domain.PaymentStatusPending, resp.Status)
	}
}

func TestCreatePaymentRejectsInvalidRequestsBeforeService(t *testing.T) {
	senderID := uuid.New()
	receiverID := uuid.New()
	validBody := map[string]any{
		"amount":          1000,
		"sender_id":       senderID.String(),
		"receiver_id":     receiverID.String(),
		"idempotency_key": "idem-1",
	}

	tests := []struct {
		name        string
		contentType string
		body        string
		wantStatus  int
	}{
		{
			name:        "missing content type",
			contentType: "",
			body:        mustJSON(t, validBody),
			wantStatus:  http.StatusUnsupportedMediaType,
		},
		{
			name:        "wrong content type",
			contentType: "text/plain",
			body:        mustJSON(t, validBody),
			wantStatus:  http.StatusUnsupportedMediaType,
		},
		{
			name:        "malformed json",
			contentType: "application/json",
			body:        `{"amount":`,
			wantStatus:  http.StatusBadRequest,
		},
		{
			name:        "unknown field",
			contentType: "application/json",
			body:        `{"amount":1000,"sender_id":"` + senderID.String() + `","receiver_id":"` + receiverID.String() + `","idempotency_key":"idem-1","extra":true}`,
			wantStatus:  http.StatusBadRequest,
		},
		{
			name:        "multiple json objects",
			contentType: "application/json",
			body:        mustJSON(t, validBody) + mustJSON(t, validBody),
			wantStatus:  http.StatusBadRequest,
		},
		{
			name:        "invalid sender id",
			contentType: "application/json",
			body:        `{"amount":1000,"sender_id":"not-a-uuid","receiver_id":"` + receiverID.String() + `","idempotency_key":"idem-1"}`,
			wantStatus:  http.StatusBadRequest,
		},
		{
			name:        "invalid receiver id",
			contentType: "application/json",
			body:        `{"amount":1000,"sender_id":"` + senderID.String() + `","receiver_id":"not-a-uuid","idempotency_key":"idem-1"}`,
			wantStatus:  http.StatusBadRequest,
		},
		{
			name:        "non-positive amount",
			contentType: "application/json",
			body:        `{"amount":0,"sender_id":"` + senderID.String() + `","receiver_id":"` + receiverID.String() + `","idempotency_key":"idem-1"}`,
			wantStatus:  http.StatusBadRequest,
		},
		{
			name:        "idempotency key too long",
			contentType: "application/json",
			body:        `{"amount":1000,"sender_id":"` + senderID.String() + `","receiver_id":"` + receiverID.String() + `","idempotency_key":"` + strings.Repeat("a", 101) + `"}`,
			wantStatus:  http.StatusBadRequest,
		},
		{
			name:        "oversized body",
			contentType: "application/json",
			body:        `{"amount":1000,"sender_id":"` + senderID.String() + `","receiver_id":"` + receiverID.String() + `","idempotency_key":"` + strings.Repeat("a", 65*1024) + `"}`,
			wantStatus:  http.StatusRequestEntityTooLarge,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := newTestPaymentHandler(fakePaymentService{
				createPaymentFn: func(ctx context.Context, req service.CreatePaymentRequest) (*domain.Payment, error) {
					t.Fatal("expected service not to be called")
					return nil, nil
				},
			})

			rr := performCreatePaymentRequest(handler, tt.contentType, tt.body)
			if rr.Code != tt.wantStatus {
				t.Fatalf("expected status %d, got %d body=%s", tt.wantStatus, rr.Code, rr.Body.String())
			}
		})
	}
}

func TestCreatePaymentMapsServiceErrors(t *testing.T) {
	senderID := uuid.New()
	receiverID := uuid.New()

	tests := []struct {
		name           string
		svc            fakePaymentService
		body           string
		wantStatus     int
		wantNotContain string
	}{
		{
			name: "missing idempotency key",
			svc: fakePaymentService{
				createPaymentFn: func(ctx context.Context, req service.CreatePaymentRequest) (*domain.Payment, error) {
					return nil, service.ErrMissingIdempotencyKey
				},
			},
			body:       `{"amount":1000,"sender_id":"` + senderID.String() + `","receiver_id":"` + receiverID.String() + `","idempotency_key":"   "}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "same sender and receiver",
			svc: fakePaymentService{
				createPaymentFn: func(ctx context.Context, req service.CreatePaymentRequest) (*domain.Payment, error) {
					return nil, service.ErrSameSenderAndReceiver
				},
			},
			body:       `{"amount":1000,"sender_id":"` + senderID.String() + `","receiver_id":"` + senderID.String() + `","idempotency_key":"idem-1"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "idempotency key conflict",
			svc: fakePaymentService{
				createPaymentFn: func(ctx context.Context, req service.CreatePaymentRequest) (*domain.Payment, error) {
					return nil, service.ErrIdempotencyKeyConflict
				},
			},
			body:       validCreatePaymentBody(senderID, receiverID),
			wantStatus: http.StatusConflict,
		},
		{
			name: "unexpected repository error",
			svc: fakePaymentService{
				createPaymentFn: func(ctx context.Context, req service.CreatePaymentRequest) (*domain.Payment, error) {
					return nil, errors.New("db unavailable")
				},
			},
			body:           validCreatePaymentBody(senderID, receiverID),
			wantStatus:     http.StatusInternalServerError,
			wantNotContain: "db unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := newTestPaymentHandler(tt.svc)

			rr := performCreatePaymentRequest(handler, "application/json", tt.body)
			if rr.Code != tt.wantStatus {
				t.Fatalf("expected status %d, got %d body=%s", tt.wantStatus, rr.Code, rr.Body.String())
			}
			if tt.wantNotContain != "" && strings.Contains(rr.Body.String(), tt.wantNotContain) {
				t.Fatalf("expected response body not to contain %q, got %s", tt.wantNotContain, rr.Body.String())
			}
		})
	}
}

func newTestPaymentHandler(svc paymentService) *paymentHandler {
	return NewPaymentHandler(
		svc,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
}

func performCreatePaymentRequest(handler *paymentHandler, contentType string, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/payments", bytes.NewBufferString(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	rr := httptest.NewRecorder()
	handler.CreatePayment(rr, req)
	return rr
}

func validCreatePaymentBody(senderID uuid.UUID, receiverID uuid.UUID) string {
	return `{"amount":1000,"sender_id":"` + senderID.String() + `","receiver_id":"` + receiverID.String() + `","idempotency_key":"idem-1"}`
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()

	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return string(b)
}
