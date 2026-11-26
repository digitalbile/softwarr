package auth

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
)

// Check tests OIDC settings before they're saved, so an admin can find
// problems without signing in. It can't catch a wrong client secret or an
// unregistered redirect URI, which only show up during a real sign-in. The
// error text is meant for admins.
func (o *OIDC) Check(ctx context.Context, externalURL, issuer, clientID string) error {
	issuer = strings.TrimSpace(issuer)
	switch {
	case strings.TrimSpace(externalURL) == "":
		return errors.New("Set the external URL in General settings first.")
	case issuer == "":
		return errors.New("Enter the issuer URL.")
	case strings.TrimSpace(clientID) == "":
		return errors.New("Enter the client ID.")
	}
	// A fresh provider, so a check never replaces the one sign-in uses.
	p, err := oidc.NewProvider(oidc.ClientContext(ctx, o.client), issuer)
	if err != nil {
		return fmt.Errorf("Couldn't load the provider's settings from %s: %v", issuer, err)
	}
	if ep := p.Endpoint(); ep.AuthURL == "" || ep.TokenURL == "" {
		return errors.New("The provider doesn't list an authorization or token endpoint.")
	}
	var meta struct {
		ResponseTypes []string `json:"response_types_supported"`
	}
	if err := p.Claims(&meta); err != nil {
		return fmt.Errorf("Couldn't read the provider's settings: %v", err)
	}
	// Many providers leave the list out; only a list without "code" is wrong.
	if len(meta.ResponseTypes) > 0 && !slices.Contains(meta.ResponseTypes, "code") {
		return errors.New("The provider doesn't support the authorization code flow.")
	}
	return nil
}
