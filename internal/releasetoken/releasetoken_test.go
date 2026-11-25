package releasetoken

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/digitalbile/softwarr/internal/prowlarr"
)

var (
	key = bytes.Repeat([]byte{7}, 32)
	now = time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
)

func sample() prowlarr.Release {
	return prowlarr.Release{
		GUID: "g", Title: "Editor", Protocol: "torrent", Size: 42, InfoHash: "abc",
		DownloadURL: "http://prowlarr.local/1/download?apikey=secret",
		MagnetURL:   "magnet:?xt=urn:btih:abc",
		Categories:  []int{4000},
	}
}

func TestSealOpenRoundTrip(t *testing.T) {
	tok, err := Seal(key, sample(), 5, now)
	require.NoError(t, err)
	assert.NotContains(t, tok, "secret")
	assert.NotContains(t, tok, "=", "base64url without padding")
	raw, err := base64.RawURLEncoding.DecodeString(tok)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "prowlarr.local", "the release is encrypted, not just encoded")

	got, err := Open(key, tok, 5, now.Add(23*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, sample(), got)

	// Each seal uses a fresh nonce.
	again, err := Seal(key, sample(), 5, now)
	require.NoError(t, err)
	assert.NotEqual(t, tok, again)
}

func TestOpenRejectsTampering(t *testing.T) {
	tok, err := Seal(key, sample(), 5, now)
	require.NoError(t, err)
	raw, err := base64.RawURLEncoding.DecodeString(tok)
	require.NoError(t, err)
	raw[len(raw)-1] ^= 1
	_, err = Open(key, base64.RawURLEncoding.EncodeToString(raw), 5, now)
	assert.ErrorIs(t, err, ErrInvalid)

	for _, bad := range []string{"", "not base64!", "abcd", strings.Repeat("A", 10)} {
		_, err = Open(key, bad, 5, now)
		assert.ErrorIs(t, err, ErrInvalid, bad)
	}

	other := bytes.Repeat([]byte{8}, 32)
	_, err = Open(other, tok, 5, now)
	assert.ErrorIs(t, err, ErrInvalid)
}

func TestOpenRejectsOtherUser(t *testing.T) {
	tok, err := Seal(key, sample(), 5, now)
	require.NoError(t, err)
	_, err = Open(key, tok, 6, now)
	assert.ErrorIs(t, err, ErrInvalid)
}

func TestOpenRejectsExpired(t *testing.T) {
	tok, err := Seal(key, sample(), 5, now)
	require.NoError(t, err)
	_, err = Open(key, tok, 5, now.Add(MaxAge+time.Second))
	assert.ErrorIs(t, err, ErrExpired)
	assert.Equal(t, "These search results are out of date. Search again.", ErrExpired.Error())
}
