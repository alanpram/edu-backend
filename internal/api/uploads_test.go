package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUploadsHandlerServesFilesWithoutDirectoryListing(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "photo.jpg"), []byte("jpeg-data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	server := &Server{UploadDir: dir}
	handler := server.uploadsHandler()

	for _, path := range []string{"/uploads/", "/uploads/nested/"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("GET %s status = %d, want 404", path, recorder.Code)
		}
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/uploads/photo.jpg", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET file status = %d, want 200", recorder.Code)
	}
	if body := recorder.Body.String(); body != "jpeg-data" {
		t.Fatalf("body = %q", body)
	}
	if cache := recorder.Header().Get("Cache-Control"); !strings.Contains(cache, "immutable") {
		t.Fatalf("Cache-Control = %q", cache)
	}
}
