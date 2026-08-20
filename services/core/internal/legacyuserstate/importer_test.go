package legacyuserstate

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestSourceRowValidation(t *testing.T) {
	valid := sourceRow{
		LegacyID: 1, Uploaded: 10, Downloaded: 20,
		Karma: "123.45", PTCoin: "-1.5", Experience: "0",
	}
	if err := valid.validate(1); err != nil {
		t.Fatalf("validate() error = %v", err)
	}

	invalid := valid
	invalid.Experience = "-0.1"
	if err := invalid.validate(1); err == nil {
		t.Fatal("validate() accepted negative experience")
	}
}

func TestSourceRowFingerprintIsStableAndCoversActivity(t *testing.T) {
	first := sourceRow{
		LegacyID: 1, Karma: "1.0", PTCoin: "2.0", Experience: "3.0",
	}
	second := first
	if first.fingerprint() != second.fingerprint() {
		t.Fatal("equal rows produced different fingerprints")
	}
	second.LastActiveAt = pgtype.Timestamptz{Valid: true}
	if first.fingerprint() == second.fingerprint() {
		t.Fatal("activity change did not change fingerprint")
	}
}

func TestStatusFingerprintIsStableAndCoversAccessFlags(t *testing.T) {
	first := sourceRow{
		LegacyID:      1,
		Banned:        true,
		BannedAt:      pgtype.Timestamptz{Time: time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC), Valid: true},
		EmailVerified: true,
		VIPEnabled:    true,
	}
	second := first
	if first.statusFingerprint() != second.statusFingerprint() {
		t.Fatal("equal status rows produced different fingerprints")
	}
	second.DownloadRestricted = true
	if first.statusFingerprint() == second.statusFingerprint() {
		t.Fatal("download restriction did not change the status fingerprint")
	}
}

func TestAttendanceFingerprintIsStableAndCoversOpeningStatistics(t *testing.T) {
	date := time.Date(2026, time.August, 14, 0, 0, 0, 0, time.UTC)
	first := sourceRow{
		LegacyID:                   1,
		AttendanceStatsPresent:     true,
		AttendanceCurrentStreak:    7,
		AttendanceLongestStreak:    20,
		AttendanceTotalDays:        100,
		AttendanceRetroactiveCards: 2,
		AttendanceLastDate:         pgtype.Date{Time: date, Valid: true},
		AttendanceRecordDays:       100,
	}
	second := first
	if first.attendanceFingerprint() != second.attendanceFingerprint() {
		t.Fatal("equal attendance openings produced different fingerprints")
	}
	second.AttendanceTotalDays++
	if first.attendanceFingerprint() == second.attendanceFingerprint() {
		t.Fatal("attendance total did not change fingerprint")
	}
}

func TestSourceRowValidationRejectsInvalidAttendanceOpening(t *testing.T) {
	row := sourceRow{
		LegacyID: 1, Karma: "0", PTCoin: "0", Experience: "0",
		AttendanceStatsPresent:  true,
		AttendanceCurrentStreak: 8,
		AttendanceLongestStreak: 7,
		AttendanceTotalDays:     10,
		AttendanceLastDate: pgtype.Date{
			Time: time.Date(2026, time.August, 14, 0, 0, 0, 0, time.UTC), Valid: true,
		},
		AttendanceRecordDays: 10,
	}
	if err := row.validate(1); err == nil {
		t.Fatal("validate() accepted current streak above longest streak")
	}
}

func TestMagicConversionRateIsFrozen(t *testing.T) {
	if PTCoinToMagicRate != 5000 {
		t.Fatalf("PTCoinToMagicRate = %d, want 5000", PTCoinToMagicRate)
	}
}
