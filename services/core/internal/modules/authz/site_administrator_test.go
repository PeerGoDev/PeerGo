package authz

import (
	"testing"

	"github.com/google/uuid"
)

func TestSiteAdministratorIDsAreStableAndSeparated(t *testing.T) {
	t.Parallel()

	userID := uuid.MustParse("0198f20a-6da8-7e51-9c64-111111111111")
	firstMandate, firstGrant := siteAdministratorIDs(userID)
	secondMandate, secondGrant := siteAdministratorIDs(userID)
	if firstMandate != secondMandate || firstGrant != secondGrant {
		t.Fatal("site administrator IDs are not stable across retries")
	}
	if firstMandate == uuid.Nil || firstGrant == uuid.Nil || firstMandate == firstGrant {
		t.Fatalf("site administrator IDs are not separated: mandate=%s grant=%s", firstMandate, firstGrant)
	}
	otherMandate, otherGrant := siteAdministratorIDs(uuid.New())
	if firstMandate == otherMandate || firstGrant == otherGrant {
		t.Fatal("site administrator IDs collide across users")
	}
}
