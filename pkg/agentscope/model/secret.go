package model

import "encoding/json"

// SecretStr wraps a sensitive string value, redacting it in logs and
// serialization while still providing access to the underlying value via
// the Value method.
type SecretStr struct {
	value string
}

// NewSecretStr creates a new SecretStr wrapping the given value.
func NewSecretStr(value string) SecretStr {
	return SecretStr{value: value}
}

// Value returns the actual secret string.
func (s SecretStr) Value() string {
	return s.value
}

// String returns a redacted placeholder so the secret is never accidentally
// printed in logs or fmt output.
func (s SecretStr) String() string {
	return "***"
}

// GoString returns the redacted placeholder for %#v formatting.
func (s SecretStr) GoString() string {
	return `SecretStr{***}`
}

// MarshalJSON serializes the secret as the redacted string "***".
func (s SecretStr) MarshalJSON() ([]byte, error) {
	return json.Marshal("***")
}

// MarshalText serializes the secret as the redacted string "***".
func (s SecretStr) MarshalText() ([]byte, error) {
	return []byte("***"), nil
}

// IsEmpty returns true if the underlying secret value is empty.
func (s SecretStr) IsEmpty() bool {
	return s.value == ""
}

// UnmarshalJSON deserializes a JSON string into the SecretStr, storing the
// plaintext value internally. This allows SecretStr fields to be populated
// from JSON configuration files. The value is never re-exposed via
// MarshalJSON (which always emits "***").
func (s *SecretStr) UnmarshalJSON(data []byte) error {
	var v string
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	s.value = v
	return nil
}
