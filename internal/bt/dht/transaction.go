package dht

import (
	"context"
	"errors"
	"fmt"
	"time"

	"yoru/internal/bt/dht/krpc"
)

var TransactionTimeout = errors.New("transaction timed out")

type transaction struct {
	onResponse func(krpc.Msg)
}

func (t *transaction) handleResponse(m krpc.Msg) {
	t.onResponse(m)
}

const defaultMaxQuerySends = 1

func transactionSender(
	ctx context.Context,
	send func() error,
	resendDelay func() time.Duration,
	maxSends int,
) error {
	var delay time.Duration
	sends := 0
	for sends < maxSends {
		select {
		case <-time.After(delay):
			err := send()
			sends++
			if err != nil {
				return fmt.Errorf("send %d: %w", sends, err)
			}
			delay = resendDelay()
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
