package domain

import "time"

type InboundMessage struct {
	Channel   string
	ChatID    string
	SenderID  string
	Content   string
	Media     []string
	Timestamp time.Time
}

type OutboundMessage struct {
	Channel string
	ChatID  string
	Content string
	Format  string // text | markdown | html
}
