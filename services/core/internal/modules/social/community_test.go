package social

import (
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestExtractTopicsNormalizesAndDeduplicates(t *testing.T) {
	t.Parallel()
	topics := extractTopics("#周末片单 和 #低功耗做种 以及重复的 #周末片单 #PEERGO #peergo")
	want := []string{"周末片单", "低功耗做种", "PEERGO"}
	if len(topics) != len(want) {
		t.Fatalf("extractTopics() = %#v, want %#v", topics, want)
	}
	for index := range want {
		if topics[index] != want[index] {
			t.Fatalf("extractTopics()[%d] = %q, want %q", index, topics[index], want[index])
		}
	}
}

func TestCreatePostDigestCoversCommunityFeaturesAndLegacyRetries(t *testing.T) {
	t.Parallel()
	body := "一条带功能的动态 #测试"
	mediaID := uuid.New()
	closesAt := time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC)
	base := createPostInputSHA256(body, "general", nil, nil, nil)
	withBoard := createPostInputSHA256(body, "resources", nil, nil, nil)
	withMedia := createPostInputSHA256(body, "general", []uuid.UUID{mediaID}, nil, nil)
	withPoll := createPostInputSHA256(body, "general", nil, &CreatePollInput{Question: "可以吗？", Options: []string{"可以", "不可以"}, ClosesAt: &closesAt}, nil)
	withPacket := createPostInputSHA256(body, "general", nil, nil, &CreateRedPacketInput{TotalAmount: 20, ClaimCount: 4})
	for name, digest := range map[string][sha256.Size]byte{"board": withBoard, "media": withMedia, "poll": withPoll, "packet": withPacket} {
		if digest == base {
			t.Fatalf("%s did not affect create digest", name)
		}
	}

	legacy := sha256.Sum256([]byte(body))
	legacyCommand := createPostCommand{Body: body, BoardID: "general", CreateBodySHA256: base}
	if !createPostDigestMatches(legacy[:], legacyCommand) {
		t.Fatal("legacy body-only retry was rejected")
	}
	legacyCommand.MediaIDs = []uuid.UUID{mediaID}
	if createPostDigestMatches(legacy[:], legacyCommand) {
		t.Fatal("legacy request accepted a newly added attachment")
	}
}

func TestCommunityFeatureValidation(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 23, 12, 0, 0, 0, time.UTC)
	validPoll := &CreatePollInput{Question: "更喜欢哪个？", Options: []string{"A", "B"}}
	if err := validateCreatePoll(validPoll, now); err != nil {
		t.Fatalf("validateCreatePoll(valid) error = %v", err)
	}
	if err := validateCreatePoll(&CreatePollInput{Question: "重复", Options: []string{"A", " a "}}, now); !errors.Is(err, ErrPostInput) {
		t.Fatalf("validateCreatePoll(duplicate) error = %v", err)
	}
	if err := validateCreateRedPacket(&CreateRedPacketInput{TotalAmount: 3, ClaimCount: 4}); !errors.Is(err, ErrPostInput) {
		t.Fatalf("validateCreateRedPacket() error = %v", err)
	}
	if err := validateBoardInput("影音-闲聊", "影音闲聊", "", "clapperboard", "blue", 30, "这是一次有效的后台变更原因"); !errors.Is(err, ErrPostInput) {
		t.Fatalf("validateBoardInput(invalid id) error = %v", err)
	}
}
