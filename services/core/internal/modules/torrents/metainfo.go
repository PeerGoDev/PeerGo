package torrents

import (
	"crypto/sha1" // #nosec G505 -- BEP 3 mandates SHA-1 for the v1 wire identity.
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// MaxMetainfoBytes bounds both parser memory and the attacker-controlled
	// pieces string. With the multi-megabyte piece sizes used for large releases
	// it can still describe TiB-scale v1 content while keeping validation
	// predictable; the upload service will apply a lower configurable policy if
	// the site does not need this compatibility ceiling.
	MaxMetainfoBytes = 16 << 20
	MaxTorrentFiles  = 100_000

	maxPathComponents = 64
	maxPathComponent  = 255
	maxPathBytes      = 4096
)

// ValidationProfile keeps new upload policy separate from conservative legacy
// import tolerance. Both profiles preserve and hash the exact info slice,
// reject duplicate keys, and require private=1.
type ValidationProfile string

const (
	ValidationProfileStrictUpload ValidationProfile = "strict_upload"
	ValidationProfileLegacyImport ValidationProfile = "legacy_import"
)

func (profile ValidationProfile) valid() bool {
	return profile == ValidationProfileStrictUpload || profile == ValidationProfileLegacyImport
}

// CompatibilityFlag records a tolerated import anomaly. It is persisted with
// the object so migration reports can explain why a legacy item cannot be
// treated as a clean new upload.
type CompatibilityFlag string

const (
	CompatibilityUnsortedDictionary CompatibilityFlag = "unsorted_dictionary"
	CompatibilityLegacyUTF8Alias    CompatibilityFlag = "legacy_utf8_alias"
	CompatibilityNonPortablePath    CompatibilityFlag = "non_portable_path"
	CompatibilityCaseCollidingPath  CompatibilityFlag = "case_colliding_path"
	CompatibilityDuplicatePath      CompatibilityFlag = "duplicate_path"
	CompatibilityIrregularPadding   CompatibilityFlag = "irregular_padding"
	CompatibilityHybridV1V2         CompatibilityFlag = "hybrid_v1_v2"
	CompatibilityOverlongPath       CompatibilityFlag = "overlong_path_component"
)

// ValidationCode is stable enough to drive upload errors and migration
// discrepancy reports without exposing parser implementation details.
type ValidationCode string

const (
	CodeObjectEmpty            ValidationCode = "object_empty"
	CodeObjectTooLarge         ValidationCode = "object_too_large"
	CodeMalformedBencode       ValidationCode = "malformed_bencode"
	CodeNonCanonicalBencode    ValidationCode = "non_canonical_bencode"
	CodeDuplicateDictionaryKey ValidationCode = "duplicate_dictionary_key"
	CodeResourceLimit          ValidationCode = "resource_limit"
	CodeMissingInfo            ValidationCode = "missing_info"
	CodeInvalidInfo            ValidationCode = "invalid_info"
	CodeUnsupportedVersion     ValidationCode = "unsupported_metainfo_version"
	CodePrivateRequired        ValidationCode = "private_required"
	CodeInvalidMetainfoField   ValidationCode = "invalid_metainfo_field"
	CodeInvalidName            ValidationCode = "invalid_name"
	CodeInvalidPieces          ValidationCode = "invalid_pieces"
	CodeInvalidFileLayout      ValidationCode = "invalid_file_layout"
	CodeInvalidPath            ValidationCode = "invalid_path"
	CodeUnsupportedFileType    ValidationCode = "unsupported_file_type"
	CodeSizeOverflow           ValidationCode = "size_overflow"
	CodeInvalidProfile         ValidationCode = "invalid_validation_profile"
	CodeInvalidScreenshot      ValidationCode = "invalid_screenshot"
)

// ValidationError avoids echoing raw bencoded values, filenames, or source
// strings into logs. Field and byte offset are enough to reconcile an object.
type ValidationError struct {
	Code   ValidationCode
	Field  string
	Offset int
	detail string
}

