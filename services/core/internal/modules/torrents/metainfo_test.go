package torrents

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestParseV1PreservesExactInfoIdentity(t *testing.T) {
	t.Parallel()

	info := testDictionary(map[string][]byte{
		"length":       testInteger(5),
		"name":         testBytes([]byte("file.txt")),
		"piece length": testInteger(16 * 1024),
		"pieces":       testBytes(bytes.Repeat([]byte{0x61}, sha1.Size)),
		"private":      testInteger(1),
		"source":       testBytes([]byte("[PeerGo]")),
	})
	raw := testDictionary(map[string][]byte{
		"announce": testBytes([]byte("https://old.example/announce")),
		"info":     info,
	})

	parsed, err := ParseV1(raw, ValidationProfileStrictUpload)
	if err != nil {
		t.Fatalf("ParseV1() error = %v", err)
	}
	wantInfoHash := sha1.Sum(info) // #nosec G401 -- test fixture for BEP 3.
	wantObjectHash := sha256.Sum256(raw)
	if parsed.InfoHashV1 != InfoHashV1(wantInfoHash) {
		t.Fatalf("InfoHashV1 = %s, want %x", parsed.InfoHashV1.Hex(), wantInfoHash)
	}
	if parsed.ObjectSHA256 != ObjectSHA256(wantObjectHash) {
		t.Fatalf("ObjectSHA256 = %s, want %x", parsed.ObjectSHA256.Hex(), wantObjectHash)
	}
	if got := raw[parsed.InfoOffset : parsed.InfoOffset+parsed.InfoLength]; !bytes.Equal(got, info) {
		t.Fatalf("captured info bytes = %q, want %q", got, info)
	}
	if parsed.Name != "file.txt" || parsed.MultiFile || parsed.TotalSizeBytes != 5 || parsed.PayloadSizeBytes != 5 {
		t.Fatalf("parsed content = %+v", parsed)
	}
	if parsed.PieceLengthBytes != 16*1024 || parsed.PieceCount != 1 || !parsed.Private {
		t.Fatalf("parsed piece/private fields = %+v", parsed)
	}
	if len(parsed.Files) != 1 || parsed.Files[0].DisplayPath != "file.txt" || parsed.Files[0].LengthBytes != 5 {
		t.Fatalf("files = %+v", parsed.Files)
	}
}

func TestParseV1OuterAnnounceRewriteDoesNotChangeInfoHash(t *testing.T) {
	t.Parallel()

	info := validSingleInfo("payload.bin", 5, 16*1024)
	first := testDictionary(map[string][]byte{"announce": testBytes([]byte("https://old.example/a")), "info": info})
	second := testDictionary(map[string][]byte{"announce": testBytes([]byte("https://new.example/a/passkey")), "info": info})

	parsedFirst := mustParseV1(t, first, ValidationProfileStrictUpload)
	parsedSecond := mustParseV1(t, second, ValidationProfileStrictUpload)
	if parsedFirst.InfoHashV1 != parsedSecond.InfoHashV1 {
		t.Fatalf("info hashes differ: %s != %s", parsedFirst.InfoHashV1.Hex(), parsedSecond.InfoHashV1.Hex())
	}
	if parsedFirst.ObjectSHA256 == parsedSecond.ObjectSHA256 {
		t.Fatal("complete object digests should differ after outer announce rewrite")
	}
}

func TestParseV1LegacyImportPreservesUnsortedInfoBytes(t *testing.T) {
	t.Parallel()

	info := testDictionaryInOrder(
		testEntry{"name", testBytes([]byte("legacy.bin"))},
		testEntry{"length", testInteger(1)},
		testEntry{"piece length", testInteger(16 * 1024)},
		testEntry{"pieces", testBytes(bytes.Repeat([]byte{0x62}, sha1.Size))},
		testEntry{"private", testInteger(1)},
	)
	raw := testDictionary(map[string][]byte{"info": info})

	assertValidationCode(t, raw, ValidationProfileStrictUpload, CodeNonCanonicalBencode)
	parsed := mustParseV1(t, raw, ValidationProfileLegacyImport)
	want := sha1.Sum(info) // #nosec G401 -- test fixture for BEP 3.
	if parsed.InfoHashV1 != InfoHashV1(want) {
		t.Fatalf("InfoHashV1 = %s, want %x", parsed.InfoHashV1.Hex(), want)
	}
	if !hasCompatibilityFlag(parsed, CompatibilityUnsortedDictionary) {
		t.Fatalf("compatibility flags = %v", parsed.CompatibilityFlags)
	}
}

