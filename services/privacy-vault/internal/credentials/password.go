package credentials

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

const (
	argon2Version       = 19
	defaultMemoryKiB    = 64 * 1024
	defaultIterations   = 3
	defaultParallelism  = 1
	defaultSaltBytes    = 16
	defaultHashBytes    = 32
	maxEncodedHashBytes = 512
	maxPasswordBytes    = 1024
	maxMemoryKiB        = 256 * 1024
	maxIterations       = 10
	maxParallelism      = 16
	legacyBcryptBytes   = 72
)

var (
	errInvalidPasswordHash = errors.New("invalid password hash")
	legacyBcryptPattern    = regexp.MustCompile(`^\$2a\$10\$[./A-Za-z0-9]{53}$`)
)

type passwordParameters struct {
	memoryKiB   uint32
	iterations  uint32
	parallelism uint8
	saltBytes   uint32
	hashBytes   uint32
}

var defaultPasswordParameters = passwordParameters{
	memoryKiB:   defaultMemoryKiB,
	iterations:  defaultIterations,
	parallelism: defaultParallelism,
	saltBytes:   defaultSaltBytes,
	hashBytes:   defaultHashBytes,
}

// HashPassword produces a PHC-formatted Argon2id hash using random salt. The
// selected 64 MiB / t=3 / p=1 profile exceeds the OWASP minimum while keeping
// interactive verification practical on the Core login path.
func HashPassword(password string) (string, error) {
	if password == "" || len(password) > maxPasswordBytes {
		return "", errors.New("password must contain between 1 and 1024 bytes")
	}

	params := defaultPasswordParameters
	salt := make([]byte, params.saltBytes)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	digest := argon2.IDKey([]byte(password), salt, params.iterations, params.memoryKiB, params.parallelism, params.hashBytes)

	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2Version,
		params.memoryKiB,
		params.iterations,
		params.parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(digest),
	), nil
}

// VerifyPassword bounds every parsed cost before invoking a password KDF. The
// only legacy branch is the exact $2a$/cost-10 profile audited in the PtYes
// snapshot. A successful legacy comparison always requests Argon2id rehash.
// This matters because a database row must not select arbitrary bcrypt work
// factors or turn login into an unbounded memory/CPU allocation.
func VerifyPassword(encodedHash, password string) (match bool, needsRehash bool, err error) {
	if len(password) > maxPasswordBytes {
		return false, false, nil
	}
	if strings.HasPrefix(encodedHash, "$2") {
		return verifyLegacyBcryptPassword(encodedHash, password)
	}
	return verifyArgon2Password(encodedHash, password)
}

func verifyArgon2Password(encodedHash, password string) (match bool, needsRehash bool, err error) {
	params, salt, expected, err := parsePasswordHash(encodedHash)
	if err != nil {
		return false, false, err
	}

	actual := argon2.IDKey([]byte(password), salt, params.iterations, params.memoryKiB, params.parallelism, uint32(len(expected)))
	match = subtle.ConstantTimeCompare(actual, expected) == 1
	needsRehash = match && params != defaultPasswordParameters
	return match, needsRehash, nil
}

func verifyLegacyBcryptPassword(encodedHash, password string) (match bool, needsRehash bool, err error) {
	if err := ValidateLegacyPtYesPasswordHash(encodedHash); err != nil {
		return false, false, errInvalidPasswordHash
	}
	// PtYes generated hashes with Go bcrypt, which rejects new passwords over
	// 72 bytes. Treat such input as a mismatch instead of relying on truncation.
	if len(password) > legacyBcryptBytes {
		return false, false, nil
	}
	err = bcrypt.CompareHashAndPassword([]byte(encodedHash), []byte(password))
	if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		return false, false, nil
	}
	if err != nil {
		return false, false, errInvalidPasswordHash
	}
	return true, true, nil
}

// ValidateLegacyPtYesPasswordHash is exposed only inside the Privacy Vault
// module for the finite import command. Runtime registration and recovery never
// call it and always produce Argon2id.
func ValidateLegacyPtYesPasswordHash(encodedHash string) error {
	if len(encodedHash) > maxEncodedHashBytes || !legacyBcryptPattern.MatchString(encodedHash) {
		return errInvalidPasswordHash
	}
	cost, err := bcrypt.Cost([]byte(encodedHash))
	if err != nil || cost != 10 {
		return errInvalidPasswordHash
	}
	return nil
}

func parsePasswordHash(encoded string) (passwordParameters, []byte, []byte, error) {
	if len(encoded) == 0 || len(encoded) > maxEncodedHashBytes {
		return passwordParameters{}, nil, nil, errInvalidPasswordHash
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v=19" {
		return passwordParameters{}, nil, nil, errInvalidPasswordHash
	}

	params, err := parsePasswordParameters(parts[3])
	if err != nil {
		return passwordParameters{}, nil, nil, err
	}
	salt, err := base64.RawStdEncoding.Strict().DecodeString(parts[4])
	if err != nil || len(salt) < 8 || len(salt) > 64 {
		return passwordParameters{}, nil, nil, errInvalidPasswordHash
	}
	expected, err := base64.RawStdEncoding.Strict().DecodeString(parts[5])
	if err != nil || len(expected) < 16 || len(expected) > 64 {
		return passwordParameters{}, nil, nil, errInvalidPasswordHash
	}
	params.saltBytes = uint32(len(salt))
	params.hashBytes = uint32(len(expected))
	return params, salt, expected, nil
}

func parsePasswordParameters(encoded string) (passwordParameters, error) {
	parts := strings.Split(encoded, ",")
	if len(parts) != 3 {
		return passwordParameters{}, errInvalidPasswordHash
	}

	memory, err := parseUintParameter(parts[0], "m=", 32)
	if err != nil || memory < 8 || memory > maxMemoryKiB {
		return passwordParameters{}, errInvalidPasswordHash
	}
	iterations, err := parseUintParameter(parts[1], "t=", 32)
	if err != nil || iterations < 1 || iterations > maxIterations {
		return passwordParameters{}, errInvalidPasswordHash
	}
	parallelism, err := parseUintParameter(parts[2], "p=", 8)
	if err != nil || parallelism < 1 || parallelism > maxParallelism || memory < 8*parallelism {
		return passwordParameters{}, errInvalidPasswordHash
	}

	return passwordParameters{
		memoryKiB:   uint32(memory),
		iterations:  uint32(iterations),
		parallelism: uint8(parallelism),
	}, nil
}

func parseUintParameter(encoded, prefix string, bitSize int) (uint64, error) {
	if !strings.HasPrefix(encoded, prefix) {
		return 0, errInvalidPasswordHash
	}
	value, err := strconv.ParseUint(strings.TrimPrefix(encoded, prefix), 10, bitSize)
	if err != nil {
		return 0, errInvalidPasswordHash
	}
	return value, nil
}
