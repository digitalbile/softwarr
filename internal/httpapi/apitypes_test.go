package httpapi

import (
	"os"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/digitalbile/softwarr/internal/downloadclient"
	"github.com/digitalbile/softwarr/internal/health"
	"github.com/digitalbile/softwarr/internal/library"
	"github.com/digitalbile/softwarr/internal/logbuf"
	"github.com/digitalbile/softwarr/internal/notify"
	"github.com/digitalbile/softwarr/internal/pathmap"
	"github.com/digitalbile/softwarr/internal/prowlarr"
	"github.com/digitalbile/softwarr/internal/requests"
	"github.com/digitalbile/softwarr/internal/settings"
)

// apiTypes maps each interface in web/src/api/types.ts to the Go type the API
// encodes for it. Add an entry when you add an interface there.
var apiTypes = map[string]any{
	"User":             sessionUser{},
	"Indexer":          prowlarr.Indexer{},
	"ProwlarrSettings": settings.Prowlarr{},
	"OIDCSettings":     settings.OIDC{},
	"Settings":         settings.Settings{},
	"Release":          prowlarr.Release{},
	"SearchResult":     SearchResult{},
	"Request":          requests.Request{},
	"Activity":         requests.Activity{},
	"LibraryFile":      library.File{},
	"LibraryItem":      library.Item{},
	"Notification":     notify.Notification{},
	"Webhook":          notify.Webhook{},
	"DownloadClient":   downloadclient.Saved{},
	"PathMapping":      pathmap.Mapping{},
	"HealthCheck":      health.Check{},
	"LogEntry":         logbuf.Entry{},
}

// TestAPITypesMatchFrontend checks that each TypeScript interface has the
// same field names as the JSON of its Go type, so the two can't drift apart.
// It doesn't compare field types.
func TestAPITypesMatchFrontend(t *testing.T) {
	src, err := os.ReadFile("../../web/src/api/types.ts")
	if err != nil {
		t.Fatal(err)
	}
	ts := tsInterfaces(string(src))
	for name := range ts {
		if _, ok := apiTypes[name]; !ok {
			t.Errorf("types.ts declares %s, which has no Go type in apiTypes", name)
		}
	}
	for name, v := range apiTypes {
		fields, ok := ts[name]
		if !ok {
			t.Errorf("apiTypes lists %s, which types.ts doesn't declare", name)
			continue
		}
		goFields := jsonFields(reflect.TypeOf(v))
		for _, f := range goFields {
			if !slices.Contains(fields, f) {
				t.Errorf("%s: Go sends %q, which types.ts doesn't declare", name, f)
			}
		}
		for _, f := range fields {
			if !slices.Contains(goFields, f) {
				t.Errorf("%s: types.ts declares %q, which Go doesn't send", name, f)
			}
		}
	}
}

var (
	tsInterface = regexp.MustCompile(`(?s)export interface (\w+)(?: extends (\w+))? \{(.*?)\n\}`)
	tsField     = regexp.MustCompile(`(?m)^\s+(\w+)\??:`)
)

// tsInterfaces returns the field names of each interface, including those it
// extends.
func tsInterfaces(src string) map[string][]string {
	out := map[string][]string{}
	var extends [][2]string
	for _, m := range tsInterface.FindAllStringSubmatch(src, -1) {
		var fields []string
		for _, f := range tsField.FindAllStringSubmatch(m[3], -1) {
			fields = append(fields, f[1])
		}
		out[m[1]] = fields
		if m[2] != "" {
			extends = append(extends, [2]string{m[1], m[2]})
		}
	}
	for _, e := range extends {
		out[e[0]] = append(out[e[0]], out[e[1]]...)
	}
	return out
}

// jsonFields returns the names encoding/json uses for t's fields, flattening
// embedded structs the way it does.
func jsonFields(t reflect.Type) []string {
	var out []string
	for f := range t.Fields() {
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if f.Anonymous && name == "" && f.Type.Kind() == reflect.Struct {
			out = append(out, jsonFields(f.Type)...)
			continue
		}
		if !f.IsExported() {
			continue
		}
		if name == "" {
			name = f.Name
		}
		out = append(out, name)
	}
	return out
}
