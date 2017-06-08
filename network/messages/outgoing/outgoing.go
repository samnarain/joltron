package outgoing

import (
	"encoding/json"
	"errors"

	"github.com/gamejolt/joltron/game/data"
	"github.com/gamejolt/joltron/stream"
)

// OutMsgState is the response for the GetState message
type OutMsgState struct {
	Version      string         `json:"version"`
	State        string         `json:"state"`
	PatcherState int            `json:"patcherState"`
	Pid          int            `json:"pid"`
	IsPaused     bool           `json:"isPaused"`
	IsRunning    bool           `json:"isRunning"`
	Manifest     *data.Manifest `json:"manifest"`
}

// OutMsgUpdate is a message that is broadcasted to all children when the updater has something new to tell em.
type OutMsgUpdate struct {
	Message string      `json:"message"`
	Payload interface{} `json:"payload,omitempty"`
}

// OutMsgProgress is a message that is broadcasted to all children for progress reports.
type OutMsgProgress struct {
	Current int64          `json:"current"`
	Total   int64          `json:"total"`
	Percent int            `json:"percent"`
	Sample  *stream.Sample `json:"sample"`
}

// OutMsgResult is a generic result response
type OutMsgResult struct {
	Success bool   `json:"success"`
	Error   string `json:"err"`
}

//go:generate jsonenums -type=OutKind

// OutKind a type enum for jsonenums (https://github.com/campoy/jsonenums)
type OutKind int

// OutMsg is a dynamic json that handles all incoming messages
// it is unmarshalled automatically to the correct struct
type OutMsg struct {
	Type    OutKind     `json:"type"`
	MsgID   string      `json:"msgId,omitempty"`
	Payload interface{} `json:"payload"`
}

const (
	state OutKind = iota
	update
	progress
	result
)

func wrapMessage(msg interface{}, msgID string) (interface{}, error) {
	var out OutMsg
	switch msg.(type) {
	case *OutMsgState:
		out = OutMsg{Type: state}
	case *OutMsgUpdate:
		out = OutMsg{Type: update}
	case *OutMsgProgress:
		out = OutMsg{Type: progress}
	case *OutMsgResult:
		out = OutMsg{Type: result}
	default:
		return nil, errors.New("Invalid outgoing message type")
	}

	out.Payload = msg
	if msgID != "" {
		out.MsgID = msgID
	}
	return out, nil
}

// EncodeMsg encodes a message and wraps it in the generic envelop type
func EncodeMsg(enc *json.Encoder, msg interface{}, msgID string) error {
	msg, err := wrapMessage(msg, msgID)
	if err != nil {
		return err
	}

	return enc.Encode(msg)
}
