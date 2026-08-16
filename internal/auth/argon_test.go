package auth

import "testing"

func TestHashVerify(t *testing.T) {
	h, err := Hash("secret-token")
	if err != nil {
		t.Fatal(err)
	}
	if h == "secret-token" {
		t.Fatal("hash stored plaintext")
	}
	if !Verify(h, "secret-token") {
		t.Fatal("verify failed")
	}
	if Verify(h, "wrong") {
		t.Fatal("wrong password accepted")
	}
}
