package domain

type LedgerEntryType string

const (
	LedgerEntryTypeDebit  LedgerEntryType = "debit"
	LedgerEntryTypeCredit LedgerEntryType = "credit"
)