func (validationError *ValidationError) Error() string {
	if validationError == nil {
		return "torrent metainfo validation failed"
	}
	return fmt.Sprintf(
		"torrent metainfo validation failed: code=%s field=%s offset=%d: %s",
		validationError.Code,
		validationError.Field,
		validationError.Offset,
		validationError.detail,
	)
}

func validationFailure(code ValidationCode, field string, offset int, detail string) error {
	return &ValidationError{Code: code, Field: field, Offset: offset, detail: detail}
}

// ValidationCodeOf extracts the stable code used by API and migration layers.
func ValidationCodeOf(err error) (ValidationCode, bool) {
	var validationError *ValidationError
	if !errors.As(err, &validationError) {
		return "", false
	}
	return validationError.Code, true
}

// ValidationDiagnostic is safe for bounded operator logs: Field is a parser
// path such as info.files[3].path[1], Reason is a fixed implementation string,
// and neither contains the user-controlled metainfo value.
type ValidationDiagnostic struct {
	Code   ValidationCode
	Field  string
	Offset int
	Reason string
}

func ValidationDiagnosticOf(err error) (ValidationDiagnostic, bool) {
	var validationError *ValidationError
	if !errors.As(err, &validationError) {
		return ValidationDiagnostic{}, false
	}
	return ValidationDiagnostic{
		Code: validationError.Code, Field: validationError.Field,
		Offset: validationError.Offset, Reason: validationError.detail,
	}, true
}

// File is an immutable, normalized file-list entry. PathComponents are relative
// to Name for multi-file torrents; a single-file torrent contains Name itself.
type File struct {
	Index          int
	PathComponents []string
	DisplayPath    string
	LengthBytes    int64
	Padding        bool
}

// ParsedMetainfo is the transport-independent result used to create an object
// manifest and torrent aggregate. It intentionally does not retain uploaded
// bytes; the object store remains their sole durable owner.
type ParsedMetainfo struct {
	ParserVersion      string
	ValidationProfile  ValidationProfile
	CompatibilityFlags []CompatibilityFlag
	ObjectSHA256       ObjectSHA256
	ObjectByteLength   int64
	InfoHashV1         InfoHashV1
	InfoOffset         int64
	InfoLength         int64
	Name               string
	Private            bool
	MultiFile          bool
	PieceLengthBytes   int64
	PieceCount         int
	TotalSizeBytes     int64
	PayloadSizeBytes   int64
	PaddingFileCount   int
	Files              []File
}

// ParseV1 validates conventional BEP 3 metainfo and hashes the original info
// substring. It never decode-encodes the info dictionary, because even a
// semantically equivalent rewrite can change the swarm identity.
func ParseV1(raw []byte, profile ValidationProfile) (ParsedMetainfo, error) {
	return parseV1(raw, profile, true, false)
}

// InspectLegacyV1 performs the same bounded structural parse but reports a
// missing private marker through ParsedMetainfo.Private instead of stopping.
// It exists only so migration can still hash and reconcile quarantined PtYes
// objects; NewPendingTorrent independently rejects any result with Private=false.
func InspectLegacyV1(raw []byte) (ParsedMetainfo, error) {
	return parseV1(raw, ValidationProfileLegacyImport, false, false)
}

// InspectLegacyV1OrHybrid is restricted to finite legacy import. For a BEP 52
// hybrid it validates the complete v1 pieces/files representation, preserves
// and hashes the exact hybrid info dictionary, and records that the v2 file
// tree was not materialized. Pure v2 and Merkle torrents remain unsupported.
func InspectLegacyV1OrHybrid(raw []byte) (ParsedMetainfo, error) {
	return parseV1(raw, ValidationProfileLegacyImport, false, true)
}

