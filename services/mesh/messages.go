package mesh

// messages.go — application-layer payloads carried inside encrypted mesh
// packets. These are the plaintext bodies that are sealed with the device
// identity key before transmission.

import "encoding/json"

// Message is a 1:1 chat message payload.
type Message struct {
	ConversationID string `json:"conversation_id"`
	Text           string `json:"text"`
	SenderName     string `json:"sender_name,omitempty"`
	SentAt         int64  `json:"sent_at"`
}

// GroupMessage is a group chat message payload.
type GroupMessage struct {
	GroupID    string `json:"group_id"`
	Text       string `json:"text"`
	SenderName string `json:"sender_name,omitempty"`
	SentAt     int64  `json:"sent_at"`
}

// VoiceMessage is a voice-note payload. Audio is a small encrypted blob
// reference (the audio bytes are carried in the payload; here we carry a
// base64/hex reference and duration).
type VoiceMessage struct {
	ConversationID string `json:"conversation_id"`
	AudioRef       string `json:"audio_ref"`
	DurationMs     int    `json:"duration_ms"`
	SenderName     string `json:"sender_name,omitempty"`
	SentAt         int64  `json:"sent_at"`
}

// CallSignal is a call-control message (audio/video/group call signaling).
type CallSignal struct {
	CallID   string `json:"call_id"`
	Type     string `json:"type"` // offer | answer | ice | hangup | join | leave | ring
	Kind     string `json:"kind"` // audio | video | group
	GroupID  string `json:"group_id,omitempty"`
	Payload  string `json:"payload,omitempty"` // SDP / ICE candidate
	FromName string `json:"from_name,omitempty"`
	SentAt   int64  `json:"sent_at"`
}

// MarshalJSON helper for each payload type.
func MarshalMessage(m *Message) ([]byte, error)          { return json.Marshal(m) }
func MarshalGroupMessage(m *GroupMessage) ([]byte, error) { return json.Marshal(m) }
func MarshalVoiceMessage(m *VoiceMessage) ([]byte, error) { return json.Marshal(m) }
func MarshalCallSignal(m *CallSignal) ([]byte, error)     { return json.Marshal(m) }

// Unmarshal helpers.
func UnmarshalMessage(b []byte) (*Message, error)           { var m Message; err := json.Unmarshal(b, &m); return &m, err }
func UnmarshalGroupMessage(b []byte) (*GroupMessage, error) { var m GroupMessage; err := json.Unmarshal(b, &m); return &m, err }
func UnmarshalVoiceMessage(b []byte) (*VoiceMessage, error) { var m VoiceMessage; err := json.Unmarshal(b, &m); return &m, err }
func UnmarshalCallSignal(b []byte) (*CallSignal, error)     { var m CallSignal; err := json.Unmarshal(b, &m); return &m, err }
