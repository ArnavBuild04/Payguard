package kafka

// Envelope is the wire shape published to payment.events and read back off it.
type Envelope struct {
	EventID       string `json:"event_id"`
	EventType     string `json:"event_type"`
	AggregateID   string `json:"aggregate_id"`
	PayloadJSON   string `json:"payload_json"`
	OriginalTopic string `json:"original_topic,omitempty"`
}
