package identity

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

// PublicUserProfileRepository owns only the bounded member-directory read.
// Keeping this port outside Repository prevents login and staff-session tests
// from acquiring a dependency on the public profile surface.
type PublicUserProfileRepository interface {
	PublicProfileByUsername(context.Context, string, time.Time) (PublicUserProfile, error)
}

type PublicUserProfileSessionAuthenticator interface {
	CurrentSession(context.Context, string) (WebSession, error)
}

type PublicUserProfileService struct {
	sessions   PublicUserProfileSessionAuthenticator
	repository PublicUserProfileRepository
	authorizer authz.Authorizer
	now        func() time.Time
}

func NewPublicUserProfileService(
	sessions PublicUserProfileSessionAuthenticator,
	repository PublicUserProfileRepository,
	authorizer authz.Authorizer,
	now func() time.Time,
) (*PublicUserProfileService, error) {
	if sessions == nil || repository == nil || authorizer == nil {
		return nil, errors.New("public user profile service dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	return &PublicUserProfileService{
		sessions: sessions, repository: repository, authorizer: authorizer, now: now,
	}, nil
}

// PublicProfile authenticates and authorizes the requesting member before
// resolving another member's small public projection. Unknown, inactive and
// account-restricted targets remain indistinguishable at the repository edge.
func (service *PublicUserProfileService) PublicProfile(ctx context.Context, cookieToken, username string) (PublicUserProfile, error) {
	session, err := service.sessions.CurrentSession(ctx, cookieToken)
	if err != nil {
		return PublicUserProfile{}, err
	}
	username = strings.TrimSpace(username)
	if username == "" || utf8.RuneCountInString(username) > 64 {
		return PublicUserProfile{}, ErrInvalidInput
	}
	now := service.now().UTC()
	if _, err := authz.AuthorizeWebMemberAction(ctx, service.authorizer, session.User.ID, authz.ActionUserProfileReadMember, now); err != nil {
		return PublicUserProfile{}, err
	}
	return service.repository.PublicProfileByUsername(ctx, username, now)
}

// WebAPIService composes the two identity surfaces required by the HTTP
// adapter while keeping their constructors and responsibilities independent.
type WebAPIService struct {
	*Service
	*PublicUserProfileService
	*AccountProfileService
	*AvatarService
	*AccountAccessAppealService
	*DownloadRestrictionAppealService
}

func NewWebAPIService(
	sessions *Service,
	profiles *PublicUserProfileService,
	accountProfiles *AccountProfileService,
	avatars *AvatarService,
	appeals *AccountAccessAppealService,
	downloadAppeals *DownloadRestrictionAppealService,
) (*WebAPIService, error) {
	if sessions == nil || profiles == nil || accountProfiles == nil || avatars == nil ||
		appeals == nil || downloadAppeals == nil {
		return nil, errors.New("web identity API services are required")
	}
	return &WebAPIService{
		Service: sessions, PublicUserProfileService: profiles,
		AccountProfileService: accountProfiles, AvatarService: avatars,
		AccountAccessAppealService:       appeals,
		DownloadRestrictionAppealService: downloadAppeals,
	}, nil
}
