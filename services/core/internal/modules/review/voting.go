package review

// resolveRound implements the deliberately asymmetric PtYes rule: publishing
// requires stronger agreement than rejection. With three initial votes, 3:0
// publishes, 0:3 or 1:2 rejects, and 2:1 asks for a fourth vote. The fourth
// vote publishes at 3:1 and escalates a 2:2 split to staff.
func resolveRound(approveCount, rejectCount int) RoundOutcome {
	total := approveCount + rejectCount
	if approveCount < 0 || rejectCount < 0 || total < RequiredReviewVotes {
		return RoundWaiting
	}
	if total == RequiredReviewVotes {
		switch {
		case rejectCount == 0:
			return RoundPublished
		case approveCount == 0 || rejectCount > approveCount:
			return RoundRejected
		default:
			return RoundWaiting
		}
	}
	if total == MaximumReviewVotes && approveCount == rejectCount {
		return RoundEscalated
	}
	if approveCount > rejectCount {
		return RoundPublished
	}
	return RoundRejected
}
