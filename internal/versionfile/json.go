package versionfile

import (
	"bytes"
	"encoding/json"
	"errors"
)

// Errors of the JSON member lookup.
var (
	errNotObject  = errors.New("the top level value is not a JSON object")
	errKeyMissing = errors.New("the version member is missing")
	errNotString  = errors.New("the version member is not a string")
)

// jsonMember locates the top level string member key. It returns the value and
// the byte span of the JSON string literal, so a write can replace only that
// literal and keep the rest of the file untouched.
func jsonMember(data []byte, key string) (string, int, int, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	token, err := dec.Token()
	if err != nil {
		return "", 0, 0, err
	}
	if delim, ok := token.(json.Delim); !ok || delim != '{' {
		return "", 0, 0, errNotObject
	}
	for dec.More() {
		token, err := dec.Token()
		if err != nil {
			return "", 0, 0, err
		}
		name, ok := token.(string)
		if !ok {
			return "", 0, 0, errNotObject
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return "", 0, 0, err
		}
		if name != key {
			continue
		}
		if len(raw) < 2 || raw[0] != '"' {
			return "", 0, 0, errNotString
		}
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return "", 0, 0, err
		}
		end := int(dec.InputOffset())
		return value, end - len(raw), end, nil
	}
	return "", 0, 0, errKeyMissing
}
