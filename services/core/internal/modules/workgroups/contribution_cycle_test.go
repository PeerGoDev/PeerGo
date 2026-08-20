package workgroups

import (
	"testing"
	"time"
)

func TestContributionActiveSecondsUsesImmutableTransitionTimeline(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(10 * 24 * time.Hour)
	transitions := []contributionMembershipTransition{
		{ToStatus: MembershipActive, OccurredAt: start.Add(-24 * time.Hour)},
		{ToStatus: MembershipSuspended, OccurredAt: start.Add(2 * 24 * time.Hour)},
		{ToStatus: MembershipActive, OccurredAt: start.Add(4 * 24 * time.Hour)},
		{ToStatus: MembershipEnded, OccurredAt: start.Add(9 * 24 * time.Hour)},
	}
	got := contributionActiveSeconds(transitions, start, end)
	want := int64(7 * 24 * time.Hour / time.Second)
	if got != want {
		t.Fatalf("contributionActiveSeconds() = %d, want %d", got, want)
	}
}

func TestContributionAssessmentNeverCallsPartialOrMissingEvidenceAFailure(t *testing.T) {
	t.Parallel()
	periodStart := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.AddDate(0, 1, 0)
	tests := []struct {
		name        string
		cycle       ContributionCycle
		wantState   ContributionAssessmentState
		wantExplain ContributionExplanationCode
	}{
		{
			name: "partial membership",
			cycle: ContributionCycle{
				ActiveSeconds: 100, FullPeriodActive: false,
				EvidenceState: ContributionEvidenceComplete,
			},
			wantState: ContributionAssessmentNotAssessable, wantExplain: ContributionExplanationPartialMembership,
		},
		{
			name: "missing evidence",
			cycle: ContributionCycle{
				ActiveSeconds: 100, FullPeriodActive: true,
				EvidenceState: ContributionEvidenceUnavailable,
			},
			wantState: ContributionAssessmentIndeterminate, wantExplain: ContributionExplanationEvidenceUnavailable,
		},
		{
			name: "open month",
			cycle: ContributionCycle{
				ActiveSeconds: 100, FullPeriodActive: true,
				EvidenceState: ContributionEvidenceCollecting,
				CurrentValue:  2, TargetValue: 20,
				ObservedAt:   periodStart.Add(10 * 24 * time.Hour),
				PeriodEndsAt: periodEnd,
			},
			wantState: ContributionAssessmentInProgress, wantExplain: ContributionExplanationPeriodInProgress,
		},
		{
			name: "closed below target",
			cycle: ContributionCycle{
				ActiveSeconds: 100, FullPeriodActive: true,
				EvidenceState: ContributionEvidenceComplete,
				CurrentValue:  12, TargetValue: 20,
				ObservedAt: periodEnd, PeriodEndsAt: periodEnd,
			},
			wantState: ContributionAssessmentNotMet, wantExplain: ContributionExplanationBelowTarget,
		},
	}
	for _, testCase := range tests {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			state, explanation := contributionAssessment(testCase.cycle)
			if state != testCase.wantState || explanation != testCase.wantExplain {
				t.Fatalf("contributionAssessment() = %s/%s, want %s/%s", state, explanation, testCase.wantState, testCase.wantExplain)
			}
		})
	}
}

func TestContributionReminderAllowedOnlyForReliableBelowTargetCycle(t *testing.T) {
	t.Parallel()
	eligible := ContributionCycle{
		EvidenceState: ContributionEvidenceCollecting,
		FullPeriodActive: true,
		CurrentValue: 5,
		TargetValue: 10,
		AssessmentState: ContributionAssessmentInProgress,
	}
	if !contributionReminderAllowed(eligible) {
		t.Fatal("eligible in-progress cycle was rejected")
	}
	for name, mutate := range map[string]func(*ContributionCycle){
		"already met": func(cycle *ContributionCycle) { cycle.CurrentValue = cycle.TargetValue },
		"partial membership": func(cycle *ContributionCycle) { cycle.FullPeriodActive = false },
		"missing evidence": func(cycle *ContributionCycle) { cycle.EvidenceState = ContributionEvidenceIncomplete },
		"indeterminate": func(cycle *ContributionCycle) { cycle.AssessmentState = ContributionAssessmentIndeterminate },
	} {
		t.Run(name, func(t *testing.T) {
			cycle := eligible
			mutate(&cycle)
			if contributionReminderAllowed(cycle) {
				t.Fatalf("contributionReminderAllowed(%s) = true", name)
			}
		})
	}
}
