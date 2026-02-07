package domain

// MessageBus routes messages between channels and the agent.
type MessageBus interface {
	Publish(msg InboundMessage)
	Subscribe() <-chan InboundMessage
	SendOutbound(msg OutboundMessage)
	OnOutbound(channelName string, handler func(OutboundMessage))
	Close()
}