func parseV1(
	raw []byte,
	profile ValidationProfile,
	requirePrivate bool,
	allowLegacyHybrid bool,
) (ParsedMetainfo, error) {
	if !profile.valid() {
		return ParsedMetainfo{}, validationFailure(CodeInvalidProfile, "profile", 0, "unknown validation profile")
	}
	if len(raw) == 0 {
		return ParsedMetainfo{}, validationFailure(CodeObjectEmpty, "metainfo", 0, "object has no bytes")
	}
	if len(raw) > MaxMetainfoBytes {
		return ParsedMetainfo{}, validationFailure(CodeObjectTooLarge, "metainfo", 0, "object exceeds the parser byte budget")
	}

	root, unsorted, err := decodeBencode(raw, profile)
	if err != nil {
		return ParsedMetainfo{}, err
	}
	if root.kind != bencodeDictionary {
		return ParsedMetainfo{}, validationFailure(CodeInvalidInfo, "metainfo", root.start, "root value is not a dictionary")
	}
	info, exists := root.get("info")
	if !exists {
		return ParsedMetainfo{}, validationFailure(CodeMissingInfo, "info", root.start, "root dictionary has no info value")
	}
	if info.kind != bencodeDictionary {
		return ParsedMetainfo{}, validationFailure(CodeInvalidInfo, "info", info.start, "info value is not a dictionary")
	}

	if _, exists := info.get("root hash"); exists {
		return ParsedMetainfo{}, validationFailure(CodeUnsupportedVersion, "info.root hash", info.start, "BEP 30 Merkle torrents are not enabled")
	}
	hybrid, err := validateLegacyHybridEnvelope(root, info, allowLegacyHybrid)
	if err != nil {
		return ParsedMetainfo{}, err
	}

	flags := newCompatibilityFlags()
	if unsorted {
		flags.add(CompatibilityUnsortedDictionary)
	}
	parserVersion := ParserVersion
	if hybrid {
		flags.add(CompatibilityHybridV1V2)
		parserVersion = ParserVersionLegacyHybridV1V2
	}
	private := hasPrivateMarker(info)
	if requirePrivate && !private {
		privateValue, _ := info.get("private")
		offset := info.start
		if privateValue != nil {
			offset = privateValue.start
		}
		return ParsedMetainfo{}, validationFailure(CodePrivateRequired, "info.private", offset, "private must be the integer 1")
	}
	name, err := selectText(info, "name", "name.utf-8", profile, flags)
	if err != nil {
		return ParsedMetainfo{}, err
	}
	if err := validatePathComponent(name, "info.name", info.start, profile, flags); err != nil {
		return ParsedMetainfo{}, err
	}

	pieceLengthValue, exists := info.get("piece length")
	if !exists {
		return ParsedMetainfo{}, validationFailure(CodeInvalidMetainfoField, "info.piece length", info.start, "piece length is missing")
	}
	pieceLength, err := positiveInt64(pieceLengthValue, "info.piece length")
	if err != nil {
		return ParsedMetainfo{}, err
	}
	piecesValue, exists := info.get("pieces")
	if !exists {
		return ParsedMetainfo{}, validationFailure(CodeInvalidPieces, "info.pieces", info.start, "pieces is missing")
	}
	if piecesValue.kind != bencodeBytes || len(piecesValue.bytes) == 0 || len(piecesValue.bytes)%sha1.Size != 0 {
		return ParsedMetainfo{}, validationFailure(CodeInvalidPieces, "info.pieces", piecesValue.start, "pieces must contain complete SHA-1 digests")
	}

	files, multiFile, totalSize, payloadSize, paddingFiles, err := parseFiles(info, name, pieceLength, profile, flags)
	if err != nil {
		return ParsedMetainfo{}, err
	}
	expectedPieces := totalSize / pieceLength
	if totalSize%pieceLength != 0 {
		expectedPieces++
	}
	actualPieces := int64(len(piecesValue.bytes) / sha1.Size)
	if expectedPieces != actualPieces {
		return ParsedMetainfo{}, validationFailure(CodeInvalidPieces, "info.pieces", piecesValue.start, "piece digest count does not match content size")
	}

	// The two hashes intentionally cover different byte ranges: the object
	// digest verifies storage, while the BEP 3 identity covers the exact info
	// substring and must survive outer announce rewriting.
	objectDigest := sha256.Sum256(raw)
	infoDigest := sha1.Sum(raw[info.start:info.end]) // #nosec G401 -- required by BEP 3.

	return ParsedMetainfo{
		ParserVersion:      parserVersion,
		ValidationProfile:  profile,
		CompatibilityFlags: flags.sorted(),
		ObjectSHA256:       ObjectSHA256(objectDigest),
		ObjectByteLength:   int64(len(raw)),
		InfoHashV1:         InfoHashV1(infoDigest),
		InfoOffset:         int64(info.start),
		InfoLength:         int64(info.end - info.start),
		Name:               name,
		Private:            private,
		MultiFile:          multiFile,
		PieceLengthBytes:   pieceLength,
		PieceCount:         int(actualPieces),
		TotalSizeBytes:     totalSize,
		PayloadSizeBytes:   payloadSize,
		PaddingFileCount:   paddingFiles,
		Files:              files,
	}, nil
}

