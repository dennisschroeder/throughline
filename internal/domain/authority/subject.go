package authority

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
)

// CanonicalizeSubject implements Workgraph Canonical JSON v1 for an AuthorizationSubject.
func CanonicalizeSubject(raw []byte) ([]byte, error) {
	if err := rejectDuplicateKeys(raw); err != nil {
		return nil, err
	}

	type wireSubject struct {
		ActionType             string          `json:"action_type"`
		Target                 json.RawMessage `json:"target"`
		Arguments              json.RawMessage `json:"arguments"`
		Scope                  json.RawMessage `json:"scope"`
		Permissions            json.RawMessage `json:"permissions"`
		CredentialRequirements json.RawMessage `json:"credential_requirements"`
		Constraints            json.RawMessage `json:"constraints"`
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	var wire wireSubject
	if err := decoder.Decode(&wire); err != nil {
		return nil, fmt.Errorf("decode authorization subject: %w", err)
	}
	if err := requireEOF(decoder); err != nil {
		return nil, err
	}
	if wire.ActionType == "" {
		return nil, errors.New("authorization subject action_type is required")
	}

	target, err := decodeObject(wire.Target, "target")
	if err != nil {
		return nil, err
	}
	arguments, err := decodeArray(wire.Arguments, "arguments")
	if err != nil {
		return nil, err
	}
	scope, err := decodeObject(wire.Scope, "scope")
	if err != nil {
		return nil, err
	}
	permissions, err := decodeStringSet(wire.Permissions, "permissions")
	if err != nil {
		return nil, err
	}
	credentialRequirements, err := decodeStringSet(wire.CredentialRequirements, "credential_requirements")
	if err != nil {
		return nil, err
	}
	constraints, err := decodeObject(wire.Constraints, "constraints")
	if err != nil {
		return nil, err
	}

	canonical := map[string]any{
		"action_type":             wire.ActionType,
		"arguments":               arguments,
		"constraints":             constraints,
		"credential_requirements": credentialRequirements,
		"permissions":             permissions,
		"scope":                   scope,
		"target":                  target,
	}
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(canonical); err != nil {
		return nil, fmt.Errorf("encode canonical authorization subject: %w", err)
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte{'\n'}), nil
}

func SubjectHash(raw []byte) (string, error) {
	canonical, err := CanonicalizeSubject(raw)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:]), nil
}

func decodeObject(raw json.RawMessage, field string) (map[string]any, error) {
	value, err := decodeJSON(raw, field)
	if err != nil {
		return nil, err
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("authorization subject %s must be an object", field)
	}
	return object, nil
}

func decodeArray(raw json.RawMessage, field string) ([]any, error) {
	value, err := decodeJSON(raw, field)
	if err != nil {
		return nil, err
	}
	array, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("authorization subject %s must be an array", field)
	}
	return array, nil
}

func decodeStringSet(raw json.RawMessage, field string) ([]string, error) {
	value, err := decodeJSON(raw, field)
	if err != nil {
		return nil, err
	}
	values, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("authorization subject %s must be an array", field)
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		text, ok := value.(string)
		if !ok || text == "" {
			return nil, fmt.Errorf("authorization subject %s must contain non-empty strings", field)
		}
		if _, exists := seen[text]; exists {
			return nil, fmt.Errorf("authorization subject %s contains duplicate %q", field, text)
		}
		seen[text] = struct{}{}
		result = append(result, text)
	}
	sort.Strings(result)
	return result, nil
}

func decodeJSON(raw json.RawMessage, field string) (any, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("authorization subject %s is required", field)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode authorization subject %s: %w", field, err)
	}
	if err := requireEOF(decoder); err != nil {
		return nil, err
	}
	return value, nil
}

func requireEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("authorization subject contains trailing JSON")
		}
		return fmt.Errorf("decode trailing authorization subject data: %w", err)
	}
	return nil
}

func rejectDuplicateKeys(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := inspectValue(decoder); err != nil {
		return fmt.Errorf("validate authorization subject keys: %w", err)
	}
	return requireEOF(decoder)
}

func inspectValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("object key is not a string")
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("duplicate object key %q", key)
			}
			seen[key] = struct{}{}
			if err := inspectValue(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	case '[':
		for decoder.More() {
			if err := inspectValue(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	default:
		return fmt.Errorf("unexpected delimiter %q", delimiter)
	}
}
