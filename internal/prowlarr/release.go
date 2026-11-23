package prowlarr

import (
	"strings"
	"time"

	"github.com/digitalbile/softwarr/internal/torrentfile"
)

// Release is one search result from Prowlarr.
type Release struct {
	GUID        string    `json:"guid"`
	Title       string    `json:"title"`
	Indexer     string    `json:"indexer"`
	IndexerID   int       `json:"indexerId"`
	Protocol    string    `json:"protocol"` // "torrent" | "usenet"
	Size        int64     `json:"size"`
	PublishDate time.Time `json:"publishDate"`
	Seeders     *int      `json:"seeders"`
	Leechers    *int      `json:"leechers"`
	Grabs       *int      `json:"grabs"`
	DownloadURL string    `json:"downloadUrl"`
	MagnetURL   string    `json:"magnetUrl"`
	InfoHash    string    `json:"infoHash"`
	Categories  []int     `json:"categories"`
}

// Key identifies a release across searches: the infohash when known, else the
// indexer GUID, else the title.
func (r Release) Key() string {
	switch {
	case r.InfoHash != "":
		return "ih:" + strings.ToLower(r.InfoHash)
	case r.GUID != "":
		return "guid:" + r.GUID
	default:
		return "title:" + r.Title
	}
}

// Indexer is a configured Prowlarr indexer.
type Indexer struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	Enabled  bool   `json:"enabled"`
}

// wireRelease mirrors Prowlarr's JSON, where fields may be null or absent.
type wireRelease struct {
	GUID        string         `json:"guid"`
	Title       string         `json:"title"`
	Indexer     string         `json:"indexer"`
	IndexerID   int            `json:"indexerId"`
	Protocol    string         `json:"protocol"`
	Size        int64          `json:"size"`
	PublishDate time.Time      `json:"publishDate"`
	Seeders     *int           `json:"seeders"`
	Leechers    *int           `json:"leechers"`
	Grabs       *int           `json:"grabs"`
	DownloadURL string         `json:"downloadUrl"`
	MagnetURL   string         `json:"magnetUrl"`
	InfoHash    string         `json:"infoHash"`
	Categories  []wireCategory `json:"categories"`
}

type wireCategory struct {
	ID            int            `json:"id"`
	SubCategories []wireCategory `json:"subCategories"`
}

func (w wireRelease) release() Release {
	r := Release{
		GUID: w.GUID, Title: w.Title, Indexer: w.Indexer, IndexerID: w.IndexerID,
		Protocol: w.Protocol, Size: w.Size, PublishDate: w.PublishDate,
		Seeders: w.Seeders, Leechers: w.Leechers, Grabs: w.Grabs,
		DownloadURL: w.DownloadURL, MagnetURL: w.MagnetURL, InfoHash: w.InfoHash,
		Categories: flattenCategories(nil, w.Categories),
	}
	if r.InfoHash == "" && r.MagnetURL != "" {
		r.InfoHash, _ = torrentfile.ParseMagnet(r.MagnetURL)
	}
	return r
}

func flattenCategories(out []int, cats []wireCategory) []int {
	if out == nil {
		out = []int{}
	}
	for _, c := range cats {
		out = append(out, c.ID)
		out = flattenCategories(out, c.SubCategories)
	}
	return out
}
