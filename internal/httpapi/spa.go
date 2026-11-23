package httpapi

import (
	"bytes"
	"encoding/json"
	"html"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

const (
	uiNotBuilt     = "The web UI hasn't been built. Run make web."
	immutableCache = "public, max-age=31536000, immutable"
)

// spaHandler serves the embedded UI. A path naming a file in ui is served as
// that file; anything else gets index.html with the base href and runtime
// config injected, so client-side routes survive a reload.
func spaHandler(ui fs.FS, urlBase string) http.Handler {
	baseTag := `<base href="` + html.EscapeString(urlBase+"/") + `">`
	cfg, _ := json.Marshal(map[string]string{"urlBase": urlBase})
	// json.Marshal escapes <, > and & so the value is safe inside a script tag.
	inject := baseTag + "<script>window.__SOFTWARR__=" + string(cfg) + "</script>"

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeError(w, http.StatusMethodNotAllowed, "Method not allowed.")
			return
		}
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name != "" && name != "index.html" {
			if info, err := fs.Stat(ui, name); err == nil && !info.IsDir() {
				data, err := fs.ReadFile(ui, name)
				if err == nil {
					if strings.HasPrefix(name, "assets/") {
						w.Header().Set("Cache-Control", immutableCache)
					}
					http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
					return
				}
			}
		}
		index, err := fs.ReadFile(ui, "index.html")
		if err != nil {
			http.Error(w, uiNotBuilt, http.StatusServiceUnavailable)
			return
		}
		page := string(index)
		if i := strings.Index(page, "<head>"); i >= 0 {
			i += len("<head>")
			page = page[:i] + inject + page[i:]
		} else {
			page = inject + page
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write([]byte(page))
	})
}
