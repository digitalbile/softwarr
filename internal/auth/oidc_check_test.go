package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/oauth2-proxy/mockoidc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// discoveryServer serves a discovery document built from doc, with "issuer"
// set to the server's own URL unless doc sets it.
func discoveryServer(t *testing.T, doc map[string]any) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		out := map[string]any{"issuer": srv.URL}
		for k, v := range doc {
			out[k] = v
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestOIDCCheck(t *testing.T) {
	m, err := mockoidc.Run()
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Shutdown() })

	endpoints := map[string]any{
		"authorization_endpoint": "https://idp.example.com/auth",
		"token_endpoint":         "https://idp.example.com/token",
	}
	idTokenOnly := discoveryServer(t, map[string]any{
		"authorization_endpoint":   "https://idp.example.com/auth",
		"token_endpoint":           "https://idp.example.com/token",
		"response_types_supported": []string{"id_token"},
	})
	noResponseTypes := discoveryServer(t, endpoints)
	noEndpoints := discoveryServer(t, nil)
	mismatch := discoveryServer(t, map[string]any{"issuer": "https://elsewhere.example.com"})
	errorPage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("<html>" + strings.Repeat("x", 2000) + "SECRET</html>"))
	}))
	t.Cleanup(errorPage.Close)
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()

	const ext = "https://soft.example.com"
	o := NewOIDC(nil, nil, nil, nil)
	for _, tc := range []struct {
		name, externalURL, issuer, clientID string
		want                                string // "" means no error; otherwise a substring
	}{
		{"works", ext, m.Issuer(), "client", ""},
		{"no external URL", " ", m.Issuer(), "client", "Set the external URL in General settings first."},
		{"no issuer", ext, "", "client", "Enter the issuer URL."},
		{"no client ID", ext, m.Issuer(), " ", "Enter the client ID."},
		{"issuer mismatch", ext, mismatch.URL, "client", `The provider says its issuer is "https://elsewhere.example.com", but the issuer URL is "` + mismatch.URL + `". They must match exactly.`},
		{"trailing slash", ext, m.Issuer() + "/", "client", `The provider says its issuer is "` + m.Issuer() + `", but the issuer URL is "` + m.Issuer() + `/". They must match exactly.`},
		{"error page", ext, errorPage.URL, "client", "Couldn't load the provider's settings from " + errorPage.URL + ": 404"},
		{"unreachable", ext, closed.URL, "client", "Couldn't load the provider's settings"},
		{"no endpoints", ext, noEndpoints.URL, "client", "The provider doesn't list an authorization or token endpoint."},
		{"no code flow", ext, idTokenOnly.URL, "client", "The provider doesn't support the authorization code flow."},
		{"response types not listed", ext, noResponseTypes.URL, "client", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := o.Check(context.Background(), tc.externalURL, tc.issuer, tc.clientID)
			if tc.want == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
			// Never echo a whole response body back to the admin.
			assert.NotContains(t, err.Error(), "SECRET")
			assert.Less(t, len(err.Error()), 400)
		})
	}
}
