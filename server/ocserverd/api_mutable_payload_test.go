package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMutableJSONDecodersRejectUnknownFields(t *testing.T) {
	t.Run("optional body", func(t *testing.T) {
		var body struct {
			Known string `json:"known"`
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPatch, "/api/settings",
			strings.NewReader(`{"known":"ok","typo":"must not disappear"}`))
		if decodeJSONBody(rec, req, &body) {
			t.Fatal("unknown field must be refused")
		}
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("required body", func(t *testing.T) {
		var body struct {
			Title string `json:"title"`
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/tasks",
			strings.NewReader(`{"title":"valid value","typo":"must not disappear"}`))
		if decodeJSONBodyRequired(rec, req, &body, "title") {
			t.Fatal("unknown field alongside a required field must be refused")
		}
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestMutableJSONDecoderStillAllowsDeclaredMapContents(t *testing.T) {
	var body struct {
		Inputs map[string]any `json:"inputs"`
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/tasks",
		strings.NewReader(`{"inputs":{"caller_defined":"preserved"}}`))
	if !decodeJSONBody(rec, req, &body) {
		t.Fatalf("declared map contents must remain valid: %d %s", rec.Code, rec.Body.String())
	}
	if got := body.Inputs["caller_defined"]; got != "preserved" {
		t.Fatalf("declared map content = %#v, want preserved", got)
	}
}