func TestParseV1AlwaysRejectsDuplicateDictionaryKeys(t *testing.T) {
	t.Parallel()

	info := testDictionaryInOrder(
		testEntry{"length", testInteger(1)},
		testEntry{"name", testBytes([]byte("duplicate.bin"))},
		testEntry{"piece length", testInteger(16 * 1024)},
		testEntry{"pieces", testBytes(bytes.Repeat([]byte{0x63}, sha1.Size))},
		testEntry{"private", testInteger(1)},
		testEntry{"private", testInteger(1)},
	)
	raw := testDictionary(map[string][]byte{"info": info})
	assertValidationCode(t, raw, ValidationProfileStrictUpload, CodeDuplicateDictionaryKey)
	assertValidationCode(t, raw, ValidationProfileLegacyImport, CodeDuplicateDictionaryKey)
}

func TestParseV1RequiresPrivateWithoutRewriting(t *testing.T) {
	t.Parallel()

	missing := testDictionary(map[string][]byte{
		"info": testDictionary(map[string][]byte{
			"length":       testInteger(1),
			"name":         testBytes([]byte("public.bin")),
			"piece length": testInteger(16 * 1024),
			"pieces":       testBytes(bytes.Repeat([]byte{0x64}, sha1.Size)),
		}),
	})
	wrongValue := testDictionary(map[string][]byte{
		"info": testDictionary(map[string][]byte{
			"length":       testInteger(1),
			"name":         testBytes([]byte("public.bin")),
			"piece length": testInteger(16 * 1024),
			"pieces":       testBytes(bytes.Repeat([]byte{0x64}, sha1.Size)),
			"private":      testInteger(0),
		}),
	})

	for _, profile := range []ValidationProfile{ValidationProfileStrictUpload, ValidationProfileLegacyImport} {
		assertValidationCode(t, missing, profile, CodePrivateRequired)
		assertValidationCode(t, wrongValue, profile, CodePrivateRequired)
	}
}

func TestInspectLegacyV1CanReconcileButCannotAdmitPublicObject(t *testing.T) {
	t.Parallel()

	info := testDictionary(map[string][]byte{
		"length":       testInteger(1),
		"name":         testBytes([]byte("legacy-public.bin")),
		"piece length": testInteger(16 * 1024),
		"pieces":       testBytes(bytes.Repeat([]byte{0x64}, sha1.Size)),
	})
	raw := testDictionary(map[string][]byte{"info": info})
	parsed, err := InspectLegacyV1(raw)
	if err != nil {
		t.Fatalf("InspectLegacyV1() error = %v", err)
	}
	if parsed.Private {
		t.Fatal("InspectLegacyV1() marked public object private")
	}
	want := sha1.Sum(info) // #nosec G401 -- test fixture for BEP 3.
	if parsed.InfoHashV1 != InfoHashV1(want) {
		t.Fatalf("InfoHashV1 = %s, want %x", parsed.InfoHashV1.Hex(), want)
	}
}

func TestParseV1RejectsV2BeforeOrdinaryV1Fields(t *testing.T) {
	t.Parallel()

	raw := testDictionary(map[string][]byte{
		"info": testDictionary(map[string][]byte{
			"meta version": testInteger(2),
		}),
	})
	assertValidationCode(t, raw, ValidationProfileStrictUpload, CodeUnsupportedVersion)
}

func TestValidationDiagnosticContainsOnlyParserLocationAndFixedReason(t *testing.T) {
	t.Parallel()

	raw := multiFileFixture("root", 16*1024, testList(testFile(nil, 1, "..")), 1)
	_, err := ParseV1(raw, ValidationProfileLegacyImport)
	diagnostic, ok := ValidationDiagnosticOf(err)
	if !ok || diagnostic.Code != CodeInvalidPath || diagnostic.Field != "info.files[0].path[0]" ||
		diagnostic.Offset < 0 || diagnostic.Reason != "path component is a relative traversal marker" {
		t.Fatalf("diagnostic = %+v, error = %v", diagnostic, err)
	}
}

