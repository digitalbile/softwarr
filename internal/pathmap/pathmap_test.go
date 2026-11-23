package pathmap

import (
	"testing"

	"github.com/digitalbile/softwarr/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMapLongestPrefix(t *testing.T) {
	ms := []Mapping{
		{Host: "qbt", RemotePath: "/downloads/", LocalPath: "/data/"},
		{Host: "qbt", RemotePath: "/downloads/complete/", LocalPath: "/media/done/"},
	}
	assert.Equal(t, "/media/done/app/setup.zip", Map(ms, "QBT", "/downloads/complete/app/setup.zip"))
	assert.Equal(t, "/data/other/x", Map(ms, "qbt", "/downloads/other/x"))
}

func TestMapSegmentBoundary(t *testing.T) {
	ms := []Mapping{{Host: "qbt", RemotePath: "/data/", LocalPath: "/mnt/"}}
	assert.Equal(t, "/database/x", Map(ms, "qbt", "/database/x"))
	assert.Equal(t, "/mnt/x", Map(ms, "qbt", "/data/x"))
	assert.Equal(t, "/mnt", Map(ms, "qbt", "/data"))
}

func TestMapOtherHostIgnored(t *testing.T) {
	ms := []Mapping{{Host: "other", RemotePath: "/downloads/", LocalPath: "/data/"}}
	assert.Equal(t, "/downloads/x", Map(ms, "qbt", "/downloads/x"))
}

func TestMapNoMatchReturnsInput(t *testing.T) {
	assert.Equal(t, "/x/y", Map(nil, "qbt", "/x/y"))
}

func TestMapWindowsRemote(t *testing.T) {
	ms := []Mapping{{Host: "win", RemotePath: `D:\Downloads\`, LocalPath: "/data/"}}
	assert.Equal(t, "/data/App/setup.zip", Map(ms, "win", `D:\Downloads\App\setup.zip`))
	assert.Equal(t, "/data/App/setup.zip", Map(ms, "win", `d:\downloads\App\setup.zip`))
	assert.Equal(t, "/data/App/setup.zip", Map(ms, "win", `D:/Downloads/App/setup.zip`))
	assert.Equal(t, `D:\Downloadsx\a`, Map(ms, "win", `D:\Downloadsx\a`))
}

func TestMapPosixCaseSensitive(t *testing.T) {
	ms := []Mapping{{Host: "qbt", RemotePath: "/Downloads/", LocalPath: "/data/"}}
	assert.Equal(t, "/downloads/x", Map(ms, "qbt", "/downloads/x"))
}

func TestLoad(t *testing.T) {
	db, q := testutil.NewDB(t)
	_, err := db.Exec(`INSERT INTO path_mappings (host, remote_path, local_path) VALUES ('qbt', '/a/', '/b/')`)
	require.NoError(t, err)
	ms, err := Load(t.Context(), q)
	require.NoError(t, err)
	require.Len(t, ms, 1)
	assert.Equal(t, Mapping{ID: ms[0].ID, Host: "qbt", RemotePath: "/a/", LocalPath: "/b/"}, ms[0])
}
