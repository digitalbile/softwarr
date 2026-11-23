// Package torrentfile computes torrent infohashes from .torrent files and
// magnet links.
package torrentfile

import (
	"crypto/sha1"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

const (
	maxDepth = 64
	maxSize  = 32 << 20
)

var errMalformed = errors.New("malformed bencode")

// InfoHash returns the lowercase hex SHA-1 of the bencoded info dict of a
// .torrent file. The hash covers the info bytes exactly as they appear in the
// file; nothing is re-encoded.
func InfoHash(torrent []byte) (string, error) {
	if len(torrent) > maxSize {
		return "", errors.New("torrent file too large")
	}
	if len(torrent) == 0 || torrent[0] != 'd' {
		return "", errors.New("torrent is not a bencoded dictionary")
	}
	pos := 1
	for {
		if pos >= len(torrent) {
			return "", fmt.Errorf("%w: unterminated dictionary", errMalformed)
		}
		if torrent[pos] == 'e' {
			return "", errors.New("torrent has no info dictionary")
		}
		key, next, err := readString(torrent, pos)
		if err != nil {
			return "", err
		}
		end, err := skipValue(torrent, next, 1)
		if err != nil {
			return "", err
		}
		if string(key) == "info" {
			if torrent[next] != 'd' {
				return "", errors.New("torrent info is not a dictionary")
			}
			// Keep scanning so a truncated trailer is still rejected.
			if err := checkRest(torrent, end); err != nil {
				return "", err
			}
			sum := sha1.Sum(torrent[next:end])
			return hex.EncodeToString(sum[:]), nil
		}
		pos = end
	}
}

// checkRest verifies the remaining top-level entries are well formed and the
// dictionary is terminated.
func checkRest(b []byte, pos int) error {
	for {
		if pos >= len(b) {
			return fmt.Errorf("%w: unterminated dictionary", errMalformed)
		}
		if b[pos] == 'e' {
			return nil
		}
		_, next, err := readString(b, pos)
		if err != nil {
			return err
		}
		if pos, err = skipValue(b, next, 1); err != nil {
			return err
		}
	}
}

// skipValue returns the index just past the bencoded value starting at pos.
func skipValue(b []byte, pos, depth int) (int, error) {
	if depth > maxDepth {
		return 0, errors.New("bencode nested too deeply")
	}
	if pos >= len(b) {
		return 0, fmt.Errorf("%w: unexpected end", errMalformed)
	}
	switch c := b[pos]; {
	case c == 'i':
		return skipInt(b, pos)
	case c >= '0' && c <= '9':
		_, next, err := readString(b, pos)
		return next, err
	case c == 'l':
		pos++
		for {
			if pos >= len(b) {
				return 0, fmt.Errorf("%w: unterminated list", errMalformed)
			}
			if b[pos] == 'e' {
				return pos + 1, nil
			}
			var err error
			if pos, err = skipValue(b, pos, depth+1); err != nil {
				return 0, err
			}
		}
	case c == 'd':
		pos++
		for {
			if pos >= len(b) {
				return 0, fmt.Errorf("%w: unterminated dictionary", errMalformed)
			}
			if b[pos] == 'e' {
				return pos + 1, nil
			}
			_, next, err := readString(b, pos)
			if err != nil {
				return 0, err
			}
			if pos, err = skipValue(b, next, depth+1); err != nil {
				return 0, err
			}
		}
	default:
		return 0, fmt.Errorf("%w: unexpected byte %q", errMalformed, c)
	}
}

// skipInt scans i<digits>e starting at pos.
func skipInt(b []byte, pos int) (int, error) {
	i := pos + 1
	if i < len(b) && b[i] == '-' {
		i++
	}
	start := i
	for i < len(b) && b[i] >= '0' && b[i] <= '9' {
		i++
	}
	if i == start || i >= len(b) || b[i] != 'e' {
		return 0, fmt.Errorf("%w: bad integer", errMalformed)
	}
	return i + 1, nil
}

// readString parses <len>:<bytes> at pos and returns the bytes and the index
// just past them.
func readString(b []byte, pos int) ([]byte, int, error) {
	n, i := 0, pos
	for i < len(b) && b[i] >= '0' && b[i] <= '9' {
		n = n*10 + int(b[i]-'0')
		if n > maxSize {
			return nil, 0, fmt.Errorf("%w: string too long", errMalformed)
		}
		i++
	}
	if i == pos || i >= len(b) || b[i] != ':' {
		return nil, 0, fmt.Errorf("%w: bad string length", errMalformed)
	}
	i++
	if n > len(b)-i {
		return nil, 0, fmt.Errorf("%w: string runs past end", errMalformed)
	}
	return b[i : i+n], i + n, nil
}

// ParseMagnet returns the lowercase hex infohash from a magnet link's
// xt=urn:btih: parameter, accepting 40-character hex or 32-character base32.
// Other xt values (such as v2 urn:btmh:) are ignored.
func ParseMagnet(uri string) (string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", fmt.Errorf("invalid magnet link: %w", err)
	}
	if !strings.EqualFold(u.Scheme, "magnet") {
		return "", errors.New("not a magnet link")
	}
	for _, xt := range u.Query()["xt"] {
		if len(xt) < 9 || !strings.EqualFold(xt[:9], "urn:btih:") {
			continue
		}
		if h, ok := decodeBTIH(xt[9:]); ok {
			return h, nil
		}
	}
	return "", errors.New("magnet link has no valid btih infohash")
}

func decodeBTIH(v string) (string, bool) {
	switch len(v) {
	case 40:
		raw, err := hex.DecodeString(v)
		if err != nil {
			return "", false
		}
		return hex.EncodeToString(raw), true
	case 32:
		raw, err := base32.StdEncoding.DecodeString(strings.ToUpper(v))
		if err != nil {
			return "", false
		}
		return hex.EncodeToString(raw), true
	}
	return "", false
}
