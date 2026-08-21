package legacytorrents

import "testing"

func TestMigrationStatusCheckpointsComplete(t *testing.T) {
	complete := MigrationStatus{
		ExpectedUsers: 12, ExpectedTorrents: 10,
		ImportedUsers: 12, ImportedTorrents: 9, ImportedTorrentObjects: 9,
		ExcludedTorrents: 1, ExcludedTorrentObjects: 1,
		UserMappings: 12, TorrentMappings: 9, VerifiedPreferredObjects: 9,
		SeedboxSourceRows: 2, SeedboxEnabledRows: 2,
		SeedboxExpectedBindings: 3, SeedboxImportedBindings: 3,
		SeedboxPolicySequence: 4,
		PersonalStateReceipts: 1,
		BookmarkSourceRows:    10, BookmarkEvidenceRows: 10,
		InvitationSourceRows: 4, InvitationEvidenceRows: 4,
		InvitationRelationships: 3, InvitationUnresolvedRows: 1,
		HaremRewardUsers: 2, InviteRewardUsers: 1,
		InvitationRewardEvidence: 3,
	}
	if !complete.CheckpointsComplete() {
		t.Fatal("complete migration status was reported incomplete")
	}

	tests := []struct {
		name   string
		mutate func(*MigrationStatus)
	}{
		{name: "missing user", mutate: func(value *MigrationStatus) { value.ImportedUsers-- }},
		{name: "missing torrent", mutate: func(value *MigrationStatus) { value.ImportedTorrents-- }},
		{name: "missing object", mutate: func(value *MigrationStatus) { value.ImportedTorrentObjects-- }},
		{name: "unmatched exclusion", mutate: func(value *MigrationStatus) { value.ExcludedTorrentObjects-- }},
		{name: "unresolved discrepancy", mutate: func(value *MigrationStatus) { value.UnresolvedDiscrepancies++ }},
		{name: "missing user mapping", mutate: func(value *MigrationStatus) { value.UserMappings-- }},
		{name: "missing torrent mapping", mutate: func(value *MigrationStatus) { value.TorrentMappings-- }},
		{name: "unverified object", mutate: func(value *MigrationStatus) { value.VerifiedPreferredObjects-- }},
		{name: "missing seedbox receipt", mutate: func(value *MigrationStatus) { value.SeedboxPolicySequence = 0 }},
		{name: "missing seedbox binding", mutate: func(value *MigrationStatus) { value.SeedboxImportedBindings-- }},
		{name: "invalid seedbox inventory", mutate: func(value *MigrationStatus) { value.SeedboxEnabledRows++ }},
		{name: "missing personal state receipt", mutate: func(value *MigrationStatus) { value.PersonalStateReceipts = 0 }},
		{name: "missing bookmark evidence", mutate: func(value *MigrationStatus) { value.BookmarkEvidenceRows-- }},
		{name: "missing invitation evidence", mutate: func(value *MigrationStatus) { value.InvitationEvidenceRows-- }},
		{name: "missing invitation disposition", mutate: func(value *MigrationStatus) { value.InvitationRelationships-- }},
		{name: "missing reward evidence", mutate: func(value *MigrationStatus) { value.InvitationRewardEvidence-- }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			status := complete
			test.mutate(&status)
			if status.CheckpointsComplete() {
				t.Fatal("incomplete migration status was reported complete")
			}
		})
	}
}

func TestMigrationStatusTrackerProjectionDrained(t *testing.T) {
	status := MigrationStatus{
		State: "reconciled", TrackerProjectionSequence: 9, TrackerOutboxSequence: 9,
	}
	if !status.TrackerProjectionDrained() {
		t.Fatal("drained reconciled Tracker projection was reported pending")
	}
	status.TrackerPendingEvents = 1
	if status.TrackerProjectionDrained() {
		t.Fatal("pending Tracker projection was reported drained")
	}
}
