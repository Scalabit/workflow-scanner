//go:build windows

package checks

import (
	"encoding/json"
	"testing"
)

func TestStatusConstants(t *testing.T) {
	tests := []struct {
		name     string
		status   Status
		expected string
	}{
		{"StatusOK", StatusOK, "OK"},
		{"StatusWarning", StatusWarning, "WARNING"},
		{"StatusCritical", StatusCritical, "CRITICAL"},
		{"StatusError", StatusError, "ERROR"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.status) != tt.expected {
				t.Errorf("got %q, want %q", string(tt.status), tt.expected)
			}
		})
	}
}

func TestResultJSONMarshaling(t *testing.T) {
	t.Run("basic fields", func(t *testing.T) {
		r := Result{
			Name:    "Test Check",
			Status:  StatusOK,
			Message: "Everything is fine",
		}
		b, err := json.Marshal(r)
		if err != nil {
			t.Fatalf("json.Marshal failed: %v", err)
		}

		var decoded map[string]interface{}
		if err := json.Unmarshal(b, &decoded); err != nil {
			t.Fatalf("json.Unmarshal failed: %v", err)
		}

		if decoded["name"] != "Test Check" {
			t.Errorf("name: got %q, want %q", decoded["name"], "Test Check")
		}
		if decoded["status"] != "OK" {
			t.Errorf("status: got %q, want %q", decoded["status"], "OK")
		}
		if decoded["message"] != "Everything is fine" {
			t.Errorf("message: got %q, want %q", decoded["message"], "Everything is fine")
		}
		// Value field should be omitted when nil.
		if _, ok := decoded["value"]; ok {
			t.Error("value field should be omitted when nil")
		}
	})

	t.Run("value field included when set", func(t *testing.T) {
		r := Result{
			Name:    "Check With Value",
			Status:  StatusWarning,
			Message: "Something to note",
			Value:   42,
		}
		b, err := json.Marshal(r)
		if err != nil {
			t.Fatalf("json.Marshal failed: %v", err)
		}

		var decoded map[string]interface{}
		if err := json.Unmarshal(b, &decoded); err != nil {
			t.Fatalf("json.Unmarshal failed: %v", err)
		}

		if _, ok := decoded["value"]; !ok {
			t.Error("value field should be present when set")
		}
	})

	t.Run("all status values marshal correctly", func(t *testing.T) {
		statuses := []Status{StatusOK, StatusWarning, StatusCritical, StatusError}
		for _, s := range statuses {
			r := Result{Name: "x", Status: s, Message: "y"}
			b, err := json.Marshal(r)
			if err != nil {
				t.Errorf("json.Marshal failed for status %q: %v", s, err)
				continue
			}
			var decoded map[string]interface{}
			if err := json.Unmarshal(b, &decoded); err != nil {
				t.Errorf("json.Unmarshal failed for status %q: %v", s, err)
				continue
			}
			if decoded["status"] != string(s) {
				t.Errorf("status round-trip: got %q, want %q", decoded["status"], string(s))
			}
		}
	})

	t.Run("round-trip unmarshaling", func(t *testing.T) {
		original := Result{
			Name:    "Round Trip",
			Status:  StatusCritical,
			Message: "Critical issue",
			Value:   map[string]interface{}{"key": "val"},
		}
		b, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("json.Marshal failed: %v", err)
		}

		var decoded Result
		if err := json.Unmarshal(b, &decoded); err != nil {
			t.Fatalf("json.Unmarshal into Result failed: %v", err)
		}

		if decoded.Name != original.Name {
			t.Errorf("Name: got %q, want %q", decoded.Name, original.Name)
		}
		if decoded.Status != original.Status {
			t.Errorf("Status: got %q, want %q", decoded.Status, original.Status)
		}
		if decoded.Message != original.Message {
			t.Errorf("Message: got %q, want %q", decoded.Message, original.Message)
		}
	})
}