func TestDetectMetainfoKindSeparatesLegacyCompatibilityFamilies(t *testing.T) {
	t.Parallel()

	v1Info := validSingleInfo("payload.bin", 5, 16*1024)
	hybridInfo := testDictionary(map[string][]byte{
		"file tree":    testDictionary(map[string][]byte{}),
		"length":       testInteger(5),
		"meta version": testInteger(2),
		"name":         testBytes([]byte("payload.bin")),
		"piece length": testInteger(16 * 1024),
		"pieces":       testBytes(bytes.Repeat([]byte{0x66}, sha1.Size)),
		"private":      testInteger(1),
	})
	tests := map[string]struct {
		raw  []byte
		want MetainfoKind
	}{
		"v1": {raw: testDictionary(map[string][]byte{"info": v1Info}), want: MetainfoKindV1},
		"hybrid": {
			raw: testDictionary(map[string][]byte{"info": hybridInfo}), want: MetainfoKindHybridV1V2,
		},
		"v2": {
			raw: testDictionary(map[string][]byte{
				"info": testDictionary(map[string][]byte{
					"file tree": testDictionary(map[string][]byte{}), "meta version": testInteger(2),
				}),
			}),
			want: MetainfoKindV2,
		},
		"bep30 merkle": {
			raw: testDictionary(map[string][]byte{
				"info": testDictionary(map[string][]byte{"root hash": testBytes(bytes.Repeat([]byte{0x67}, 20))}),
			}),
			want: MetainfoKindBEP30Merkle,
		},
	}
	for name, test := range tests {
		name, test := name, test
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := DetectMetainfoKind(test.raw, ValidationProfileLegacyImport)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("DetectMetainfoKind() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestInspectLegacyV1OrHybridMaterializesOnlyVerifiedV1View(t *testing.T) {
	t.Parallel()

	info := testDictionary(map[string][]byte{
		"file tree":    testDictionary(map[string][]byte{}),
		"length":       testInteger(5),
		"meta version": testInteger(2),
		"name":         testBytes([]byte("payload.bin")),
		"piece length": testInteger(16 * 1024),
		"pieces":       testBytes(bytes.Repeat([]byte{0x68}, sha1.Size)),
		"private":      testInteger(1),
	})
	raw := testDictionary(map[string][]byte{"info": info})
	assertValidationCode(t, raw, ValidationProfileLegacyImport, CodeUnsupportedVersion)

	parsed, err := InspectLegacyV1OrHybrid(raw)
	if err != nil {
		t.Fatalf("InspectLegacyV1OrHybrid() error = %v", err)
	}
	wantInfoHash := sha1.Sum(info) // #nosec G401 -- BEP 3 identity of the exact hybrid info value.
	if parsed.ParserVersion != ParserVersionLegacyHybridV1V2 ||
		parsed.InfoHashV1 != InfoHashV1(wantInfoHash) || parsed.TotalSizeBytes != 5 ||
		len(parsed.Files) != 1 || parsed.Files[0].DisplayPath != "payload.bin" ||
		!hasCompatibilityFlag(parsed, CompatibilityHybridV1V2) {
		t.Fatalf("parsed hybrid v1 view = %+v", parsed)
	}
}

func TestInspectLegacyV1OrHybridRejectsPureV2(t *testing.T) {
	t.Parallel()

	raw := testDictionary(map[string][]byte{
		"info": testDictionary(map[string][]byte{
			"file tree":    testDictionary(map[string][]byte{}),
			"meta version": testInteger(2),
			"name":         testBytes([]byte("payload")),
			"piece length": testInteger(16 * 1024),
		}),
	})
	_, err := InspectLegacyV1OrHybrid(raw)
	actual, ok := ValidationCodeOf(err)
	if !ok || actual != CodeUnsupportedVersion {
		t.Fatalf("InspectLegacyV1OrHybrid() error = %v, want %s", err, CodeUnsupportedVersion)
	}
}

func TestParseV1ValidatesPieceDigestCount(t *testing.T) {
	t.Parallel()

	raw := testDictionary(map[string][]byte{
		"info": testDictionary(map[string][]byte{
			"length":       testInteger(20_000),
			"name":         testBytes([]byte("two-pieces.bin")),
			"piece length": testInteger(16 * 1024),
			"pieces":       testBytes(bytes.Repeat([]byte{0x65}, sha1.Size)),
			"private":      testInteger(1),
		}),
	})
	assertValidationCode(t, raw, ValidationProfileStrictUpload, CodeInvalidPieces)
}

func TestParseV1ParsesBEP47PaddingWithoutCountingItAsPayload(t *testing.T) {
	t.Parallel()

	files := testList(
		testFile(nil, 3, "video", "part-a.bin"),
		testFile([]byte("p"), 13, ".pad", "13"),
		testFile(nil, 2, "video", "part-b.bin"),
	)
	info := testDictionary(map[string][]byte{
		"files":        files,
		"name":         testBytes([]byte("release")),
		"piece length": testInteger(16),
		"pieces":       testBytes(bytes.Repeat([]byte{0x66}, 2*sha1.Size)),
		"private":      testInteger(1),
	})
	parsed := mustParseV1(t, testDictionary(map[string][]byte{"info": info}), ValidationProfileStrictUpload)

	if !parsed.MultiFile || parsed.TotalSizeBytes != 18 || parsed.PayloadSizeBytes != 5 || parsed.PaddingFileCount != 1 {
		t.Fatalf("parsed padding summary = %+v", parsed)
	}
	if !parsed.Files[1].Padding || parsed.Files[1].DisplayPath != ".pad/13" {
		t.Fatalf("padding file = %+v", parsed.Files[1])
	}
}

func TestParseV1LegacyImportFlagsIrregularPadding(t *testing.T) {
	t.Parallel()

	files := testList(
		testFile(nil, 3, "video", "part-a.bin"),
		testFile([]byte("p"), 12, ".pad", "12"),
		testFile(nil, 2, "video", "part-b.bin"),
	)
	info := testDictionary(map[string][]byte{
		"files":        files,
		"name":         testBytes([]byte("release")),
		"piece length": testInteger(16),
		"pieces":       testBytes(bytes.Repeat([]byte{0x67}, 2*sha1.Size)),
		"private":      testInteger(1),
	})
	raw := testDictionary(map[string][]byte{"info": info})

	assertValidationCode(t, raw, ValidationProfileStrictUpload, CodeInvalidFileLayout)
	parsed := mustParseV1(t, raw, ValidationProfileLegacyImport)
	if !hasCompatibilityFlag(parsed, CompatibilityIrregularPadding) {
		t.Fatalf("compatibility flags = %v", parsed.CompatibilityFlags)
	}
}

func TestParseV1RejectsTraversalAndSymlinkEntries(t *testing.T) {
	t.Parallel()

	traversal := multiFileFixture(
		"unsafe",
		16*1024,
		testList(testFile(nil, 1, "..", "secret.txt")),
		1,
	)
	assertValidationCode(t, traversal, ValidationProfileStrictUpload, CodeInvalidPath)
	assertValidationCode(t, traversal, ValidationProfileLegacyImport, CodeInvalidPath)

	symlink := multiFileFixture(
		"unsafe",
		16*1024,
		testList(testFile([]byte("l"), 0, "link")),
		1,
	)
	assertValidationCode(t, symlink, ValidationProfileStrictUpload, CodeUnsupportedFileType)
}

func TestParseV1LegacyImportFlagsPortablePathConflicts(t *testing.T) {
	t.Parallel()

	files := testList(
		testFile(nil, 1, "CON"),
		testFile(nil, 1, "Readme.txt"),
		testFile(nil, 1, "README.txt"),
	)
	raw := multiFileFixture("legacy", 16*1024, files, 1)
	assertValidationCode(t, raw, ValidationProfileStrictUpload, CodeInvalidPath)

	parsed := mustParseV1(t, raw, ValidationProfileLegacyImport)
	if !hasCompatibilityFlag(parsed, CompatibilityNonPortablePath) ||
		!hasCompatibilityFlag(parsed, CompatibilityCaseCollidingPath) {
		t.Fatalf("compatibility flags = %v", parsed.CompatibilityFlags)
	}
}

func TestParseV1LegacyImportPreservesDuplicateFileEntries(t *testing.T) {
	t.Parallel()

	files := testList(
		testFile(nil, 1, "duplicate.bin"),
		testFile(nil, 1, "duplicate.bin"),
	)
	raw := multiFileFixture("legacy", 16*1024, files, 1)
	assertValidationCode(t, raw, ValidationProfileStrictUpload, CodeInvalidPath)

	parsed := mustParseV1(t, raw, ValidationProfileLegacyImport)
	if len(parsed.Files) != 2 || parsed.TotalSizeBytes != 2 ||
		parsed.Files[0].DisplayPath != parsed.Files[1].DisplayPath ||
		!hasCompatibilityFlag(parsed, CompatibilityDuplicatePath) {
		t.Fatalf("parsed duplicate path legacy object = %+v", parsed)
	}
}

func TestParseV1LegacyImportPreservesBoundedOverlongPathComponent(t *testing.T) {
	t.Parallel()

	component := strings.Repeat("界", 86) // 258 UTF-8 bytes, beyond portable component limits.
	raw := multiFileFixture("root", 16*1024, testList(testFile(nil, 1, component)), 1)
	assertValidationCode(t, raw, ValidationProfileStrictUpload, CodeInvalidPath)
	parsed := mustParseV1(t, raw, ValidationProfileLegacyImport)
	if len(parsed.Files) != 1 || parsed.Files[0].DisplayPath != component ||
		!hasCompatibilityFlag(parsed, CompatibilityOverlongPath) {
		t.Fatalf("legacy overlong path = %+v", parsed)
	}

	tooLong := strings.Repeat("x", maxPathBytes+1)
	tooLongRaw := multiFileFixture("root", 16*1024, testList(testFile(nil, 1, tooLong)), 1)
	assertValidationCode(t, tooLongRaw, ValidationProfileLegacyImport, CodeInvalidPath)
}

func TestParseV1LegacyUTF8AliasIsExplicit(t *testing.T) {
	t.Parallel()

	info := testDictionary(map[string][]byte{
		"length":       testInteger(1),
		"name":         testBytes([]byte{0xff}),
		"name.utf-8":   testBytes([]byte("legacy.bin")),
		"piece length": testInteger(16 * 1024),
		"pieces":       testBytes(bytes.Repeat([]byte{0x68}, sha1.Size)),
		"private":      testInteger(1),
	})
	raw := testDictionary(map[string][]byte{"info": info})
	assertValidationCode(t, raw, ValidationProfileStrictUpload, CodeInvalidName)

	parsed := mustParseV1(t, raw, ValidationProfileLegacyImport)
	if parsed.Name != "legacy.bin" || !hasCompatibilityFlag(parsed, CompatibilityLegacyUTF8Alias) {
		t.Fatalf("parsed legacy name = %+v", parsed)
	}
}

func TestParseV1AcceptsUnknownArbitraryPrecisionInteger(t *testing.T) {
	t.Parallel()

	info := testDictionary(map[string][]byte{
		"length":       testInteger(1),
		"name":         testBytes([]byte("large-extension.bin")),
		"piece length": testInteger(16 * 1024),
		"pieces":       testBytes(bytes.Repeat([]byte{0x69}, sha1.Size)),
		"private":      testInteger(1),
		"zz-extension": []byte("i" + strings.Repeat("9", 100) + "e"),
	})
	_ = mustParseV1(t, testDictionary(map[string][]byte{"info": info}), ValidationProfileStrictUpload)
}

func TestParseV1RejectsSizeOverflow(t *testing.T) {
	t.Parallel()

	files := testList(
		testFile(nil, math.MaxInt64, "a.bin"),
		testFile(nil, 1, "b.bin"),
	)
	raw := multiFileFixture("overflow", math.MaxInt64, files, 2)
	assertValidationCode(t, raw, ValidationProfileStrictUpload, CodeSizeOverflow)
}

func TestParseV1RejectsMalformedAndNonCanonicalBencode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  []byte
		code ValidationCode
	}{
		{name: "trailing bytes", raw: append(validSingleFixture("x", 1, 16*1024), '\n'), code: CodeMalformedBencode},
		{name: "leading zero string", raw: []byte("d04:info1:xe"), code: CodeNonCanonicalBencode},
		{name: "leading zero integer", raw: []byte("d4:infod6:lengthi01eee"), code: CodeNonCanonicalBencode},
		{name: "negative zero", raw: []byte("d4:infod6:lengthi-0eee"), code: CodeNonCanonicalBencode},
		{name: "unterminated dictionary", raw: []byte("d4:info"), code: CodeMalformedBencode},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assertValidationCode(t, test.raw, ValidationProfileStrictUpload, test.code)
		})
	}
}