func validateLegacyHybridEnvelope(
	root *bencodeValue,
	info *bencodeValue,
	allow bool,
) (bool, error) {
	metaVersion, hasMetaVersion := info.get("meta version")
	fileTree, hasFileTree := info.get("file tree")
	pieceLayers, hasPieceLayers := root.get("piece layers")
	if !hasMetaVersion && !hasFileTree && !hasPieceLayers {
		return false, nil
	}
	if !allow {
		return false, validationFailure(
			CodeUnsupportedVersion,
			"info.meta version",
			info.start,
			"v2 and hybrid torrents are not enabled",
		)
	}
	// The migration compatibility path is deliberately narrower than generic
	// v2 detection: only a structurally identifiable BEP 52 hybrid with its v1
	// representation present may continue into the ordinary v1 parser.
	if !hasMetaVersion || metaVersion.kind != bencodeInteger || string(metaVersion.integer) != "2" ||
		!hasFileTree || fileTree.kind != bencodeDictionary {
		return false, validationFailure(
			CodeUnsupportedVersion,
			"info.meta version",
			info.start,
			"object is not a supported BEP 52 hybrid",
		)
	}
	if hasPieceLayers && pieceLayers.kind != bencodeDictionary {
		return false, validationFailure(
			CodeInvalidMetainfoField,
			"piece layers",
			pieceLayers.start,
			"piece layers must be a dictionary",
		)
	}
	pieces, hasPieces := info.get("pieces")
	_, hasLength := info.get("length")
	_, hasFiles := info.get("files")
	if !hasPieces || pieces.kind != bencodeBytes || hasLength == hasFiles {
		return false, validationFailure(
			CodeUnsupportedVersion,
			"info",
			info.start,
			"hybrid object has no complete v1 representation",
		)
	}
	return true, nil
}

func hasPrivateMarker(info *bencodeValue) bool {
	privateValue, exists := info.get("private")
	return exists && privateValue.kind == bencodeInteger && string(privateValue.integer) == "1"
}

