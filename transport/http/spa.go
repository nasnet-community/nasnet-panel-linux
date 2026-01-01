package http

import (
	"embed"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

// webMIME: explicit MIME map. mime.TypeByExtension reads /etc/mime.types
// which is often incomplete on minimal Linux (.js → text/plain breaks JS modules).
var webMIME = map[string]string{
	".js":    "application/javascript",
	".mjs":   "application/javascript",
	".css":   "text/css",
	".html":  "text/html; charset=utf-8",
	".json":  "application/json",
	".svg":   "image/svg+xml",
	".png":   "image/png",
	".jpg":   "image/jpeg",
	".jpeg":  "image/jpeg",
	".gif":   "image/gif",
	".ico":   "image/x-icon",
	".webp":  "image/webp",
	".woff":  "font/woff",
	".woff2": "font/woff2",
	".ttf":   "font/ttf",
	".eot":   "application/vnd.ms-fontobject",
	".map":   "application/json",
}

// SPAConfig holds the embedded filesystem and runtime config for SPA serving.
type SPAConfig struct {
	DistFS        fs.FS
	RuntimeConfig string
	BasePath      string
}

// ServeSPA registers the embedded SPA file serving on the Gin engine.
// Assets are served under the panel group, while the NoRoute fallback is
// registered on the engine so it catches all unmatched paths.
func ServeSPA(panel *gin.RouterGroup, engine *gin.Engine, webFS embed.FS, basePath string, runtimeConfig string) *SPAConfig {
	log := logger.GetLogger()

	distFS, err := fs.Sub(webFS, "web-panel/dist")
	if err != nil {
		log.Fatalf("failed to access embedded web-panel/dist: %v", err)
	}

	if _, err := fs.ReadFile(distFS, "index.html"); err != nil {
		log.Fatalf("embedded web-panel/dist missing index.html: %v", err)
	}

	// serveEmbeddedFile reads the file from the embedded FS and writes it
	// with the correct MIME type. This avoids http.FileServer which relies
	// on the system's /etc/mime.types for Content-Type detection.
	serveEmbeddedFile := func(c *gin.Context, filePath string) {
		// Clean and normalize the path
		filePath = strings.TrimPrefix(filePath, "/")

		data, err := fs.ReadFile(distFS, filePath)
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}

		ext := filepath.Ext(filePath)
		if ct, ok := webMIME[ext]; ok {
			c.Data(http.StatusOK, ct, data)
		} else {
			c.Data(http.StatusOK, "application/octet-stream", data)
		}
	}

	// Vite-hashed assets get aggressive caching
	panel.GET("/assets/*filepath", func(c *gin.Context) {
		c.Header("Cache-Control", "public, max-age=31536000, immutable")
		serveEmbeddedFile(c, c.Request.URL.Path[len(basePath):])
	})

	// SPA fallback for client-side routing
	engine.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path

		// When basePath is set, only handle paths under it
		if basePath != "" && !strings.HasPrefix(path, basePath) {
			c.Status(http.StatusNotFound)
			return
		}

		if len(path) > 1 && strings.HasSuffix(path, "/") {
			c.Redirect(http.StatusMovedPermanently, strings.TrimRight(path, "/"))
			return
		}

		// Strip basePath before looking up files
		lookupPath := path
		if basePath != "" {
			lookupPath = strings.TrimPrefix(path, basePath)
			if lookupPath == "" {
				lookupPath = "/"
			}
		}

		// Paths with "." are static file requests
		if strings.Contains(lookupPath, ".") {
			serveEmbeddedFile(c, lookupPath)
			return
		}

		c.Header("Content-Type", "text/html; charset=utf-8")
		c.Header("Cache-Control", "no-cache")
		c.Status(http.StatusOK)
		ServeIndexWithConfig(c.Writer, distFS, basePath, runtimeConfig)
	})

	return &SPAConfig{
		DistFS:        distFS,
		RuntimeConfig: runtimeConfig,
		BasePath:      basePath,
	}
}

// ServeIndexWithConfig: index.html from embedded FS with runtime config
// substituted; rewrites Vite "./assets/" to absolute paths so deep-link
// refreshes resolve.
func ServeIndexWithConfig(w http.ResponseWriter, fsys fs.FS, basePath string, config string) {
	data, err := fs.ReadFile(fsys, "index.html")
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	html := strings.Replace(string(data),
		`"__RUNTIME_CONFIG_PLACEHOLDER__"`, config, 1)

	// Rewrite relative asset paths to absolute so they resolve correctly
	// when the browser is on a nested route (e.g. /base/nodes/1).
	prefix := basePath
	if prefix == "" {
		prefix = ""
	}
	html = strings.ReplaceAll(html, `"./assets/`, `"`+prefix+`/assets/`)
	html = strings.ReplaceAll(html, `'./assets/`, `'`+prefix+`/assets/`)

	w.Write([]byte(html))
}
