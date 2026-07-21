package domain

import "github.com/google/uuid"

type ReconciliationDiscrepancyKind string

const (
	ReconciliationCompletedMissingSenderDebit      ReconciliationDiscrepancyKind = "completed_missing_sender_debit"
	ReconciliationCompletedMissingReceiverCredit   ReconciliationDiscrepancyKind = "completed_missing_receiver_credit"
	ReconciliationFailedMissingSenderDebit         ReconciliationDiscrepancyKind = "failed_missing_sender_debit"
	ReconciliationFailedMissingSenderRefund        ReconciliationDiscrepancyKind = "failed_missing_sender_refund"
	ReconciliationPendingUnexpectedLedgerMovement  ReconciliationDiscrepancyKind = "pending_unexpected_ledger_movement"
	ReconciliationRejectedUnexpectedLedgerMovement ReconciliationDiscrepancyKind = "rejected_unexpected_ledger_movement"
	ReconciliationStalePendingPayment              ReconciliationDiscrepancyKind = "stale_pending_payment"
	ReconciliationStaleProcessingPayment           ReconciliationDiscrepancyKind = "stale_processing_payment"
	ReconciliationStaleUnpublishedPaymentEvent     ReconciliationDiscrepancyKind = "stale_unpublished_payment_event"
)

type ReconciliationDiscrepancy struct {
	Kind      ReconciliationDiscrepancyKind `db:"kind" json:"kind"`
	PaymentID uuid.UUID                     `db:"payment_id" json:"payment_id"`
	EventID   *uuid.UUID                    `db:"event_id" json:"event_id,omitempty"`
}
