// Package webassets validates embedded assets and confines navigation fallback.
package webassets

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"exta/internal/security"
	"exta/internal/store"
)

//go:embed production.zip
var embedded []byte

type asset struct {
	Size      int    `json:"size"`
	SHA256    string `json:"sha256"`
	Immutable bool   `json:"immutable"`
}

type manifest struct {
	Format       int              `json:"format"`
	Mode         string           `json:"mode"`
	Files        map[string]asset `json:"files"`
	Routes       []string         `json:"routes"`
	BundleSHA256 *string          `json:"bundle_sha256,omitempty"`
}

// Bundle retains only manifest-verified bytes; it never reads adjacent runtime files.
type Bundle struct {
	manifest manifest
	contents map[string][]byte
}

var embeddedOnce sync.Once
var embeddedBundle *Bundle
var embeddedError error

// Embedded returns the immutable build bundle, including an explicit backend-only mode.
func Embedded() (*Bundle, error) {
	embeddedOnce.Do(func() {
		archive, err := zip.NewReader(bytes.NewReader(embedded), int64(len(embedded)))
		if err != nil {
			embeddedError = err
			return
		}
		root, err := fs.Sub(archive, "bundle")
		if err != nil {
			embeddedError = err
			return
		}
		embeddedBundle, embeddedError = Load(root)
	})
	return embeddedBundle, embeddedError
}

// Load validates every declared asset, rejects undeclared files and requires a real UI index.
func Load(source fs.FS) (*Bundle, error) {
	data, err := fs.ReadFile(source, "manifest.json")
	if err != nil {
		return nil, err
	}
	if len(data) > 1<<20 {
		return nil, errors.New("asset manifest exceeds limit")
	}
	result := &Bundle{contents: map[string][]byte{}}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&result.manifest); err != nil {
		return nil, err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return nil, errors.New("invalid asset manifest envelope")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	for name := range fields {
		if strings.EqualFold(name, "bundle_sha256") && name != "bundle_sha256" {
			return nil, errors.New("noncanonical asset bundle digest field")
		}
	}
	if _, present := fields["bundle_sha256"]; present && result.manifest.BundleSHA256 == nil {
		return nil, errors.New("null asset bundle digest")
	}
	if result.manifest.Format != 1 {
		return nil, errors.New("unsupported asset manifest format")
	}
	if result.manifest.BundleSHA256 != nil && (result.manifest.Mode != "production" || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(*result.manifest.BundleSHA256)) {
		return nil, errors.New("invalid asset bundle digest")
	}
	if result.manifest.Mode == "backend-only" {
		if len(result.manifest.Files) != 0 || len(result.manifest.Routes) != 0 {
			return nil, errors.New("backend-only bundle cannot claim UI assets")
		}
	} else if result.manifest.Mode == "production" {
		if _, ok := result.manifest.Files["index.html"]; !ok || len(result.manifest.Routes) == 0 {
			return nil, errors.New("production bundle requires index and navigation routes")
		}
	} else {
		return nil, errors.New("unknown asset mode")
	}
	for _, route := range result.manifest.Routes {
		if !strings.HasPrefix(route, "/app/") || strings.ContainsAny(route, ".\\%?#") || strings.Contains(route, "//") {
			return nil, errors.New("invalid UI navigation route")
		}
		for _, segment := range strings.Split(route, "/") {
			if strings.Contains(segment, ":") && segment != ":id" {
				return nil, errors.New("unsupported navigation parameter")
			}
		}
	}
	total := 0
	assetNames := []string{}
	caseNames := map[string]bool{}
	for name, entry := range result.manifest.Files {
		if !fs.ValidPath(name) || strings.ContainsAny(name, "\\%?#\x00\r\n\t") || name == "manifest.json" || name != "index.html" && !strings.HasPrefix(name, "assets/") {
			return nil, errors.New("asset path is outside the bundle namespace")
		}
		if caseNames[strings.ToLower(name)] {
			return nil, errors.New("case-colliding asset paths")
		}
		caseNames[strings.ToLower(name)] = true
		if entry.Size < 1 || entry.Size > 16<<20 || len(entry.SHA256) != 64 {
			return nil, errors.New("invalid asset size or digest")
		}
		content, err := fs.ReadFile(source, name)
		if err != nil {
			return nil, err
		}
		digest := sha256.Sum256(content)
		if len(content) != entry.Size || hex.EncodeToString(digest[:]) != entry.SHA256 {
			return nil, errors.New("asset integrity mismatch")
		}
		if result.manifest.BundleSHA256 != nil && name != "index.html" {
			prefix := "assets/" + *result.manifest.BundleSHA256 + "/"
			if !strings.HasPrefix(name, prefix) {
				return nil, errors.New("asset outside content-addressed bundle")
			}
			assetNames = append(assetNames, strings.TrimPrefix(name, prefix))
		}
		if entry.Immutable && (name == "index.html" || result.manifest.BundleSHA256 == nil && !strings.Contains(path.Base(name), entry.SHA256[:8])) {
			return nil, errors.New("immutable asset lacks a content hash in its name")
		}
		total += len(content)
		if total > 64<<20 {
			return nil, errors.New("asset bundle exceeds limit")
		}
		result.contents[name] = content
	}
	if result.manifest.BundleSHA256 != nil {
		sort.Strings(assetNames)
		digest := sha256.New()
		for _, name := range assetNames {
			entry := result.manifest.Files["assets/"+*result.manifest.BundleSHA256+"/"+name]
			io.WriteString(digest, name+"\x00"+entry.SHA256+"\n")
		}
		if len(assetNames) == 0 || hex.EncodeToString(digest.Sum(nil)) != *result.manifest.BundleSHA256 {
			return nil, errors.New("asset bundle namespace integrity mismatch")
		}
	}
	err = fs.WalkDir(source, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return errors.New("asset links are forbidden")
		}
		if entry.IsDir() || name == "manifest.json" {
			return nil
		}
		if _, ok := result.contents[name]; !ok {
			return errors.New("unmanifested asset file")
		}
		return nil
	})
	return result, err
}

