// Package requests implements the request lifecycle: a state machine, the
// service that drives it, and the notifications each change produces.
package requests

import (
	"errors"
	"time"

	"github.com/digitalbile/softwarr/internal/prowlarr"
)

type Status string

const (
	Pending     Status = "pending"
	Approved    Status = "approved"
	Downloading Status = "downloading"
	Importing   Status = "importing"
	Scanning    Status = "scanning" // reserved; no phase 1 transition uses it
	Available   Status = "available"
	Denied      Status = "denied"
	Cancelled   Status = "cancelled"
	Failed      Status = "failed"
	Abandoned   Status = "abandoned"
)

type Action string

const (
	ActApprove      Action = "approve"
	ActDeny         Action = "deny"
	ActCancel       Action = "cancel"
	ActGrabbed      Action = "grabbed"
	ActGrabFailed   Action = "grab_failed"
	ActCompleted    Action = "completed"
	ActClientError  Action = "client_error"
	ActImported     Action = "imported"
	ActImportFailed Action = "import_failed"
	ActAbandon      Action = "abandon"
	ActRetry        Action = "retry"
	ActReplace      Action = "replace" // approve a failed request with another release
)

var ErrInvalidTransition = errors.New("That change isn't allowed for this request.")

var transitions = map[Status]map[Action]Status{
	Pending:     {ActApprove: Approved, ActDeny: Denied, ActCancel: Cancelled},
	Approved:    {ActGrabbed: Downloading, ActGrabFailed: Failed, ActAbandon: Abandoned},
	Downloading: {ActCompleted: Importing, ActClientError: Failed, ActAbandon: Abandoned},
	Importing:   {ActImported: Available, ActImportFailed: Failed},
	Failed:      {ActRetry: Approved, ActReplace: Approved, ActAbandon: Abandoned},
}

// Next returns the status reached by applying a to from.
func Next(from Status, a Action) (Status, error) {
	if to, ok := transitions[from][a]; ok {
		return to, nil
	}
	return "", ErrInvalidTransition
}

type Request struct {
	ID              int64             `json:"id"`
	RequesterID     int64             `json:"requesterId"`
	Requester       string            `json:"requester"`
	Status          Status            `json:"status"`
	Release         prowlarr.Release  `json:"release"`
	OriginalRelease *prowlarr.Release `json:"originalRelease"`
	Comment         string            `json:"comment,omitempty"`
	DisplayTitle    string            `json:"displayTitle"`
	DenyReason      string            `json:"denyReason"`
	Error           string            `json:"error"`
	// ErrorDetail is the full error behind Error, for admins only.
	ErrorDetail   string    `json:"errorDetail,omitempty"`
	Progress      float64   `json:"progress"`
	LibraryItemID *int64    `json:"libraryItemId"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// ApproveOpts adjusts an approval. Replacement must be a release the server
// found, such as one opened from a search result token.
type ApproveOpts struct {
	DisplayTitle string
	Replacement  *prowlarr.Release
}
