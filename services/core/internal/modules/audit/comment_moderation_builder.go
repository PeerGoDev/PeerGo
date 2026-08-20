package audit

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/social"
)

type CommentModerationDecisionEventBuilder struct {
	pseudonymKey      []byte
	pseudonymKeyEpoch string
	newEventID        func() uuid.UUID
}

func NewCommentModerationDecisionEventBuilder(config RecorderConfig) (*CommentModerationDecisionEventBuilder, error) {
	config, err := validatedRecorderConfig(config)
	if err != nil {
		return nil, err
	}
	return &CommentModerationDecisionEventBuilder{
		pseudonymKey: config.PseudonymKey, pseudonymKeyEpoch: config.PseudonymKeyEpoch,
		newEventID: config.NewEventID,
	}, nil
}

func (builder *CommentModerationDecisionEventBuilder) BuildCommentModerationDecisionEvent(input social.CommentModerationAuditInput) (auditevent.Event, error) {
	if err := validateCommentModerationAuditInput(input); err != nil {
		return auditevent.Event{}, err
	}
	eventID := builder.newEventID()
	if eventID == uuid.Nil {
		return auditevent.Event{}, errors.New("audit event id generator returned nil")
	}
	beforeHash, err := commentModerationStateHash(input.Before)
	if err != nil {
		return auditevent.Event{}, fmt.Errorf("hash comment moderation before state: %w", err)
	}
	afterHash, err := commentModerationStateHash(input.After)
	if err != nil {
		return auditevent.Event{}, fmt.Errorf("hash comment moderation after state: %w", err)
	}
	torrentID, announcementID, postID := commentModerationTargetFields(input.Target)
	event := CommentModerationDecisionRecordedV4{
		SchemaVersion: CommentModerationDecisionSchemaVersion, EventType: CommentModerationDecisionEventType,
		EventID: eventID, OccurredAt: input.OccurredAt.UTC(), ModerationDecisionID: input.DecisionID,
		CaseID: input.Before.CaseID, CommentID: input.Before.CommentID,
		TargetKind: string(input.Target.Kind), TorrentID: torrentID, AnnouncementID: announcementID, PostID: postID,
		Decision: string(input.Decision), ReasonCode: string(input.ReasonCode), NoteSHA256: digestLabel(input.Note),
		ModeratorPseudonym:     subjectPseudonym(builder.pseudonymKey, input.ModeratorID),
		CommentAuthorPseudonym: subjectPseudonym(builder.pseudonymKey, input.CommentAuthorID),
		PseudonymKeyEpoch:      builder.pseudonymKeyEpoch, ReportCount: input.ReportCount,
		ExpectedCaseVersion: input.Before.CaseVersion, ResultingCaseVersion: input.After.CaseVersion,
		ExpectedCommentVersion: input.Before.CommentVersion, ResultingCommentVersion: input.After.CommentVersion,
		BeforeSHA256: beforeHash, AfterSHA256: afterHash,
		AuthorizationDecisionID: input.Authorization.ID, PolicyVersion: input.Authorization.PolicyVersion,
		Authority: DecisionAuthorityV1{
			RoleID: input.Authorization.RoleID, GrantID: input.Authorization.GrantID,
			GrantVersion: input.Authorization.GrantVersion, MandateID: input.Authorization.MandateID,
			EffectiveUntil: input.Authorization.EffectiveUntil.UTC(),
		},
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return auditevent.Event{}, fmt.Errorf("encode comment moderation audit event: %w", err)
	}
	if len(payload) > MaxEventPayloadBytes {
		return auditevent.Event{}, errors.New("comment moderation audit event exceeds outbox limit")
	}
	digest := sha256.Sum256(payload)
	return auditevent.Event{
		ID: eventID, Type: CommentModerationDecisionEventType,
		SchemaVersion: CommentModerationDecisionSchemaVersion,
		OccurredAt:    input.OccurredAt.UTC(), Payload: payload, PayloadSHA256: digest,
	}, nil
}

func validateCommentModerationAuditInput(input social.CommentModerationAuditInput) error {
	decision := input.Authorization
	if input.DecisionID == uuid.Nil || input.ModeratorID == uuid.Nil || input.CommentAuthorID == uuid.Nil ||
		input.ModeratorID == input.CommentAuthorID || !input.Target.Valid() ||
		utf8.RuneCountInString(input.Note) < social.MinModerationNoteRunes ||
		utf8.RuneCountInString(input.Note) > social.MaxModerationNoteRunes || input.ReportCount < 1 ||
		input.OccurredAt.IsZero() || input.Before.CaseID == uuid.Nil || input.Before.CommentID == uuid.Nil ||
		input.Before.CaseID != input.After.CaseID || input.Before.CommentID != input.After.CommentID ||
		input.Before.CaseState != social.CommentModerationCaseOpen || input.Before.CaseVersion < 1 ||
		input.After.CaseVersion != input.Before.CaseVersion+1 || input.Before.CommentVersion < 1 ||
		!decision.Allow || decision.Reason != authz.ReasonAllowed || decision.ID == uuid.Nil ||
		decision.PolicyVersion == "" || decision.GrantID == uuid.Nil || decision.GrantVersion < 1 ||
		decision.RoleID == "" || decision.MandateID == uuid.Nil || decision.EffectiveUntil.IsZero() {
		return errors.New("comment moderation audit event is missing required metadata")
	}
	switch input.Decision {
	case social.CommentModerationDismiss:
		if input.ReasonCode != social.CommentModerationNoViolation ||
			input.After.CaseState != social.CommentModerationCaseDismissed ||
			input.After.CommentState != input.Before.CommentState ||
			input.After.CommentVersion != input.Before.CommentVersion {
			return errors.New("comment moderation dismiss transition is invalid")
		}
	case social.CommentModerationHideComment:
		if !validCommentModerationViolationReason(input.ReasonCode) ||
			input.Before.CommentState != social.CommentVisible ||
			input.After.CaseState != social.CommentModerationCaseCommentHidden ||
			input.After.CommentState != social.CommentModeratorHidden ||
			input.After.CommentVersion != input.Before.CommentVersion+1 {
			return errors.New("comment moderation hide transition is invalid")
		}
	default:
		return errors.New("comment moderation decision is invalid")
	}
	return nil
}

func commentModerationTargetFields(target social.CommentTarget) (*int64, string, *uuid.UUID) {
	if target.Kind == social.CommentTargetTorrent {
		value := target.TorrentID
		return &value, "", nil
	}
	if target.Kind == social.CommentTargetAnnouncement {
		return nil, target.AnnouncementID, nil
	}
	value := target.PostPublicID
	return nil, "", &value
}

func validCommentModerationViolationReason(reason social.CommentModerationReasonCode) bool {
	switch reason {
	case social.CommentModerationSpam,
		social.CommentModerationHarassment,
		social.CommentModerationPersonalInformation,
		social.CommentModerationOffTopic,
		social.CommentModerationOther:
		return true
	default:
		return false
	}
}

func commentModerationStateHash(state social.CommentModerationAuditState) (string, error) {
	encoded, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	return digestLabel(string(encoded)), nil
}

var _ social.CommentModerationEventBuilder = (*CommentModerationDecisionEventBuilder)(nil)
