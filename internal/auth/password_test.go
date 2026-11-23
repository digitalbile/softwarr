package auth

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHashAndVerify(t *testing.T) {
	hash, err := HashPassword("pw")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(hash, "$argon2id$v=19$m=19456,t=2,p=1$"))
	assert.True(t, VerifyPassword(hash, "pw"))
	assert.False(t, VerifyPassword(hash, "nope"))
	assert.False(t, VerifyPassword("", "pw"))
	assert.False(t, VerifyPassword("garbage", "pw"))
}
