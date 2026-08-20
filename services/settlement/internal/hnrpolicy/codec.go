package hnrpolicy

import (
	"crypto/sha256"
	"errors"

	"github.com/peergo/peergo/contracts/go/hnrpolicyv1"
)

const MaxPolicyBytes = hnrpolicyv1.MaxPolicyBytes

var ErrInvalidEncoding = errors.New("H&R policy encoding is invalid")

func Encode(policy Policy) ([]byte, error) {
	encoded, err := hnrpolicyv1.Encode(policy)
	if err != nil {
		return nil, ErrInvalidEncoding
	}
	return encoded, nil
}

func Decode(encoded []byte) (Policy, error) {
	policy, err := hnrpolicyv1.Decode(encoded)
	if err != nil {
		return Policy{}, ErrInvalidEncoding
	}
	return policy, nil
}

func SHA256(policy Policy) ([sha256.Size]byte, error) {
	digest, err := hnrpolicyv1.SHA256(policy)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	return digest, nil
}
