package domain

import "time"

type InboundMessage struct {
	Channel           string
	ChatID            string
	SenderID          string
	Content           string
	Media             []string
	AttachmentContent string   // text content from uploaded files (injected into context for agent)
	Timestamp         time.Time
	Provider          string   // optional: override provider for this message
}

type OutboundMessage struct {
	Channel     string
	ChatID      string
	Content     string
	Format      string       // text | markdown | html
	StreamEvent *StreamEvent // optional: for streaming delivery
}
