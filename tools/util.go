package tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

var errEmptyArgs = fmt.Errorf("unexpected end of JSON input")

func argsEmpty(raw json.RawMessage) bool {
	return len(raw) == 0 || string(raw) == "null"
}

// argsOrEmptyObject coalesces missing/null args to {} for tools whose parameters
// are entirely optional.
func argsOrEmptyObject(raw json.RawMessage) json.RawMessage {
	if argsEmpty(raw) {
		return json.RawMessage("{}")
	}
	return raw
}

// strictDecode unmarshals raw into dst, rejecting unknown JSON fields.
func strictDecode(raw json.RawMessage, dst any) error {
	if argsEmpty(raw) {
		return errEmptyArgs
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("unexpected trailing data after JSON value")
	}
	return nil
}

// decodeParams strict-decodes tool arguments and returns a validation error that
// names unknown fields and points callers at tools/list.
func decodeParams(raw json.RawMessage, dst any, toolName string) error {
	if err := strictDecode(raw, dst); err != nil {
		return formatDecodeError(err, toolName)
	}
	return nil
}

// rejectExtraArgs rejects any non-empty arguments for tools that take none.
func rejectExtraArgs(args json.RawMessage, toolName string) error {
	if argsEmpty(args) {
		return nil
	}
	return decodeParams(args, &struct{}{}, toolName)
}

func requireNonEmpty(fields map[string]string) error {
	for name, val := range fields {
		if strings.TrimSpace(val) == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	return nil
}

func formatDecodeError(err error, toolName string) error {
	msg := err.Error()
	if !strings.Contains(msg, "unknown field") {
		return err
	}
	out := msg + " — call tools/list to refresh your schema."
	return fmt.Errorf("%s", out)
}

// dispatchBatch peeks the "items" field of args.
// If present and non-null, batchFn is called with the raw items array.
// Otherwise singleFn is called with the full args.
func dispatchBatch[T any](
	args json.RawMessage,
	toolName string,
	singleFn func(json.RawMessage) (T, error),
	batchFn func(json.RawMessage) (T, error),
) (T, error) {
	var zero T
	if argsEmpty(args) {
		return zero, errEmptyArgs
	}

	var keys map[string]json.RawMessage
	if err := json.Unmarshal(args, &keys); err != nil {
		return zero, err
	}
	itemsRaw, hasItems := keys["items"]
	if hasItems && len(itemsRaw) > 0 && string(itemsRaw) != "null" {
		var wrap struct {
			Items json.RawMessage `json:"items"`
		}
		if err := decodeParams(args, &wrap, toolName); err != nil {
			return zero, err
		}
		return batchFn(wrap.Items)
	}
	return singleFn(stripNullItemsFromKeys(keys))
}

// stripNullItemsFromKeys removes a null/absent items key from an already-parsed
// arg map and re-marshals for single-mode strict decode.
func stripNullItemsFromKeys(keys map[string]json.RawMessage) json.RawMessage {
	itemsRaw, ok := keys["items"]
	if !ok || len(itemsRaw) == 0 || string(itemsRaw) == "null" {
		delete(keys, "items")
	}
	if len(keys) == 0 {
		return nil
	}
	b, err := json.Marshal(keys)
	if err != nil {
		return nil
	}
	return b
}

// decodeBatchItems strict-decodes each object in a JSON array.
func decodeBatchItems[T any](items json.RawMessage, toolName string) ([]T, error) {
	var rawItems []json.RawMessage
	if err := json.Unmarshal(items, &rawItems); err != nil {
		return nil, err
	}
	out := make([]T, len(rawItems))
	for i, raw := range rawItems {
		if err := decodeParams(raw, &out[i], toolName); err != nil {
			return nil, fmt.Errorf("item %d: %w", i, err)
		}
	}
	return out, nil
}

// trimWithTruncation caps items at limit and reports whether more existed.
func trimWithTruncation[T any](items []T, limit int) ([]T, bool) {
	if limit <= 0 || len(items) <= limit {
		return items, false
	}
	return items[:limit], true
}
