package webassets

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func assetFixture(t *testing.T) (fstest.MapFS, string) {
	t.Helper()
	script := []byte("export const fixture = true;")
	digest := sha256.Sum256(script)
	hash := hex.EncodeToString(digest[:])
	name := "assets/app." + hash[:8] + ".js"
	files := fstest.MapFS{"index.html": {Data: []byte("<!doctype html><title>Backend routing fixture</title>")}, name: {Data: script}}
	metadata := manifest{Format: 1, Mode: "production", Files: map[string]asset{}, Routes: []string{"/app/", "/app/jobs/:id"}}
	for path, file := range files {
		sum := sha256.Sum256(file.Data)
		metadata.Files[path] = asset{Size: len(file.Data), SHA256: hex.EncodeToString(sum[:]), Immutable: path != "index.html"}
	}
	data, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	files["manifest.json"] = &fstest.MapFile{Data: data}
	return files, name
}

func TestAssetsValidateAndConfineNavigationFallback(t *testing.T) {
	files, script := assetFixture(t)
	bundle, err := Load(files)
	if err != nil {
		t.Fatal(err)
	}
	handler := GuardPaths(bundle)
	for _, test := range []struct {
		method, path, accept string
		status               int
	}{
		{"GET", "/", "text/html", 307}, {"GET", "/app/", "text/html", 200}, {"HEAD", "/app/jobs/job_1", "text/html", 200},
		{"GET", "/app/jobs/job_1", "application/json", 404}, {"GET", "/app/unknown", "text/html", 404},
		{"GET", "/app/jobs/job_1", "application/json, text/html;q=0", 404},
		{"GET", "/app/jobs/job_1", "text/html;q=0.5", 200},
		{"GET", "/app/jobs/job_1", "text/html;q=invalid", 404},
		{"GET", "/app/jobs/job_1", "text/html;q=NaN", 404},
		{"GET", "/app/jobs/job_1", "*/*", 404},
		{"GET", "/app/assets/missing.js", "text/html", 404}, {"POST", "/app/", "text/html", 405},
		{"GET", "/v1/unknown", "text/html", 404}, {"GET", "/mcp/unknown", "text/html", 404}, {"GET", "/exa", "text/html", 404},
		{"GET", "/app/%2e%2e/v1", "text/html", 400}, {"GET", "/app/foo%2fbar", "text/html", 400}, {"GET", "/app/../v1", "text/html", 400},
		{"GET", "/app/" + script, "*/*", 200},
	} {
		request := httptest.NewRequest(test.method, test.path, nil)
		request.Header.Set("Accept", test.accept)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != test.status {
			t.Errorf("%s %s: %d %s", test.method, test.path, response.Code, response.Body.String())
		}
		if test.status >= 400 && !strings.Contains(response.Header().Get("Content-Type"), "json") {
			t.Error("error became HTML")
		}
		if test.method == "HEAD" && response.Body.Len() != 0 {
			t.Error("HEAD returned body")
		}
		if test.path == "/app/"+script && !strings.Contains(response.Header().Get("Cache-Control"), "immutable") {
			t.Error("hashed asset not immutable")
		}
	}
}

func TestAssetsRejectCorruptionAndBackendOnlyCannotClaimUI(t *testing.T) {
	files, script := assetFixture(t)
	files[script].Data = []byte("corrupted")
	if _, err := Load(files); err == nil {
		t.Fatal("accepted corrupted asset")
	}
	files, _ = assetFixture(t)
	files["unexpected"] = &fstest.MapFile{Data: []byte("hidden")}
	if _, err := Load(files); err == nil {
		t.Fatal("accepted unmanifested asset")
	}
	bundle, err := Embedded()
	if err != nil || bundle.Available() {
		t.Fatalf("paused frontend claimed ready: %v", err)
	}
	response := httptest.NewRecorder()
	bundle.ServeHTTP(response, httptest.NewRequest("GET", "/app/", nil))
	if response.Code != 404 {
		t.Fatal("backend-only candidate served placeholder UI")
	}
}
