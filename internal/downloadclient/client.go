// Package downloadclient defines the interface softwarr uses to hand releases
// to a download client and to watch their progress. softwarr only adds and
// reads downloads; clients must never pause, remove, or recategorize them.
package downloadclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
)

type State string

const (
	StateQueued      State = "queued"
	StateDownloading State = "downloading"
	StateCompleted   State = "completed"
	StateError       State = "error"
	StateMissing     State = "missing"
)

// File is one file in a download. Path is relative to Item.SavePath and uses
// forward slashes.
type File struct {
	Path string
	Size int64
}

type Item struct {
	ID, Name, SavePath string
	Progress           float64
	State              State
	Message            string
	Files              []File
}

type AddRequest struct {
	TorrentFile []byte
	MagnetURI   string
	// InfoHash is the lowercase hex v1 infohash. When empty, clients derive it.
	InfoHash string
}

type Client interface {
	Protocol() string
	Test(ctx context.Context) error
	Add(ctx context.Context, req AddRequest) (id string, err error)
	// Get returns StateMissing when the client no longer has the download.
	Get(ctx context.Context, id string) (Item, error)
	// Host is the client's host name, used to look up path mappings.
	Host() string
}

type Config struct {
	ID       int64
	Name     string
	Type     string
	Enabled  bool
	Priority int // lowest value wins
	Settings json.RawMessage
}

type Factory func(Config, *http.Client) (Client, error)

// InputError is a validation failure that is safe to show to the admin.
type InputError struct{ Msg string }

func (e *InputError) Error() string { return e.Msg }

// Invalid builds an InputError.
func Invalid(msg string) error { return &InputError{Msg: msg} }

func IsInputError(err error) bool {
	var ie *InputError
	return errors.As(err, &ie)
}
