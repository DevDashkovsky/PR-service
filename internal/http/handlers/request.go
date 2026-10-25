package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"

	"pr-service/internal/domain/dto"
)

const (
	maxRequestBody     = 1 << 20
	pullRequestIDField = "pull_request_id"
)

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
	if err := validateRequestFields(raw, dst); err != nil {
		return err
	}
	strict := json.NewDecoder(bytes.NewReader(raw))
	strict.DisallowUnknownFields()
	return strict.Decode(dst)
}

// encoding/json matches struct fields without regard to case, while the API
// contract requires exact property names, including those of nested members.
func validateRequestFields(raw json.RawMessage, dst any) error {
	var allowed []string
	switch dst.(type) {
	case *dto.SetUserActiveRequest:
		allowed = []string{"user_id", "is_active"}
	case *dto.TeamDTO:
		allowed = []string{"team_name", "members"}
	case *dto.CreatePullRequestRequest:
		allowed = []string{pullRequestIDField, "pull_request_name", "author_id"}
	case *dto.MergePullRequestRequest:
		allowed = []string{pullRequestIDField}
	case *dto.ReassignPullRequestRequest:
		allowed = []string{pullRequestIDField, "old_user_id"}
	default:
		return errors.New("unsupported request type")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	if err := validateFieldNames(fields, allowed...); err != nil {
		return err
	}
	switch dst.(type) {
	case *dto.SetUserActiveRequest:
		return requiredBool(fields["is_active"])
	case *dto.TeamDTO:
		var members []map[string]json.RawMessage
		if err := json.Unmarshal(fields["members"], &members); err != nil {
			return err
		}
		if members == nil {
			return errors.New("members must be an array")
		}
		for _, member := range members {
			if err := validateFieldNames(member, "user_id", "username", "is_active"); err != nil {
				return err
			}
			if err := requiredBool(member["is_active"]); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateFieldNames(fields map[string]json.RawMessage, allowed ...string) error {
	for name := range fields {
		if !slices.Contains(allowed, name) {
			return fmt.Errorf("unknown field %q", name)
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
