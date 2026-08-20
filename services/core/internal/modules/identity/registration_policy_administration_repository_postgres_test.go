package identity

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestRegistrationPolicyPersistencePreservesEmptyArrays(t *testing.T) {
	t.Parallel()

	params := registrationPolicyUpdateParams(UpdateRegistrationPolicyCommand{})
	if params.ReservedUsernames == nil {
		t.Fatal("reserved usernames became SQL NULL; want an empty PostgreSQL array")
	}
	if params.EmailDomains == nil {
		t.Fatal("email domains became SQL NULL; want an empty PostgreSQL array")
	}
	encoded, err := pgtype.NewMap().Encode(
		pgtype.TextArrayOID,
		pgtype.BinaryFormatCode,
		params.EmailDomains,
		nil,
	)
	if err != nil {
		t.Fatalf("encode empty email domain array: %v", err)
	}
	if encoded == nil {
		t.Fatal("pgx encoded empty email domains as SQL NULL")
	}

	audit := registrationPolicyAuditState(RegistrationPolicy{})
	if audit.ReservedUsernames == nil || audit.EmailDomains == nil {
		t.Fatal("empty registration policy collections became JSON null in audit evidence")
	}
}
