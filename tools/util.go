package tools

import "encoding/json"

// dispatchBatch peeks the "items" field of args.
// If present and non-null, batchFn is called with the raw items array.
// Otherwise singleFn is called with the full args.
func dispatchBatch[T any](
	args json.RawMessage,
	singleFn func(json.RawMessage) (T, error),
	batchFn func(json.RawMessage) (T, error),
) (T, error) {
	var peek struct {
		Items json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(args, &peek); err != nil {
		var zero T
		return zero, err
	}
	if len(peek.Items) > 0 && string(peek.Items) != "null" {
		return batchFn(peek.Items)
	}
	return singleFn(args)
}