func TestParseV1RejectsObjectBeyondBudget(t *testing.T) {
	t.Parallel()

	raw := make([]byte, MaxMetainfoBytes+1)
	assertValidationCode(t, raw, ValidationProfileStrictUpload, CodeObjectTooLarge)
}

// This optional local corpus is populated by the ignored reference snapshots
// documented in references/README.md. CI clones without those snapshots skip
// the cases; development machines use them to detect parser regressions against
// independent UNIT3D and Torrust fixtures without making either project a
// runtime dependency.
func TestLocalReferenceTorrentCorpus(t *testing.T) {
	t.Parallel()

	fixtures := []struct {
		path         string
		expectedCode ValidationCode
		expectedHash string
	}{
		{path: "torrust-index/tests/upgrades/from_v1_0_0_to_v2_0_0/fixtures/uploads/1.torrent", expectedCode: CodeUnsupportedVersion},
		{path: "torrust-index/tests/upgrades/from_v1_0_0_to_v2_0_0/fixtures/uploads/2.torrent", expectedCode: CodeUnsupportedVersion},
		{path: "torrust-index/tests/fixtures/torrents/6c690018c5786dbbb00161f62b0712d69296df97_with_custom_info_dict_key.torrent", expectedHash: "6c690018c5786dbbb00161f62b0712d69296df97"},
		{path: "torrust-index/tests/fixtures/torrents/MC_GRID.zip-3cd18ff2d3eec881207dcc5ca5a2c3a2a3afe462.torrent"},
		{path: "torrust-index/tests/fixtures/torrents/not-working-with-two-nodes.torrent"},
		{path: "torrust-index/tests/fixtures/torrents/working-with-one-node.torrent"},
		{path: "torrust-index/docs/media/mandelbrot_2048x2048_infohash_v1.png.torrent"},
		{path: "unit3d/tests/Resources/Pony Music - Mind Fragments (2014).torrent"},
	}
	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(filepath.Base(fixture.path), func(t *testing.T) {
			t.Parallel()
			fullPath := filepath.Join("..", "..", "..", "..", "..", "references", fixture.path)
			raw, err := os.ReadFile(fullPath)
			if os.IsNotExist(err) {
				t.Skip("local reference snapshot is not installed")
			}
			if err != nil {
				t.Fatalf("read reference fixture: %v", err)
			}
			parsed, err := InspectLegacyV1(raw)
			if fixture.expectedCode != "" {
				actual, ok := ValidationCodeOf(err)
				if !ok || actual != fixture.expectedCode {
					t.Fatalf("InspectLegacyV1(%s) error = %v, want code %s", fixture.path, err, fixture.expectedCode)
				}
				return
			}
			if err != nil {
				t.Fatalf("InspectLegacyV1(%s) error = %v", fixture.path, err)
			}
			captured := raw[parsed.InfoOffset : parsed.InfoOffset+parsed.InfoLength]
			want := sha1.Sum(captured) // #nosec G401 -- reference corpus verifies BEP 3 identity.
			if parsed.InfoHashV1 != InfoHashV1(want) || len(parsed.Files) == 0 || parsed.TotalSizeBytes <= 0 {
				t.Fatalf("parsed reference fixture = %+v", parsed)
			}
			if fixture.expectedHash != "" && parsed.InfoHashV1.Hex() != fixture.expectedHash {
				t.Fatalf("InfoHashV1 = %s, want reference hash %s", parsed.InfoHashV1.Hex(), fixture.expectedHash)
			}
		})
	}
}

