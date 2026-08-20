package torrents

// MetainfoKind distinguishes formats that share a bencoded container but need
// different swarm identities and file-tree rules. Detection is diagnostic: it
// never makes an unsupported format admissible to the v1 upload path.
type MetainfoKind string

const (
	MetainfoKindV1          MetainfoKind = "v1"
	MetainfoKindHybridV1V2  MetainfoKind = "hybrid_v1_v2"
	MetainfoKindV2          MetainfoKind = "v2"
	MetainfoKindBEP30Merkle MetainfoKind = "bep30_merkle"
)

// DetectMetainfoKind performs one bounded structural decode and looks only at
// version-discriminating keys. Callers must still run the complete parser for
// the returned format before trusting hashes, paths, sizes, or privacy flags.
func DetectMetainfoKind(raw []byte, profile ValidationProfile) (MetainfoKind, error) {
	if !profile.valid() {
		return "", validationFailure(CodeInvalidProfile, "profile", 0, "unknown validation profile")
	}
	root, _, err := decodeBencode(raw, profile)
	if err != nil {
		return "", err
	}
	if root.kind != bencodeDictionary {
		return "", validationFailure(CodeInvalidInfo, "metainfo", root.start, "root value is not a dictionary")
	}
	info, exists := root.get("info")
	if !exists {
		return "", validationFailure(CodeMissingInfo, "info", root.start, "root dictionary has no info value")
	}
	if info.kind != bencodeDictionary {
		return "", validationFailure(CodeInvalidInfo, "info", info.start, "info value is not a dictionary")
	}
	if _, exists := info.get("root hash"); exists {
		return MetainfoKindBEP30Merkle, nil
	}
	_, hasMetaVersion := info.get("meta version")
	_, hasFileTree := info.get("file tree")
	_, hasPieceLayers := root.get("piece layers")
	if !hasMetaVersion && !hasFileTree && !hasPieceLayers {
		return MetainfoKindV1, nil
	}
	_, hasPieces := info.get("pieces")
	_, hasLength := info.get("length")
	_, hasFiles := info.get("files")
	if hasPieces && (hasLength != hasFiles) {
		return MetainfoKindHybridV1V2, nil
	}
	return MetainfoKindV2, nil
}
