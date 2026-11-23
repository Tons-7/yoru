package ctxrw

import (
	"context"
	"time"
)

type deadliner interface {
	SetDeadline(time.Time) error
}

func Bind(ctx context.Context, rw any) (stop func() bool) {
	conn, ok := rw.(deadliner)
	if !ok || ctx.Done() == nil {
		return func() bool { return false }
	}
	return context.AfterFunc(ctx, func() { conn.SetDeadline(time.Unix(1, 0)) })
}