// Available reports verified production UI availability, never a placeholder claim.
func (bundle *Bundle) Available() bool { return bundle != nil && bundle.manifest.Mode == "production" }

// GuardPaths rejects traversal and encoded separator ambiguity before ServeMux normalizes it.
func GuardPaths(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		value := request.URL.Path
		escaped := strings.ToLower(request.URL.EscapedPath())
		if strings.ContainsAny(value, "\\%\x00") || strings.Contains(escaped, "%2f") || strings.Contains(escaped, "%5c") || strings.Contains(escaped, "%2e") || strings.Contains(value, "//") || strings.TrimSuffix(value, "/") != strings.TrimSuffix(path.Clean(value), "/") {
			security.WriteError(writer, request, store.Reject("invalid_request", 400, "Ambiguous or traversing request path"))
			return
		}
		next.ServeHTTP(writer, request)
	})
}

var localID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

func acceptsHTML(values []string) bool {
	accepted := false
	for _, value := range strings.Split(strings.Join(values, ","), ",") {
		mediaType, parameters, err := mime.ParseMediaType(strings.TrimSpace(value))
		if err != nil || mediaType != "text/html" {
			continue
		}
		if quality, exists := parameters["q"]; exists {
			amount, err := strconv.ParseFloat(quality, 64)
			if err != nil || !(amount > 0 && amount <= 1) {
				return false
			}
		}
		accepted = true
	}
	return accepted
}

func (bundle *Bundle) navigation(value string) bool {
	for _, route := range bundle.manifest.Routes {
		pattern, actual := strings.Split(route, "/"), strings.Split(value, "/")
		if len(pattern) != len(actual) {
			continue
		}
		matches := true
		for index, part := range pattern {
			if part == ":id" {
				matches = matches && localID.MatchString(actual[index])
			} else {
				matches = matches && part == actual[index]
			}
		}
		if matches {
			return true
		}
	}
	return false
}

// ServeHTTP serves only /app assets and explicitly declared HTML navigation patterns.
func (bundle *Bundle) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	fail := func(status int) {
		security.WriteError(writer, request, store.Reject("not_found", status, "Resource not found"))
	}
	if request.URL.Path != "/" && !strings.HasPrefix(request.URL.Path, "/app/") {
		fail(404)
		return
	}
	if request.Method != "GET" && request.Method != "HEAD" {
		writer.Header().Set("Allow", "GET, HEAD")
		security.WriteError(writer, request, store.Reject("invalid_request", 405, "UI resources require GET or HEAD"))
		return
	}
	if !bundle.Available() {
		fail(404)
		return
	}
	if request.URL.Path == "/" {
		http.Redirect(writer, request, "/app/", http.StatusTemporaryRedirect)
		return
	}
	name := strings.TrimPrefix(request.URL.Path, "/app/")
	content, exists := bundle.contents[name]
	if !exists {
		if !acceptsHTML(request.Header.Values("Accept")) || !bundle.navigation(request.URL.Path) {
			fail(404)
			return
		}
		name, content = "index.html", bundle.contents["index.html"]
	}
	entry := bundle.manifest.Files[name]
	writer.Header().Set("Content-Type", mime.TypeByExtension(path.Ext(name)))
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.Header().Set("ETag", `"`+entry.SHA256+`"`)
	writer.Header().Set("Cache-Control", "no-cache")
	if entry.Immutable {
		writer.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	writer.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
	http.ServeContent(writer, request, name, time.Time{}, bytes.NewReader(content))
}
