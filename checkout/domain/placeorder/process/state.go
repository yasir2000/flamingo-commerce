package process

import (
	"context"

	"flamingo.me/flamingo-commerce/v3/payment/application"
)

type (
	// State interface
	State interface {
		Run(context.Context, *Process, StateData) RunResult
		Rollback(context.Context, RollbackData) error
		IsFinal() bool
		Name() string
	}

	// RunResult of a state
	RunResult struct {
		RollbackData RollbackData
		Failed       FailedReason
	}

	// PaymentValidatorFunc to decide over next state depending on payment situation
	PaymentValidatorFunc func(ctx context.Context, p *Process, paymentService *application.PaymentService) RunResult
)
