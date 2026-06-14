package rootaika

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestWarningSoundStoreVersionAndSave(t *testing.T) {
	dir := t.TempDir()
	store := newWarningSoundStore(dir)

	if !store.enabled() {
		t.Fatal("store with dir should be enabled")
	}
	if v := store.version(); v != "" {
		t.Fatalf("version with no file = %q, want empty", v)
	}

	if err := store.save(bytes.NewReader([]byte("ID3 fake mp3 bytes"))); err != nil {
		t.Fatalf("save: %v", err)
	}
	v1 := store.version()
	if v1 == "" {
		t.Fatal("version after save is empty")
	}
	if _, err := os.Stat(store.path()); err != nil {
		t.Fatalf("stat saved file: %v", err)
	}

	// A new save with different content yields a different version.
	time.Sleep(1100 * time.Millisecond)
	if err := store.save(bytes.NewReader([]byte("ID3 different bytes here"))); err != nil {
		t.Fatalf("second save: %v", err)
	}
	if store.version() == v1 {
		t.Fatalf("version did not change after re-upload: %q", v1)
	}
}

func TestWarningSoundStoreRejectsEmpty(t *testing.T) {
	store := newWarningSoundStore(t.TempDir())
	if err := store.save(bytes.NewReader(nil)); err == nil {
		t.Fatal("expected error saving empty file")
	}
}

func TestWarningSoundStoreDisabledWithoutDir(t *testing.T) {
	store := newWarningSoundStore("")
	if store.enabled() {
		t.Fatal("empty-dir store should be disabled")
	}
	if store.version() != "" || store.path() != "" {
		t.Fatal("disabled store should report empty version/path")
	}
	if err := store.save(bytes.NewReader([]byte("x"))); err == nil {
		t.Fatal("disabled store should reject save")
	}
}

func TestHandleWarningSoundNotFoundWhenAbsent(t *testing.T) {
	app := testApp(t).WithDataDir(t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/warning-sound", nil)
	req.SetBasicAuth("client", "client")
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestHandleWarningSoundRequiresClientAuth(t *testing.T) {
	app := testApp(t).WithDataDir(t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/warning-sound", nil)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestAdminUploadWarningSoundServesBytesAndBumpsConfigVersion(t *testing.T) {
	app := testApp(t).WithDataDir(t.TempDir())

	before := clientConfigVersion(t, app)

	soundBytes := []byte("ID3 pretend this is an mp3 loop")
	body, contentType := multipartFile(t, "sound", "warning.mp3", soundBytes)
	upload := httptest.NewRequest(http.MethodPost, "/admin/settings/warning-sound", body)
	upload.Header.Set("Content-Type", contentType)
	upload.SetBasicAuth("admin", "admin")
	uploadRec := httptest.NewRecorder()
	app.ServeHTTP(uploadRec, upload)
	if uploadRec.Code != http.StatusSeeOther {
		t.Fatalf("upload status = %d body=%s", uploadRec.Code, uploadRec.Body.String())
	}

	// The MP3 is now served to clients.
	get := httptest.NewRequest(http.MethodGet, "/api/v1/warning-sound", nil)
	get.SetBasicAuth("client", "client")
	getRec := httptest.NewRecorder()
	app.ServeHTTP(getRec, get)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get status = %d", getRec.Code)
	}
	if !bytes.Equal(getRec.Body.Bytes(), soundBytes) {
		t.Fatalf("served bytes mismatch: got %q", getRec.Body.Bytes())
	}
	if ct := getRec.Header().Get("Content-Type"); ct != "audio/mpeg" {
		t.Fatalf("content-type = %q", ct)
	}

	// The new sound changes the client config version so long-pollers wake.
	after := clientConfigVersion(t, app)
	if before == after {
		t.Fatalf("config version did not change after upload: %q", after)
	}
}

func TestClientConfigIncludesWarningSoundVersion(t *testing.T) {
	app := testApp(t).WithDataDir(t.TempDir())
	if err := app.warningSound.save(bytes.NewReader([]byte("ID3 sound"))); err != nil {
		t.Fatalf("save: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/client/config?client_id=client-1", nil)
	req.SetBasicAuth("client", "client")
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp struct {
		WarningSoundVersion string `json:"warning_sound_version"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.WarningSoundVersion == "" {
		t.Fatal("warning_sound_version is empty in client config")
	}
}

// clientConfigVersion fetches the config_version field the server reports for a
// client, used to assert that a sound upload changes it.
func clientConfigVersion(t *testing.T, app *App) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/client/config?client_id=client-1", nil)
	req.SetBasicAuth("client", "client")
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("config status = %d", rec.Code)
	}
	var resp struct {
		ConfigVersion string `json:"config_version"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	return resp.ConfigVersion
}

func multipartFile(t *testing.T, field, filename string, content []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile(field, filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := fw.Write(content); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	return &buf, w.FormDataContentType()
}
