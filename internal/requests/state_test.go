package requests

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTransitionTable(t *testing.T) {
	allowed := map[Status]map[Action]Status{
		Pending:     {ActApprove: Approved, ActDeny: Denied, ActCancel: Cancelled},
		Approved:    {ActGrabbed: Downloading, ActGrabFailed: Failed, ActAbandon: Abandoned},
		Downloading: {ActCompleted: Importing, ActClientError: Failed, ActAbandon: Abandoned},
		Importing:   {ActImported: Available, ActImportFailed: Failed},
		Failed:      {ActRetry: Approved, ActReplace: Approved, ActAbandon: Abandoned},
	}
	statuses := []Status{Pending, Approved, Downloading, Importing, Available, Denied, Cancelled, Failed, Abandoned}
	actions := []Action{ActApprove, ActDeny, ActCancel, ActGrabbed, ActGrabFailed, ActCompleted, ActClientError, ActImported, ActImportFailed, ActAbandon, ActRetry, ActReplace}
	for _, from := range statuses {
		for _, a := range actions {
			got, err := Next(from, a)
			if want, ok := allowed[from][a]; ok {
				require.NoError(t, err, "%s %s", from, a)
				assert.Equal(t, want, got, "%s %s", from, a)
			} else {
				assert.ErrorIs(t, err, ErrInvalidTransition, "%s %s", from, a)
			}
		}
	}
}

func TestIsOpen(t *testing.T) {
	for _, s := range []Status{Pending, Approved, Downloading, Importing, Scanning} {
		assert.True(t, IsOpen(s), s)
	}
	for _, s := range []Status{Available, Denied, Cancelled, Failed, Abandoned} {
		assert.False(t, IsOpen(s), s)
	}
}
