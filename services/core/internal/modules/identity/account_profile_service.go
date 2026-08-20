package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

const maxSelfServiceDisplayNameRunes = 40

type UpdateMyProfileInput struct {
	DisplayName string
}

// AccountProfileRepository is intentionally limited to the current user's
// public projection. Email, credential and avatar storage do not enter this
// mutation merely because they appear beside the nickname in the Web UI.
type AccountProfileRepository interface {
	UpdateMyDisplayName(context.Context, uuid.UUID, string, time.Time) (User, error)
}

type AccountProfileService struct {
	sessions   WebSessionAuthenticator
	repository AccountProfileRepository
	authorizer authz.Authorizer
	now        func() time.Time
}

func NewAccountProfileService(sessions WebSessionAuthenticator, repository AccountProfileRepository, authorizer authz.Authorizer, now func() time.Time) (*AccountProfileService, error) {
	if sessions == nil || repository == nil || authorizer == nil {
		return nil, errors.New("account profile service dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	return &AccountProfileService{sessions: sessions, repository: repository, authorizer: authorizer, now: now}, nil
}

// UpdateProfile authenticates the session-bound write, proves the dedicated
// self-service permission, and persists only the normalized public nickname.
func (service *AccountProfileService) UpdateProfile(ctx context.Context, cookieToken, csrfToken string, input UpdateMyProfileInput) (User, error) {
	displayName := strings.TrimSpace(input.DisplayName)
	if displayName == "" || utf8.RuneCountInString(displayName) > maxSelfServiceDisplayNameRunes {
		return User{}, ErrInvalidInput
	}
	session, err := service.sessions.AuthenticateWrite(ctx, cookieToken, csrfToken)
	if err != nil {
		return User{}, err
	}
	now := service.now().UTC()
	if _, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionAccountProfileUpdateSelf, now); err != nil {
		return User{}, err
	}
	user, err := service.repository.UpdateMyDisplayName(ctx, session.User.ID, displayName, now)
	if err != nil {
		return User{}, fmt.Errorf("update account profile: %w", err)
	}
	return user, nil
}
