package httpx

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestDecodeRejectsOversizedBody(t *testing.T) {
	body := `{"name":"` + strings.Repeat("a", 128) + `"}`
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	var target map[string]any
	err := Decode(request, &target, 32)
	var appErr *Error
	if !errors.As(err, &appErr) {
		t.Fatalf("error = %v, want *Error", err)
	}
	if appErr.Status != http.StatusRequestEntityTooLarge || appErr.Code != "PAYLOAD_TOO_LARGE" {
		t.Fatalf("error = %d/%s", appErr.Status, appErr.Code)
	}
}

func TestDecodeRejectsTrailingJSON(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"a":1}{"b":2}`))
	var target map[string]any
	err := Decode(request, &target, 1024)
	var appErr *Error
	if !errors.As(err, &appErr) {
		t.Fatalf("error = %v, want *Error", err)
	}
	if appErr.Status != http.StatusBadRequest || appErr.Code != "INVALID_JSON" {
		t.Fatalf("error = %d/%s", appErr.Status, appErr.Code)
	}
}

func TestWriteErrorDoesNotLeakConstraintName(t *testing.T) {
	recorder := httptest.NewRecorder()
	WriteError(recorder, &pgconn.PgError{Code: "23505", ConstraintName: "secret_unique_constraint"})
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", recorder.Code)
	}
	if body := recorder.Body.String(); strings.Contains(body, "secret_unique_constraint") {
		t.Fatalf("response leaks constraint name: %s", body)
	}
}

func TestDecodeAllowsSingleJSONDocument(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{\"a\":1}\n"))
	var target map[string]any
	if err := Decode(request, &target, 1024); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if BodyInt(target, "a", 0) != 1 {
		t.Fatalf("decoded value = %#v", target)
	}
}
