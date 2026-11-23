// Package releasetoken seals search results into opaque tokens, so a browser
// can pick a release without ever seeing its download links or being able to
// make one up.
//
// A token is AES-256-GCM over the stored release, the user it was issued to,
// and when. The server opens it on request or approval and uses the sealed
// release, never one written by the client.
package releasetoken

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/digitalbile/softwarr/internal/prowlarr"
)

// MaxAge is how long a search result can be acted on.
const MaxAge = 24 * time.Hour

// clockSkew tolerates a token issued slightly in the future.
const clockSkew = time.Minute

// aad binds tokens to this purpose and format.
var aad = []byte("softwarr release token v1")

var (
	ErrInvalid = errors.New("This search result isn't valid. Search again.")
	ErrExpired = errors.New("These search results are out of date. Search again.")
)

type payload struct {
	Release json.RawMessage `json:"release"`
	UID     int64           `json:"uid"`
	IAT     int64           `json:"iat"`
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Seal returns a token for rel issued to user uid at now. key must be 32 bytes.
func Seal(key []byte, rel prowlarr.Release, uid int64, now time.Time) (string, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	raw, err := prowlarr.MarshalStored(rel)
	if err != nil {
		return "", err
	}
	plain, err := json.Marshal(payload{Release: raw, UID: uid, IAT: now.Unix()})
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize(), gcm.NonceSize()+len(plain)+gcm.Overhead())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(gcm.Seal(nonce, nonce, plain, aad)), nil
}

// Open checks a token and returns its release. It fails with ErrInvalid when
// the token was altered, sealed with another key, or issued to someone other
// than uid, and with ErrExpired when it is older than MaxAge.
func Open(key []byte, token string, uid int64, now time.Time) (prowlarr.Release, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return prowlarr.Release{}, err
	}
	sealed, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(sealed) < gcm.NonceSize()+gcm.Overhead() {
		return prowlarr.Release{}, ErrInvalid
	}
	n := gcm.NonceSize()
	plain, err := gcm.Open(nil, sealed[:n], sealed[n:], aad)
	if err != nil {
		return prowlarr.Release{}, ErrInvalid
	}
	var p payload
	if err := json.Unmarshal(plain, &p); err != nil || p.UID != uid {
		return prowlarr.Release{}, ErrInvalid
	}
	issued := time.Unix(p.IAT, 0)
	if now.Sub(issued) > MaxAge || issued.Sub(now) > clockSkew {
		return prowlarr.Release{}, ErrExpired
	}
	rel, err := prowlarr.UnmarshalStored(p.Release)
	if err != nil {
		return prowlarr.Release{}, ErrInvalid
	}
	return rel, nil
}