func FuzzParseV1DoesNotPanic(f *testing.F) {
	f.Add(validSingleFixture("seed.bin", 1, 16*1024), string(ValidationProfileStrictUpload))
	f.Add([]byte("d4:infodee"), string(ValidationProfileLegacyImport))
	f.Add([]byte("not-bencode"), string(ValidationProfileStrictUpload))

	f.Fuzz(func(t *testing.T, raw []byte, profileText string) {
		profile := ValidationProfile(profileText)
		if !profile.valid() {
			profile = ValidationProfileStrictUpload
		}
		_, _ = ParseV1(raw, profile)
	})
}

type testEntry struct {
	key   string
	value []byte
}

func testBytes(value []byte) []byte {
	return append([]byte(fmt.Sprintf("%d:", len(value))), value...)
}

func testInteger(value int64) []byte {
	return []byte(fmt.Sprintf("i%de", value))
}

func testList(values ...[]byte) []byte {
	result := []byte{'l'}
	for _, value := range values {
		result = append(result, value...)
	}
	return append(result, 'e')
}

func testDictionary(values map[string][]byte) []byte {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(left, right int) bool {
		return bytes.Compare([]byte(keys[left]), []byte(keys[right])) < 0
	})
	entries := make([]testEntry, 0, len(keys))
	for _, key := range keys {
		entries = append(entries, testEntry{key: key, value: values[key]})
	}
	return testDictionaryInOrder(entries...)
}

