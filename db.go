package lnmux

import (
	"errors"
	"time"

	"github.com/bottlepay/lnmux/types"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
)

var (
	// ErrInvoiceAlreadyCanceled is returned when the invoice is already
	// canceled.
	ErrInvoiceAlreadyCanceled = errors.New("invoice already canceled")

	// ErrInvoiceCannotOpen is returned when an attempt is made to move an
	// invoice to the open state.
	ErrInvoiceCannotOpen = errors.New("cannot move invoice to open")

	// ErrInvoiceCannotAccept is returned when an attempt is made to accept
	// an invoice while the invoice is not in the open state.
	ErrInvoiceCannotAccept = errors.New("cannot accept invoice")

	// ErrInvoicePreimageMismatch is returned when the preimage doesn't
	// match the invoice hash.
	ErrInvoicePreimageMismatch = errors.New("preimage does not match")

	// ErrEmptyHTLCSet is returned when attempting to accept or settle and
	// HTLC set that has no HTLCs.
	ErrEmptyHTLCSet = errors.New("cannot settle/accept empty HTLC set")
)

// Invoice is a payment invoice generated by a payee in order to request
// payment for some good or service. The inclusion of invoices within Lightning
// creates a payment work flow for merchants very similar to that of the
// existing financial system within PayPal, etc.  Invoices are added to the
// database when a payment is requested, then can be settled manually once the
// payment is received at the upper layer. For record keeping purposes,
// invoices are never deleted from the database, instead a bit is toggled
// denoting the invoice has been fully settled. Within the database, all
// invoices must have a unique payment hash which is generated by taking the
// sha256 of the payment preimage.
type Invoice struct {
	types.InvoiceCreationData

	Settled bool

	// SettleDate is the exact time the invoice was settled.
	SettleDate time.Time

	// CreationDate is the exact time the invoice was created.
	CreationDate time.Time

	// PaymentRequest is the encoded payment request for this invoice. For
	// spontaneous (keysend) payments, this field will be empty.
	PaymentRequest string
}

func newCircuitKeyFromRPC(key *routerrpc.CircuitKey) types.CircuitKey {
	return types.CircuitKey{
		ChanID: key.ChanId,
		HtlcID: key.HtlcId,
	}
}
