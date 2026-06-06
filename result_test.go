package axi_test

import (
	"strings"
	"testing"

	"go.klarlabs.de/axi"
)

func TestTruncate_ShortStringUnchanged(t *testing.T) {
	out, truncated := axi.Truncate("hello", 10)
	if truncated {
		t.Error("expected truncated=false")
	}
	if out != "hello" {
		t.Errorf("got %q", out)
	}
}

func TestTruncate_ExactLengthUnchanged(t *testing.T) {
	out, truncated := axi.Truncate("hello", 5)
	if truncated {
		t.Error("expected truncated=false for exact-fit string")
	}
	if out != "hello" {
		t.Errorf("got %q", out)
	}
}

func TestTruncate_LongStringGetsHint(t *testing.T) {
	s := strings.Repeat("x", 50)
	out, truncated := axi.Truncate(s, 10)
	if !truncated {
		t.Fatal("expected truncated=true")
	}
	if !strings.HasPrefix(out, strings.Repeat("x", 10)) {
		t.Errorf("expected 10-x prefix, got %q", out)
	}
	if !strings.Contains(out, "50 chars total") {
		t.Errorf("expected size hint with total count, got %q", out)
	}
}

func TestTruncate_MultiByteRunes(t *testing.T) {
	// Each 😀 is 1 rune / 4 bytes.
	s := strings.Repeat("😀", 20)
	out, truncated := axi.Truncate(s, 5)
	if !truncated {
		t.Fatal("expected truncated=true")
	}
	if !strings.HasPrefix(out, strings.Repeat("😀", 5)) {
		t.Errorf("expected 5 emoji prefix, got %q", out)
	}
	if !strings.Contains(out, "20 chars total") {
		t.Errorf("expected size hint, got %q", out)
	}
}

func TestTruncate_NoLimit(t *testing.T) {
	s := "anything"
	out, truncated := axi.Truncate(s, 0)
	if truncated {
		t.Error("zero max should mean no limit")
	}
	if out != s {
		t.Errorf("got %q", out)
	}
}

func TestListActionsResult(t *testing.T) {
	kernel := axi.New()
	kernel.RegisterActionExecutor("exec.echo", &echoExecutor{})
	if err := kernel.RegisterPlugin(&testPlugin{}); err != nil {
		t.Fatalf("register: %v", err)
	}

	result := kernel.ListActionsResult()
	if result.TotalCount != 1 {
		t.Errorf("TotalCount = %d, want 1", result.TotalCount)
	}
	if len(result.Items) != 1 {
		t.Errorf("len(Items) = %d, want 1", len(result.Items))
	}
	if result.Items[0].Name() != "echo" {
		t.Errorf("Items[0].Name = %s", result.Items[0].Name())
	}
}

func TestListCapabilitiesResult_Empty(t *testing.T) {
	kernel := axi.New()
	result := kernel.ListCapabilitiesResult()
	if result.TotalCount != 0 {
		t.Errorf("TotalCount = %d, want 0", result.TotalCount)
	}
	if result.Items == nil {
		t.Error("Items should be non-nil even when empty")
	}
}

func TestListActionSummaries(t *testing.T) {
	kernel := axi.New()
	kernel.RegisterActionExecutor("exec.echo", &echoExecutor{})
	if err := kernel.RegisterPlugin(&testPlugin{}); err != nil {
		t.Fatalf("register: %v", err)
	}

	summaries := kernel.ListActionSummaries()
	if summaries.TotalCount != 1 {
		t.Fatalf("TotalCount = %d, want 1", summaries.TotalCount)
	}
	s := summaries.Items[0]
	if s.Name != "echo" {
		t.Errorf("Name = %s", s.Name)
	}
	if s.Description != "Echoes input" {
		t.Errorf("Description = %s", s.Description)
	}
	if !s.Idempotent {
		t.Error("expected Idempotent=true")
	}
}

func TestListCapabilitySummaries_Empty(t *testing.T) {
	kernel := axi.New()
	summaries := kernel.ListCapabilitySummaries()
	if summaries.TotalCount != 0 {
		t.Errorf("TotalCount = %d, want 0", summaries.TotalCount)
	}
	if summaries.Items == nil {
		t.Error("Items should be non-nil even when empty")
	}
}

func TestListResult_IsEmpty(t *testing.T) {
	kernel := axi.New()

	// Empty kernel.
	r := kernel.ListActionsResult()
	if !r.IsEmpty() {
		t.Error("expected IsEmpty=true for empty kernel")
	}

	// Non-empty kernel.
	kernel.RegisterActionExecutor("exec.echo", &echoExecutor{})
	_ = kernel.RegisterPlugin(&testPlugin{})
	r = kernel.ListActionsResult()
	if r.IsEmpty() {
		t.Error("expected IsEmpty=false after registering an action")
	}
}