func testDictionaryInOrder(entries ...testEntry) []byte {
	result := []byte{'d'}
	for _, entry := range entries {
		result = append(result, testBytes([]byte(entry.key))...)
		result = append(result, entry.value...)
	}
	return append(result, 'e')
}

func testFile(attributes []byte, length int64, path ...string) []byte {
	values := map[string][]byte{
		"length": testInteger(length),
	}
	components := make([][]byte, 0, len(path))
	for _, component := range path {
		components = append(components, testBytes([]byte(component)))
	}
	values["path"] = testList(components...)
	if attributes != nil {
		values["attr"] = testBytes(attributes)
	}
	return testDictionary(values)
}

func validSingleInfo(name string, length, pieceLength int64) []byte {
	pieceCount := length / pieceLength
	if length%pieceLength != 0 {
		pieceCount++
	}
	return testDictionary(map[string][]byte{
		"length":       testInteger(length),
		"name":         testBytes([]byte(name)),
		"piece length": testInteger(pieceLength),
		"pieces":       testBytes(bytes.Repeat([]byte{0x70}, int(pieceCount)*sha1.Size)),
		"private":      testInteger(1),
	})
}

func validSingleFixture(name string, length, pieceLength int64) []byte {
	return testDictionary(map[string][]byte{"info": validSingleInfo(name, length, pieceLength)})
}

