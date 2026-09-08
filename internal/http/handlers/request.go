package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"pr-service/internal/domain/dto"
)

const maxRequestBody = 1 << 20

func decodeRequest(w http.ResponseWriter, r *http.Request, dst any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBody))
	var raw json.RawMessage
	if err := decoder.Decode(&raw); err != nil {
		return err
	}
	if len(raw) == 0 || raw[0] != '{' {
		return errors.New("expected JSON object")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("expected a single JSON object within 1 MiB")
	}
	strict := json.NewDecoder(bytes.NewReader(raw))
	strict.DisallowUnknownFields()
	if err := strict.Decode(dst); err != nil {
		return err
	}
	switch dst.(type) {
	case *dto.SetUserActiveRequest:
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return err
		}
		return requiredBool(fields["is_active"])
	case *dto.TeamDTO:
		var fields struct {
			Members []map[string]json.RawMessage `json:"members"`
		}
		if err := json.Unmarshal(raw, &fields); err != nil {
			return err
		}
		if fields.Members == nil {
			return errors.New("members must be an array")
		}
		for _, member := range fields.Members {
			if err := requiredBool(member["is_active"]); err != nil {
				return err
			}
		}
	}
	return nil
}

func requiredBool(value json.RawMessage) error {
	if string(value) != "true" && string(value) != "false" {
		return errors.New("is_active must be a boolean")
	}
	return nil
}
