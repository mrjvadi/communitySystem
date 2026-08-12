package ports

import "context"

// PaymentGateway is the interface for payment processing.
// Implementations: ZarinpalGateway, NowPaymentsGateway, CardGateway.
// Swap gateways in main.go without touching business logic.
type PaymentGateway interface {
	// Name returns the gateway identifier (e.g. "zarinpal", "nowpayments", "card").
	Name() string

	// CreatePayment initiates a payment and returns a redirect URL or payment address.
	CreatePayment(ctx context.Context, req PaymentRequest) (*PaymentResponse, error)

	// VerifyPayment verifies a completed payment by its reference/tx ID.
	VerifyPayment(ctx context.Context, refID string) (*VerifyResponse, error)
}

// PaymentRequest is the input for creating a payment.
type PaymentRequest struct {
	Amount      float64
	Currency    string // "IRR", "IRT", "TON", "USDT", etc.
	Description string
	CallbackURL string
	// UserID is stored in metadata for post-payment routing.
	UserID int64
	// OrderID is a unique idempotency key.
	OrderID string
}

// PaymentResponse is returned after creating a payment.
type PaymentResponse struct {
	// PaymentURL is the URL to redirect the user to (for web gateways).
	PaymentURL string
	// Address is the crypto wallet address (for crypto gateways).
	Address string
	// RefID is the gateway-assigned reference ID.
	RefID string
}

// VerifyResponse is returned after verifying a payment.
type VerifyResponse struct {
	RefID    string
	Amount   float64
	Currency string
	Success  bool
}