func multiFileFixture(name string, pieceLength int64, files []byte, pieceCount int) []byte {
	return testDictionary(map[string][]byte{
		"info": testDictionary(map[string][]byte{
			"files":        files,
			"name":         testBytes([]byte(name)),
			"piece length": testInteger(pieceLength),
			"pieces":       testBytes(bytes.Repeat([]byte{0x71}, pieceCount*sha1.Size)),
			"private":      testInteger(1),
		}),
	})
}

func mustParseV1(t *testing.T, raw []byte, profile ValidationProfile) ParsedMetainfo {
	t.Helper()
	parsed, err := ParseV1(raw, profile)
	if err != nil {
		t.Fatalf("ParseV1() error = %v", err)
	}
	return parsed
}

func assertValidationCode(t *testing.T, raw []byte, profile ValidationProfile, expected ValidationCode) {
	t.Helper()
	_, err := ParseV1(raw, profile)
	if err == nil {
		t.Fatalf("ParseV1() error = nil, want code %s", expected)
	}
	actual, ok := ValidationCodeOf(err)
	if !ok || actual != expected {
		t.Fatalf("ParseV1() error = %v, code = %s, want %s", err, actual, expected)
	}
}

func hasCompatibilityFlag(parsed ParsedMetainfo, expected CompatibilityFlag) bool {
	for _, flag := range parsed.CompatibilityFlags {
		if flag == expected {
			return true
		}
	}
	return false
}
