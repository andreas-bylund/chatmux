package chat

import (
	"context"
	"time"
)

type Message struct {
	Platform string
	Channel  string
	Time     time.Time
	User     string
	Color    string
	Text     string
	Action   bool
}

type Source interface {
	Run(ctx context.Context, out chan<- Message) error
	Label() string
}
