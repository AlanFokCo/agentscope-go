package model

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestSecretStr_String(t *testing.T) {
	s := NewSecretStr("my-api-key-123")
	if s.String() != "***" {
		t.Errorf("String() = %q, want %q", s.String(), "***")
	}
}

func TestSecretStr_Value(t *testing.T) {
	s := NewSecretStr("my-api-key-123")
	if s.Value() != "my-api-key-123" {
		t.Errorf("Value() = %q, want %q", s.Value(), "my-api-key-123")
	}
}

func TestSecretStr_GoString(t *testing.T) {
	s := NewSecretStr("secret")
	got := fmt.Sprintf("%#v", s)
	if got != "SecretStr{***}" {
		t.Errorf("GoString() = %q, want %q", got, "SecretStr{***}")
	}
}

func TestSecretStr_MarshalJSON(t *testing.T) {
	s := NewSecretStr("real-secret")
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `"***"` {
		t.Errorf("MarshalJSON = %s, want %q", b, `"***"`)
	}
}

func TestSecretStr_MarshalJSON_InStruct(t *testing.T) {
	type Config struct {
		APIKey SecretStr `json:"api_key"`
		Name   string    `json:"name"`
	}
	cfg := Config{APIKey: NewSecretStr("sk-1234"), Name: "test"}
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	expected := `{"api_key":"***","name":"test"}`
	if string(b) != expected {
		t.Errorf("JSON = %s, want %s", b, expected)
	}
}

func TestSecretStr_MarshalText(t *testing.T) {
	s := NewSecretStr("hidden")
	b, err := s.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "***" {
		t.Errorf("MarshalText = %q, want %q", string(b), "***")
	}
}

func TestSecretStr_IsEmpty(t *testing.T) {
	if !NewSecretStr("").IsEmpty() {
		t.Error("empty secret should report IsEmpty=true")
	}
	if NewSecretStr("x").IsEmpty() {
		t.Error("non-empty secret should report IsEmpty=false")
	}
}

func TestSecretStr_Sprintf(t *testing.T) {
	s := NewSecretStr("real-value")
	got := fmt.Sprintf("key=%s", s)
	if got != "key=***" {
		t.Errorf("Sprintf = %q, should not contain real value", got)
	}
}
