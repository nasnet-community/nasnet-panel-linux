package http

import (
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestServeIndexWithConfig_NoBasePath(t *testing.T) {
	testFS := fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data: []byte(`<html><head><link href="./assets/style.css"></head><body><script src="./assets/app.js"></script>"__RUNTIME_CONFIG_PLACEHOLDER__"</body></html>`),
		},
	}
	w := httptest.NewRecorder()
	ServeIndexWithConfig(w, testFS, "", `{"basePath":"","appName":"Test"}`)

	body := w.Body.String()
	if strings.Contains(body, "__RUNTIME_CONFIG_PLACEHOLDER__") {
		t.Error("placeholder was not replaced")
	}
	if !strings.Contains(body, `{"basePath":"","appName":"Test"}`) {
		t.Error("runtime config not injected")
	}
	if !strings.Contains(body, `src="/assets/app.js"`) {
		t.Errorf("asset path should be rewritten to absolute root, got: %s", body)
	}
}

func TestServeIndexWithConfig_WithBasePath(t *testing.T) {
	testFS := fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data: []byte(`<html><head><link href="./assets/style.css"></head><body><script src="./assets/app.js"></script>"__RUNTIME_CONFIG_PLACEHOLDER__"</body></html>`),
		},
	}
	w := httptest.NewRecorder()
	ServeIndexWithConfig(w, testFS, "/panel", `{"basePath":"/panel","appName":"Test"}`)

	body := w.Body.String()
	if !strings.Contains(body, `src="/panel/assets/app.js"`) {
		t.Errorf("asset src should be rewritten to basePath-absolute, got: %s", body)
	}
	if !strings.Contains(body, `href="/panel/assets/style.css"`) {
		t.Errorf("asset href should be rewritten to basePath-absolute, got: %s", body)
	}
	if !strings.Contains(body, `{"basePath":"/panel","appName":"Test"}`) {
		t.Error("runtime config not injected")
	}
}
