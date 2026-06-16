package domain

import "testing"

func TestPaymentStatusCanTransitionTo(t *testing.T) {
	tests := []struct {
		name string
		from PaymentStatus
		to   PaymentStatus
		want bool
	}{
		{
			name: "pending to processing",
			from: PaymentStatusPending,
			to:   PaymentStatusProcessing,
			want: true,
		},
		{
			name: "pending to rejected",
			from: PaymentStatusPending,
			to:   PaymentStatusRejected,
			want: true,
		},
		{
			name: "processing to completed",
			from: PaymentStatusProcessing,
			to:   PaymentStatusCompleted,
			want: true,
		},
		{
			name: "processing to failed",
			from: PaymentStatusProcessing,
			to:   PaymentStatusFailed,
			want: true,
		},
		{
			name: "pending to completed rejected",
			from: PaymentStatusPending,
			to:   PaymentStatusCompleted,
			want: false,
		},
		{
			name: "processing to rejected rejected",
			from: PaymentStatusProcessing,
			to:   PaymentStatusRejected,
			want: false,
		},
		{
			name: "completed is terminal",
			from: PaymentStatusCompleted,
			to:   PaymentStatusFailed,
			want: false,
		},
		{
			name: "failed is terminal",
			from: PaymentStatusFailed,
			to:   PaymentStatusProcessing,
			want: false,
		},
		{
			name: "rejected is terminal",
			from: PaymentStatusRejected,
			to:   PaymentStatusProcessing,
			want: false,
		},
		{
			name: "same state rejected",
			from: PaymentStatusPending,
			to:   PaymentStatusPending,
			want: false,
		},
		{
			name: "unknown current status rejected",
			from: PaymentStatus("unknown"),
			to:   PaymentStatusPending,
			want: false,
		},
		{
			name: "unknown next status rejected",
			from: PaymentStatusPending,
			to:   PaymentStatus("unknown"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.from.CanTransitionTo(tt.to)
			if got != tt.want {
				t.Fatalf("expected %v -> %v to be %v, got %v", tt.from, tt.to, tt.want, got)
			}
		})
	}
}