func parseFiles(
	info *bencodeValue,
	name string,
	pieceLength int64,
	profile ValidationProfile,
	flags *compatibilityFlags,
) ([]File, bool, int64, int64, int, error) {
	lengthValue, hasLength := info.get("length")
	filesValue, hasFiles := info.get("files")
	if hasLength == hasFiles {
		return nil, false, 0, 0, 0, validationFailure(
			CodeInvalidFileLayout,
			"info",
			info.start,
			"exactly one of length or files is required",
		)
	}

	if hasLength {
		length, err := nonnegativeInt64(lengthValue, "info.length")
		if err != nil {
			return nil, false, 0, 0, 0, err
		}
		if length == 0 {
			return nil, false, 0, 0, 0, validationFailure(CodeInvalidFileLayout, "info.length", lengthValue.start, "single-file content is empty")
		}
		attributes, err := fileAttributes(info, "info.attr")
		if err != nil {
			return nil, false, 0, 0, 0, err
		}
		if strings.ContainsRune(attributes, 'l') || strings.ContainsRune(attributes, 'p') {
			return nil, false, 0, 0, 0, validationFailure(CodeUnsupportedFileType, "info.attr", info.start, "single-file symlink and padding attributes are not supported")
		}
		return []File{{Index: 0, PathComponents: []string{name}, DisplayPath: name, LengthBytes: length}}, false, length, length, 0, nil
	}

	if filesValue.kind != bencodeList || len(filesValue.list) == 0 {
		return nil, true, 0, 0, 0, validationFailure(CodeInvalidFileLayout, "info.files", filesValue.start, "files must be a non-empty list")
	}
	if len(filesValue.list) > MaxTorrentFiles {
		return nil, true, 0, 0, 0, validationFailure(CodeResourceLimit, "info.files", filesValue.start, "file count exceeds the parser budget")
	}

	files := make([]File, 0, len(filesValue.list))
	exactPaths := make(map[string]struct{}, len(filesValue.list))
	portablePaths := make(map[string]string, len(filesValue.list))
	var totalSize int64
	var payloadSize int64
	paddingFiles := 0

	for index, entry := range filesValue.list {
		field := fmt.Sprintf("info.files[%d]", index)
		if entry.kind != bencodeDictionary {
			return nil, true, 0, 0, 0, unexpectedBencodeType(field, entry, "dictionary")
		}
		lengthValue, exists := entry.get("length")
		if !exists {
			return nil, true, 0, 0, 0, validationFailure(CodeInvalidFileLayout, field+".length", entry.start, "file length is missing")
		}
		length, err := nonnegativeInt64(lengthValue, field+".length")
		if err != nil {
			return nil, true, 0, 0, 0, err
		}
		attributes, err := fileAttributes(entry, field+".attr")
		if err != nil {
			return nil, true, 0, 0, 0, err
		}
		if strings.ContainsRune(attributes, 'l') {
			return nil, true, 0, 0, 0, validationFailure(CodeUnsupportedFileType, field+".attr", entry.start, "symlink entries are not enabled")
		}
		padding := strings.ContainsRune(attributes, 'p')

		components, err := selectPath(entry, field, profile, flags)
		if err != nil {
			return nil, true, 0, 0, 0, err
		}
		displayPath := strings.Join(components, "/")
		if len(name)+1+len(displayPath) > maxPathBytes {
			return nil, true, 0, 0, 0, validationFailure(CodeInvalidPath, field+".path", entry.start, "path exceeds the byte budget")
		}
		if _, duplicate := exactPaths[displayPath]; duplicate {
			if profile == ValidationProfileStrictUpload {
				return nil, true, 0, 0, 0, validationFailure(CodeInvalidPath, field+".path", entry.start, "path is duplicated")
			}
			flags.add(CompatibilityDuplicatePath)
		}
		exactPaths[displayPath] = struct{}{}
		portableKey := strings.ToLower(displayPath)
		if previous, collision := portablePaths[portableKey]; collision && previous != displayPath {
			if profile == ValidationProfileStrictUpload {
				return nil, true, 0, 0, 0, validationFailure(CodeInvalidPath, field+".path", entry.start, "paths collide on case-insensitive filesystems")
			}
			flags.add(CompatibilityCaseCollidingPath)
		} else {
			portablePaths[portableKey] = displayPath
		}

		if padding {
			if length == 0 {
				return nil, true, 0, 0, 0, validationFailure(CodeInvalidFileLayout, field+".length", lengthValue.start, "padding file is empty")
			}
			remainder := totalSize % pieceLength
			expected := pieceLength - remainder
			if remainder == 0 || length != expected {
				if profile == ValidationProfileStrictUpload {
					return nil, true, 0, 0, 0, validationFailure(CodeInvalidFileLayout, field, entry.start, "padding does not align the next file to a piece boundary")
				}
				flags.add(CompatibilityIrregularPadding)
			}
			paddingFiles++
		} else {
			if payloadSize > math.MaxInt64-length {
				return nil, true, 0, 0, 0, validationFailure(CodeSizeOverflow, field+".length", lengthValue.start, "payload size exceeds bigint")
			}
			payloadSize += length
		}
		if totalSize > math.MaxInt64-length {
			return nil, true, 0, 0, 0, validationFailure(CodeSizeOverflow, field+".length", lengthValue.start, "total size exceeds bigint")
		}
		totalSize += length
		files = append(files, File{
			Index: index, PathComponents: append([]string(nil), components...),
			DisplayPath: displayPath, LengthBytes: length, Padding: padding,
		})
	}
	if totalSize == 0 || payloadSize == 0 {
		return nil, true, 0, 0, 0, validationFailure(CodeInvalidFileLayout, "info.files", filesValue.start, "torrent has no payload bytes")
	}
	return files, true, totalSize, payloadSize, paddingFiles, nil
}

