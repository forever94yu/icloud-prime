package srp

import (
	_ "crypto/sha256"
	"testing"
)

func TestChallengeRejectsInvalidPublicValues(t *testing.T) {
	params := GetParams(2048)
	for _, value := range [][]byte{nil, {0}, params.N.Bytes(), make([]byte, 300)} {
		c := NewSRPClient(params, []byte{1})
		if err := c.ProcessClientChanllenge([]byte("user"), []byte("password"), []byte{1}, value); err == nil {
			t.Fatal("invalid server public value accepted")
		}
	}
}

func TestGetParamsReturnsIndependentSettings(t *testing.T) {
	first := GetParams(2048)
	first.NoUserNameInX = true
	first.G.SetInt64(3)
	second := GetParams(2048)
	if second.NoUserNameInX || second.G.Int64() != 2 {
		t.Fatal("SRP parameter mutation leaked between clients")
	}
}
