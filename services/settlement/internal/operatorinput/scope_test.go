package operatorinput

import "testing"

func TestParseScopeUsesOneSelectorContract(t *testing.T) {
	t.Parallel()
	scope, err := ParseScope(ScopeValues{
		UserID: "a53e2a55-fdc3-40de-b4a5-98202af40cc6", TorrentID: "42",
		TorrentControlSequence: "3", SubjectControlSequence: "4",
	})
	if err != nil || scope.UserID == nil || scope.TorrentID == nil || *scope.TorrentID != 42 ||
		scope.TorrentControlSequence == nil || *scope.TorrentControlSequence != 3 ||
		scope.SubjectControlSequence == nil || *scope.SubjectControlSequence != 4 {
		t.Fatalf("ParseScope() scope=%+v error=%v", scope, err)
	}
}

func TestParseScopeRejectsNonPositiveControlSequence(t *testing.T) {
	t.Parallel()
	if _, err := ParseScope(ScopeValues{SubjectControlSequence: "0"}); err == nil {
		t.Fatal("ParseScope() accepted a zero control sequence")
	}
}
