package signedsnapshotv1

import (
	"crypto/ed25519"
	"encoding/base64"
	"testing"
)

func TestParseTrustedKeysAcceptsRotationSet(t *testing.T) {
	first := make([]byte, ed25519.PublicKeySize)
	second := make([]byte, ed25519.PublicKeySize)
	for index := range first {
		first[index] = 0x11
		second[index] = 0x22
	}
	keys, err := ParseTrustedKeys(
		"old=" + base64.StdEncoding.EncodeToString(first) +
			",active=" + base64.StdEncoding.EncodeToString(second),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 || len(keys["old"]) != ed25519.PublicKeySize || keys["active"][0] != 0x22 {
		t.Fatalf("keys = %#v", keys)
	}
}

func TestParseTrustedKeysRejectsDuplicateAndMalformedEntries(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString(make([]byte, ed25519.PublicKeySize))
	for _, value := range []string{
		"active=" + encoded + ",active=" + encoded,
		"Active=" + encoded,
		"active=short",
		"",
	} {
		if _, err := ParseTrustedKeys(value); err == nil {
			t.Fatalf("ParseTrustedKeys(%q) accepted invalid input", value)
		}
	}
}
