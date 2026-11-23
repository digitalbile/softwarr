package torrentfile

import (
	"crypto/sha1"
	"encoding/base32"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var update = flag.Bool("update", false, "regenerate testdata/sample.torrent")

const samplePath = "testdata/sample.torrent"

// sampleInfo is the bencoded info dict of the fixture, built by hand with
// fixed values so the expected hash is computed independently of the parser.
func sampleInfo() []byte {
	pieces := strings.Repeat("\x01\x02\x03\x04\x05\x06\x07\x08\x09\x0a\x0b\x0c\x0d\x0e\x0f\x10\x11\x12\x13\x14", 1)
	return []byte(fmt.Sprintf("d6:lengthi12345e4:name10:sample.bin12:piece lengthi16384e6:pieces%d:%se", len(pieces), pieces))
}

func sampleTorrent() []byte {
	return []byte("d8:announce20:http://tracker.test/4:info" + string(sampleInfo()) + "7:comment5:helloe")
}

func sampleHash() string {
	sum := sha1.Sum(sampleInfo())
	return hex.EncodeToString(sum[:])
}

func TestMain(m *testing.M) {
	flag.Parse()
	if *update {
		if err := os.WriteFile(samplePath, sampleTorrent(), 0o644); err != nil {
			panic(err)
		}
	}
	os.Exit(m.Run())
}

func TestSampleFixtureIsCurrent(t *testing.T) {
	b, err := os.ReadFile(samplePath)
	require.NoError(t, err)
	assert.Equal(t, sampleTorrent(), b)
}

func TestInfoHashMatchesKnown(t *testing.T) {
	b, err := os.ReadFile(samplePath)
	require.NoError(t, err)
	got, err := InfoHash(b)
	require.NoError(t, err)
	assert.Equal(t, sampleHash(), got)
	assert.Len(t, got, 40)
}

func TestInfoHashRawBytesNotReencoded(t *testing.T) {
	// Unsorted keys in info must be hashed exactly as written.
	info := "d4:name1:x6:lengthi1ee"
	sum := sha1.Sum([]byte(info))
	got, err := InfoHash([]byte("d4:info" + info + "e"))
	require.NoError(t, err)
	assert.Equal(t, hex.EncodeToString(sum[:]), got)
}

func TestInfoHashNestedValues(t *testing.T) {
	info := "d5:filesld6:lengthi1e4:pathl1:aeee4:name1:xe"
	sum := sha1.Sum([]byte(info))
	got, err := InfoHash([]byte("d1:ali1ei2ee4:info" + info + "1:zd1:ai0eee"))
	require.NoError(t, err)
	assert.Equal(t, hex.EncodeToString(sum[:]), got)
}

func TestInfoHashRejectsGarbage(t *testing.T) {
	deep := strings.Repeat("l", 100) + strings.Repeat("e", 100)
	cases := map[string]string{
		"empty":           "",
		"not bencode":     "hello world",
		"not a dict":      "li1ee",
		"no info":         "d3:fooi1ee",
		"truncated":       "d4:infod4:name1:x",
		"truncated int":   "d4:infoi12",
		"bad int":         "d4:infoiabce",
		"empty int":       "d4:infoiee",
		"bad length":      "d4:infox:abce",
		"length too long": "d4:info99:abce",
		"negative length": "d4:info-1:ae",
		"missing end":     "d4:infod3:fooi1ee",
		"too deep":        "d4:info" + deep + "e",
		"non-string key":  "di1e1:ae",
		"trailing key":    "d4:info1:a3:fooe",
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := InfoHash([]byte(in))
			assert.Error(t, err)
		})
	}
}

func TestParseMagnetHexAndBase32(t *testing.T) {
	h := sampleHash()
	raw, err := hex.DecodeString(h)
	require.NoError(t, err)
	b32 := base32.StdEncoding.EncodeToString(raw)
	require.Len(t, b32, 32)

	for _, uri := range []string{
		"magnet:?xt=urn:btih:" + h + "&dn=sample",
		"magnet:?dn=sample&xt=urn:btih:" + strings.ToUpper(h),
		"magnet:?xt=urn:btih:" + b32,
		"magnet:?xt=urn:btih:" + strings.ToLower(b32),
		"MAGNET:?xt=urn:btih:" + h,
		"magnet:?xt=urn:btmh:1220abcd&xt=urn:btih:" + h,
	} {
		got, err := ParseMagnet(uri)
		require.NoError(t, err, uri)
		assert.Equal(t, h, got, uri)
	}
}

func TestParseMagnetRejects(t *testing.T) {
	for _, uri := range []string{
		"",
		"http://example.com/?xt=urn:btih:" + sampleHash(),
		"magnet:?dn=x",
		"magnet:?xt=urn:btmh:1220abcd",
		"magnet:?xt=urn:btih:short",
		"magnet:?xt=urn:btih:" + strings.Repeat("z", 40),
		"magnet:?xt=urn:btih:" + strings.Repeat("1", 32),
		"magnet:?xt=urn:btih:" + strings.Repeat("a", 39),
	} {
		_, err := ParseMagnet(uri)
		assert.Error(t, err, uri)
	}
}

func FuzzInfoHash(f *testing.F) {
	f.Add(sampleTorrent())
	f.Add([]byte("d4:infod4:name1:xee"))
	f.Add([]byte("d4:info" + strings.Repeat("l", 70)))
	f.Fuzz(func(t *testing.T, b []byte) {
		h, err := InfoHash(b)
		if err == nil {
			assert.Len(t, h, 40)
		}
	})
}