func selectPath(
	entry *bencodeValue,
	field string,
	profile ValidationProfile,
	flags *compatibilityFlags,
) ([]string, error) {
	rawPath, exists := entry.get("path")
	if !exists || rawPath.kind != bencodeList || len(rawPath.list) == 0 || len(rawPath.list) > maxPathComponents {
		offset := entry.start
		if rawPath != nil {
			offset = rawPath.start
		}
		return nil, validationFailure(CodeInvalidPath, field+".path", offset, "path must be a bounded non-empty list")
	}
	rawComponents, rawUTF8, err := decodePathComponents(rawPath, field+".path")
	if err != nil {
		return nil, err
	}

	selected := rawComponents
	if utf8Path, hasAlias := entry.get("path.utf-8"); hasAlias {
		if utf8Path.kind != bencodeList || len(utf8Path.list) != len(rawPath.list) {
			return nil, validationFailure(CodeInvalidPath, field+".path.utf-8", utf8Path.start, "UTF-8 path alias shape differs from path")
		}
		aliasComponents, aliasUTF8, aliasErr := decodePathComponents(utf8Path, field+".path.utf-8")
		if aliasErr != nil || !aliasUTF8 {
			return nil, validationFailure(CodeInvalidPath, field+".path.utf-8", utf8Path.start, "UTF-8 path alias is invalid")
		}
		selected = aliasComponents
		if !rawUTF8 {
			if profile == ValidationProfileStrictUpload {
				return nil, validationFailure(CodeInvalidPath, field+".path", rawPath.start, "path is not UTF-8")
			}
			flags.add(CompatibilityLegacyUTF8Alias)
		}
	} else if !rawUTF8 {
		return nil, validationFailure(CodeInvalidPath, field+".path", rawPath.start, "path is not UTF-8")
	}

	for index, component := range selected {
		if err := validatePathComponent(component, fmt.Sprintf("%s.path[%d]", field, index), rawPath.start, profile, flags); err != nil {
			return nil, err
		}
	}
	return selected, nil
}

func decodePathComponents(path *bencodeValue, field string) ([]string, bool, error) {
	components := make([]string, 0, len(path.list))
	allUTF8 := true
	for _, component := range path.list {
		if component.kind != bencodeBytes {
			return nil, false, unexpectedBencodeType(field, component, "byte strings")
		}
		if !utf8.Valid(component.bytes) {
			allUTF8 = false
		}
		components = append(components, string(component.bytes))
	}
	return components, allUTF8, nil
}

func selectText(
	dictionary *bencodeValue,
	key string,
	utf8Key string,
	profile ValidationProfile,
	flags *compatibilityFlags,
) (string, error) {
	rawValue, exists := dictionary.get(key)
	if !exists || rawValue.kind != bencodeBytes || len(rawValue.bytes) == 0 {
		offset := dictionary.start
		if rawValue != nil {
			offset = rawValue.start
		}
		return "", validationFailure(CodeInvalidName, "info."+key, offset, "name must be a non-empty byte string")
	}
	rawValid := utf8.Valid(rawValue.bytes)
	selected := rawValue.bytes
	if alias, hasAlias := dictionary.get(utf8Key); hasAlias {
		if alias.kind != bencodeBytes || len(alias.bytes) == 0 || !utf8.Valid(alias.bytes) {
			return "", validationFailure(CodeInvalidName, "info."+utf8Key, alias.start, "UTF-8 name alias is invalid")
		}
		selected = alias.bytes
		if !rawValid {
			if profile == ValidationProfileStrictUpload {
				return "", validationFailure(CodeInvalidName, "info."+key, rawValue.start, "name is not UTF-8")
			}
			flags.add(CompatibilityLegacyUTF8Alias)
		}
	} else if !rawValid {
		return "", validationFailure(CodeInvalidName, "info."+key, rawValue.start, "name is not UTF-8")
	}
	return string(selected), nil
}

