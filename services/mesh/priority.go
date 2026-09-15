package mesh

// priority.go — traffic classes for the forwarding buffer.
//
// A relay carries other devices' traffic. With one undifferentiated FIFO
// queue, a burst of media starves control traffic: an ACK or a call signal
// waits behind a voice note, and a live call degrades even though the radio
// has capacity to spare. The mesh analyses require an explicit priority order
// ("prioritise control, text, voice notes and media; avoid queue starvation"),
// so the forwarding buffer is priority-ordered and byte-bounded.
//
// Priority is local queue policy only — it is never carried on the wire, so
// adding it does not change the packet format and stays compatible with peers
// that predate it.

// Priority orders traffic in the forwarding buffer. Lower values are more
// important and are drained first.
type Priority int

const (
	// PriorityControl is protocol traffic: acknowledgements, route control
	// and call signalling. It is drained first and is the last class a
	// congested node may drop.
	PriorityControl Priority = iota
	// PriorityText is chat and group messages.
	PriorityText
	// PriorityVoice is voice notes.
	PriorityVoice
	// PriorityMedia is bulk media. It is dropped first under pressure.
	PriorityMedia
)

// String renders a priority for logs and status output.
func (p Priority) String() string {
	switch p {
	case PriorityControl:
		return "control"
	case PriorityText:
		return "text"
	case PriorityVoice:
		return "voice"
	case PriorityMedia:
		return "media"
	default:
		return "unknown"
	}
}

// PriorityForKind maps a packet kind to its traffic class. Call signalling and
// acknowledgements are control; voice notes and media get their own classes so
// bulk transfer can never crowd out interactive traffic.
func PriorityForKind(k PacketKind) Priority {
	switch k {
	case KindAck, KindCallSignal:
		return PriorityControl
	case KindMessage, KindGroupMessage:
		return PriorityText
	case KindVoiceMessage:
		return PriorityVoice
	default:
		return PriorityMedia
	}
}
