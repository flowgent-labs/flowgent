package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// decodeStrictJSON enforces one canonical request object: unknown fields and
// trailing JSON values are rejected instead of being silently ignored.
func decodeStrictJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain exactly one JSON value")
	}
	return nil
}
