package middleware

import (
	"context"
	"errors"
	"testing"

	agenterrors "github.com/alanfokco/agentscope-go/v2/pkg/agentscope/errors"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/model"
)

func makeResp(text string) *model.ChatResponse {
	return &model.ChatResponse{
		Content: []message.ContentBlock{message.TextBlock{Text: text}},
		IsLast:  true,
	}
}

func passThroughHandler(resp *model.ChatResponse) ModelCallHandler {
	return func(_ context.Context, _ *ModelCallInput) (*model.ChatResponse, error) {
		return resp, nil
	}
}

func TestGuardrail_Block(t *testing.T) {
	gm := NewGuardrailMiddleware(
		KeywordBlockRule("profanity", "badword", "offensive"),
	)

	// Clean content passes
	resp, err := gm.OnModelCall(context.Background(), &ModelCallInput{},
		passThroughHandler(makeResp("This is fine.")))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}

	// Content with blocked keyword
	_, err = gm.OnModelCall(context.Background(), &ModelCallInput{},
		passThroughHandler(makeResp("This contains a badword.")))
	if err == nil {
		t.Fatal("expected error for blocked content")
	}
	var ae *agenterrors.AgentError
	if !errors.As(err, &ae) {
		t.Fatalf("expected AgentError, got %T", err)
	}
	if ae.Code != "guardrail.blocked" {
		t.Errorf("expected code guardrail.blocked, got %s", ae.Code)
	}
}

func TestGuardrail_Redact(t *testing.T) {
	gm := NewGuardrailMiddleware(
		KeywordRedactRule("secrets", "[REDACTED]", "password", "secret"),
	)

	resp, err := gm.OnModelCall(context.Background(), &ModelCallInput{},
		passThroughHandler(makeResp("The password is 12345.")))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractResponseText(resp)
	if text != "[REDACTED]" {
		t.Errorf("expected [REDACTED], got %q", text)
	}
	if resp.Metadata["guardrail_redacted"] != "secrets" {
		t.Error("expected guardrail_redacted metadata")
	}
}

func TestGuardrail_Warn(t *testing.T) {
	gm := NewGuardrailMiddleware(
		CustomRule("length_warn", GuardrailWarn, func(text string) (bool, string) {
			if len(text) > 10 {
				return true, "long response"
			}
			return false, ""
		}),
	)

	resp, err := gm.OnModelCall(context.Background(), &ModelCallInput{},
		passThroughHandler(makeResp("This is a longer response that exceeds ten characters.")))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	warnings, ok := resp.Metadata["guardrail_warnings"].([]string)
	if !ok || len(warnings) == 0 {
		t.Fatal("expected guardrail_warnings metadata")
	}
	if warnings[0] != "length_warn: long response" {
		t.Errorf("unexpected warning: %s", warnings[0])
	}
}

func TestGuardrail_MaxLength(t *testing.T) {
	gm := NewGuardrailMiddleware(
		MaxLengthRule("max_len", 20, GuardrailBlock),
	)

	_, err := gm.OnModelCall(context.Background(), &ModelCallInput{},
		passThroughHandler(makeResp("This response is way too long and should be blocked.")))
	if err == nil {
		t.Fatal("expected error for max length violation")
	}
}

func TestGuardrail_MultipleRules(t *testing.T) {
	gm := NewGuardrailMiddleware(
		CustomRule("warn_first", GuardrailWarn, func(text string) (bool, string) {
			return true, "always warns"
		}),
		KeywordBlockRule("block_second", "danger"),
	)

	// Warn fires, then block fires
	_, err := gm.OnModelCall(context.Background(), &ModelCallInput{},
		passThroughHandler(makeResp("There is danger ahead.")))
	if err == nil {
		t.Fatal("expected block error despite prior warn")
	}
}

func TestGuardrail_CleanPassThrough(t *testing.T) {
	gm := NewGuardrailMiddleware(
		KeywordBlockRule("test", "forbidden"),
	)

	resp, err := gm.OnModelCall(context.Background(), &ModelCallInput{},
		passThroughHandler(makeResp("Perfectly safe content.")))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if extractResponseText(resp) != "Perfectly safe content." {
		t.Error("content should pass through unchanged")
	}
}
