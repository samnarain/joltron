package incoming

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/gamejolt/joltron/game/data"
)

// InMsgControlCommand is an incoming command to the patch handler to pause, resume or abort the current operation
type InMsgControlCommand struct {
	Command   string            `json:"command"`
	ExtraData map[string]string `json:"extraData,omitempty"`
}

// InMsgState is an incoming command to query the updater's current state. It may be "installing", "uninstalling" or "running"
type InMsgState struct {
	IncludePatchInfo bool `json:"includePatchInfo"`
}

// InMsgCheckForUpdates is an incoming command from a child to check for updates
// The runner will broadcast an update available message (UpdateMsg with Message being "updateAvailable") if it finds a new game build to update to
type InMsgCheckForUpdates struct {
	GameUID     string `json:"gameUID"`
	PlatformURL string `json:"platformURL"`
	AuthToken   string `json:"authToken"`
	Metadata    string `json:"metadata"`
}

// InMsgUpdateBegin is an incoming command to start updating. Must come after an UpdateMetadata message.
// When an update is ready to be applied the runner will broadcast an UpdateMsg with message being "updateReady"
type InMsgUpdateBegin struct{}

// InMsgUpdateApply is an incoming command from a child to apply the update.
// In auto mode this just pauses before re-launching the game.
// In manual mode this pauses before starting extraction. It'll re-launch the game after extraction is
type InMsgUpdateApply struct {
	// Env  map[string]string `json:"env,omitempty"`
	Args []string `json:"args,omitempty"`
}

//go:generate jsonenums -type=InKind

// InKind a type enum for jsonenums (https://github.com/campoy/jsonenums)
type InKind int

// InMsg is a dynamic json that handles all incoming messages
// it is unmarshalled automatically to the correct struct
type InMsg struct {
	Type    InKind           `json:"type"`
	MsgID   string           `json:"msgId,omitempty"`
	Payload *json.RawMessage `json:"payload"`
}

// InMsgRequest is a dynamic json that should be identical to the incoming messages,
// however it is used to marshal them as OUTGOING messages. Used when the runner needs to
// communicate with another runner and send incoming messages out.
type InMsgRequest struct {
	Type    InKind      `json:"type"`
	MsgID   string      `json:"msgId,omitempty"`
	Payload interface{} `json:"payload"`
}

const (
	control InKind = iota
	state
	checkForUpdates
	updateAvailable
	updateBegin
	updateApply
)

var inKindHandlers = map[InKind]func() interface{}{
	control:         func() interface{} { return &InMsgControlCommand{} },
	state:           func() interface{} { return &InMsgState{} },
	checkForUpdates: func() interface{} { return &InMsgCheckForUpdates{} },
	updateAvailable: func() interface{} { return &data.UpdateMetadata{} },
	updateBegin:     func() interface{} { return &InMsgUpdateBegin{} },
	updateApply:     func() interface{} { return &InMsgUpdateApply{} },
}

// DecodeMsg parses a dynamic json message from a json decoder and returns the payload
func DecodeMsg(dec *json.Decoder) (interface{}, string, error) {
	var raw json.RawMessage
	inMsg := InMsg{
		Payload: &raw,
	}

	// Decode into the Msg envelop type.
	if err := dec.Decode(&inMsg); err != nil {
		return nil, "", err
	}

	bytes, err := json.Marshal(inMsg)
	if err == nil {
		fmt.Printf("Received %s\n", string(bytes))
	}

	// Parse the payload by looking at the Msg's type
	payload, err := parseMsg(&inMsg)
	if err != nil {
		return nil, "", err
	}

	return payload, inMsg.MsgID, nil
}

func parseMsg(msg *InMsg) (interface{}, error) {
	if msg == nil {
		return nil, errors.New("Cannot parse empty message")
	}
	fn, ok := inKindHandlers[msg.Type]
	if !ok {
		return nil, errors.New("Unknown message type ")
	}
	payload := fn()
	if err := json.Unmarshal(*msg.Payload, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func wrapMessage(msg interface{}, msgID string) (interface{}, error) {
	var in InMsgRequest
	switch msg.(type) {
	case *InMsgControlCommand:
		in = InMsgRequest{Type: control}
	case *InMsgState:
		in = InMsgRequest{Type: state}
	case *InMsgCheckForUpdates:
		in = InMsgRequest{Type: checkForUpdates}
	case *data.UpdateMetadata:
		in = InMsgRequest{Type: updateAvailable}
	case *InMsgUpdateBegin:
		in = InMsgRequest{Type: updateBegin}
	case *InMsgUpdateApply:
		in = InMsgRequest{Type: updateApply}
	default:
		return nil, errors.New("Invalid outgoing message type")
	}

	in.Payload = msg
	if msgID != "" {
		in.MsgID = msgID
	}
	return in, nil
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
