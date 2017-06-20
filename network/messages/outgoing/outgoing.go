package outgoing

import (
	"encoding/json"
	"errors"
	"fmt"

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
	Type    string         `json:"type"`
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

// OutMsg is a dynamic json that handles all outgoing messages
// it is marshalled automatically to the correct struct
type OutMsg struct {
	Type    OutKind     `json:"type"`
	MsgID   string      `json:"msgId,omitempty"`
	Payload interface{} `json:"payload"`
}

// OutMsgResponse is a dynamic json that should be identical to the outgoing messages,
// however it is used to parse them as INCOMING messages. Used when the runner needs to
// communicate with another runner and parse its outgoing messages back.
type OutMsgResponse struct {
	Type    OutKind          `json:"type"`
	MsgID   string           `json:"msgId,omitempty"`
	Payload *json.RawMessage `json:"payload"`
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

	bytes, err := json.Marshal(msg)
	if err == nil {
		fmt.Printf("Sending %s\n", string(bytes))
	}
	return enc.Encode(msg)
}

var outKindHandlers = map[OutKind]func() interface{}{
	state:    func() interface{} { return &OutMsgState{} },
	update:   func() interface{} { return &OutMsgUpdate{} },
	progress: func() interface{} { return &OutMsgProgress{} },
	result:   func() interface{} { return &OutMsgResult{} },
}

// DecodeMsg parses a dynamic json message from a json decoder and returns the payload
func DecodeMsg(dec *json.Decoder) (interface{}, string, error) {
	var raw json.RawMessage
	outMsg := OutMsgResponse{
		Payload: &raw,
	}

	// Decode into the Msg envelop type.
	if err := dec.Decode(&outMsg); err != nil {
		return nil, "", err
	}

	bytes, err := json.Marshal(outMsg)
	if err == nil {
		fmt.Printf("Received %s\n", string(bytes))
	}

	// Parse the payload by looking at the Msg's type
	payload, err := parseMsg(&outMsg)
	if err != nil {
		return nil, "", err
	}

	return payload, outMsg.MsgID, nil
}

func parseMsg(msg *OutMsgResponse) (interface{}, error) {
	if msg == nil {
		return nil, errors.New("Cannot parse empty message")
	}
	fn, ok := outKindHandlers[msg.Type]
	if !ok {
		return nil, errors.New("Unknown message type ")
	}
	payload := fn()
	if err := json.Unmarshal(*msg.Payload, payload); err != nil {
		return nil, err
	}
	return payload, nil
}
