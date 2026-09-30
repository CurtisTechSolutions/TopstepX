package realtime

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/CurtisTechSolutions/TopstepX/go/internal/jsonx"
)

// Lenient payload decoding (contract section 11): a malformed payload
// becomes an *UnknownEvent and never affects the connection.

func unknownEvent(target string, args []json.RawMessage, err error) *UnknownEvent {
	return &UnknownEvent{Target: target, Arguments: args, Err: err}
}

// elements returns the members of a JSON array, or the value itself when it
// is an object.
func elements(raw json.RawMessage) ([]json.RawMessage, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, errors.New("empty payload")
	}
	switch raw[0] {
	case '{':
		return []json.RawMessage{raw}, nil
	case '[':
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, err
		}
		return items, nil
	}
	return nil, fmt.Errorf("payload must be an object or an array, got %.32s", raw)
}

// unwrap returns the action and data objects of a user-hub element, which is
// either a bare payload or an {"action": n, "data": ...} envelope whose data
// may itself be an array.
func unwrap(el json.RawMessage) (*int32, []json.RawMessage, error) {
	el = bytes.TrimSpace(el)
	if len(el) == 0 || el[0] != '{' {
		return nil, nil, fmt.Errorf("payload element must be an object, got %.32s", el)
	}
	var members map[string]json.RawMessage
	if err := json.Unmarshal(el, &members); err != nil {
		return nil, nil, err
	}
	data, ok := members["data"]
	if !ok {
		return nil, []json.RawMessage{el}, nil
	}
	var action *int32
	if raw, ok := members["action"]; ok && string(bytes.TrimSpace(raw)) != "null" {
		n, err := jsonx.LenientInt(raw, 32)
		if err != nil {
			return nil, nil, fmt.Errorf("envelope action: %w", err)
		}
		a := int32(n)
		action = &a
	}
	items, err := elements(data)
	if err != nil {
		return nil, nil, fmt.Errorf("envelope data: %w", err)
	}
	return action, items, nil
}

// decodeUserPayload emits one event per payload element of a user-hub
// invocation with arguments (data).
func decodeUserPayload[T any](target string, args []json.RawMessage, build func(action *int32, data T, raw json.RawMessage) Event) []Event {
	if len(args) == 0 {
		return []Event{unknownEvent(target, args, errors.New("missing payload argument"))}
	}
	items, err := elements(args[0])
	if err != nil {
		return []Event{unknownEvent(target, args, err)}
	}
	var out []Event
	for i, item := range items {
		action, datas, err := unwrap(item)
		if err != nil {
			out = append(out, unknownEvent(target, args, fmt.Errorf("element %d: %w", i, err)))
			continue
		}
		for _, raw := range datas {
			var v T
			if err := json.Unmarshal(raw, &v); err != nil {
				out = append(out, unknownEvent(target, args, fmt.Errorf("element %d: %w", i, err)))
				continue
			}
			out = append(out, build(action, v, raw))
		}
	}
	return out
}

// marketArgs splits market-hub arguments (contractId, data).
func marketArgs(args []json.RawMessage) (string, json.RawMessage, error) {
	if len(args) < 2 {
		return "", nil, fmt.Errorf("expected (contractId, data) arguments, got %d", len(args))
	}
	var contractID string
	if err := json.Unmarshal(args[0], &contractID); err != nil {
		return "", nil, fmt.Errorf("contractId: %w", err)
	}
	return contractID, args[1], nil
}

// decodeList decodes a single object or an array into a list.
func decodeList[T any](raw json.RawMessage) ([]T, error) {
	items, err := elements(raw)
	if err != nil {
		return nil, err
	}
	out := make([]T, 0, len(items))
	for i, item := range items {
		var v T
		if err := json.Unmarshal(item, &v); err != nil {
			return nil, fmt.Errorf("element %d: %w", i, err)
		}
		out = append(out, v)
	}
	return out, nil
}
