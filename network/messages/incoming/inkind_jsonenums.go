// generated by jsonenums -type=InKind; DO NOT EDIT

package incoming

import (
	"encoding/json"
	"fmt"
)

var (
	_InKindNameToValue = map[string]InKind{
		"control":         control,
		"state":           state,
		"checkForUpdates": checkForUpdates,
		"updateAvailable": updateAvailable,
		"updateBegin":     updateBegin,
		"updateApply":     updateApply,
	}

	_InKindValueToName = map[InKind]string{
		control:         "control",
		state:           "state",
		checkForUpdates: "checkForUpdates",
		updateAvailable: "updateAvailable",
		updateBegin:     "updateBegin",
		updateApply:     "updateApply",
	}
)

func init() {
	var v InKind
	if _, ok := interface{}(v).(fmt.Stringer); ok {
		_InKindNameToValue = map[string]InKind{
			interface{}(control).(fmt.Stringer).String():         control,
			interface{}(state).(fmt.Stringer).String():           state,
			interface{}(checkForUpdates).(fmt.Stringer).String(): checkForUpdates,
			interface{}(updateAvailable).(fmt.Stringer).String(): updateAvailable,
			interface{}(updateBegin).(fmt.Stringer).String():     updateBegin,
			interface{}(updateApply).(fmt.Stringer).String():     updateApply,
		}
	}
}

// MarshalJSON is generated so InKind satisfies json.Marshaler.
func (r InKind) MarshalJSON() ([]byte, error) {
	if s, ok := interface{}(r).(fmt.Stringer); ok {
		return json.Marshal(s.String())
	}
	s, ok := _InKindValueToName[r]
	if !ok {
		return nil, fmt.Errorf("invalid InKind: %d", r)
	}
	return json.Marshal(s)
}

// UnmarshalJSON is generated so InKind satisfies json.Unmarshaler.
func (r *InKind) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("InKind should be a string, got %s", data)
	}
	v, ok := _InKindNameToValue[s]
	if !ok {
		return fmt.Errorf("invalid InKind %q", s)
	}
	*r = v
	return nil
}