func validatePathComponent(
	component string,
	field string,
	offset int,
	profile ValidationProfile,
	flags *compatibilityFlags,
) error {
	if component == "" {
		return validationFailure(CodeInvalidPath, field, offset, "path component is empty")
	}
	if component == "." || component == ".." {
		return validationFailure(CodeInvalidPath, field, offset, "path component is a relative traversal marker")
	}
	if len(component) > maxPathComponent {
		if profile == ValidationProfileStrictUpload || len(component) > maxPathBytes {
			return validationFailure(CodeInvalidPath, field, offset, "path component exceeds the byte budget")
		}
		// Historical clients accepted long UTF-8 names even though they are
		// not portable to all filesystems. PeerGo stores and displays the exact
		// name but never treats it as a host extraction path.
		flags.add(CompatibilityOverlongPath)
	}
	if strings.ContainsAny(component, "/\\") {
		return validationFailure(CodeInvalidPath, field, offset, "path component contains a separator")
	}
	if !utf8.ValidString(component) {
		return validationFailure(CodeInvalidPath, field, offset, "path component is not UTF-8")
	}
	for _, character := range component {
		if unicode.IsControl(character) {
			return validationFailure(CodeInvalidPath, field, offset, "path component contains a control character")
		}
	}

	if strings.HasSuffix(component, ".") || strings.HasSuffix(component, " ") || windowsReservedName(component) {
		if profile == ValidationProfileStrictUpload {
			return validationFailure(CodeInvalidPath, field, offset, "path component is not portable")
		}
		flags.add(CompatibilityNonPortablePath)
	}
	return nil
}

func windowsReservedName(component string) bool {
	base := component
	if dot := strings.IndexByte(base, '.'); dot >= 0 {
		base = base[:dot]
	}
	base = strings.ToUpper(base)
	if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" {
		return true
	}
	if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) {
		return base[3] >= '1' && base[3] <= '9'
	}
	return false
}

func fileAttributes(dictionary *bencodeValue, field string) (string, error) {
	value, exists := dictionary.get("attr")
	if !exists {
		return "", nil
	}
	if value.kind != bencodeBytes {
		return "", unexpectedBencodeType(field, value, "byte string")
	}
	return string(value.bytes), nil
}

func positiveInt64(value *bencodeValue, field string) (int64, error) {
	result, err := integerInt64(value, field)
	if err != nil {
		return 0, err
	}
	if result <= 0 {
		return 0, validationFailure(CodeInvalidMetainfoField, field, value.start, "integer must be positive")
	}
	return result, nil
}

func nonnegativeInt64(value *bencodeValue, field string) (int64, error) {
	result, err := integerInt64(value, field)
	if err != nil {
		return 0, err
	}
	if result < 0 {
		return 0, validationFailure(CodeInvalidMetainfoField, field, value.start, "integer must not be negative")
	}
	return result, nil
}

func integerInt64(value *bencodeValue, field string) (int64, error) {
	if value == nil || value.kind != bencodeInteger {
		return 0, unexpectedBencodeType(field, value, "integer")
	}
	result, err := strconv.ParseInt(string(value.integer), 10, 64)
	if err != nil {
		return 0, validationFailure(CodeSizeOverflow, field, value.start, "integer does not fit signed 64-bit storage")
	}
	return result, nil
}

type compatibilityFlags map[CompatibilityFlag]struct{}

func newCompatibilityFlags() *compatibilityFlags {
	flags := compatibilityFlags{}
	return &flags
}

func (flags *compatibilityFlags) add(flag CompatibilityFlag) {
	(*flags)[flag] = struct{}{}
}

func (flags *compatibilityFlags) sorted() []CompatibilityFlag {
	result := make([]CompatibilityFlag, 0, len(*flags))
	for flag := range *flags {
		result = append(result, flag)
	}
	sort.Slice(result, func(left, right int) bool { return result[left] < result[right] })
	return result
}
