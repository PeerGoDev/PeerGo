package magicstore

import (
	"crypto/rand"
	"math/big"
)

// PvPOutcome is the result of one accepted challenge. CreatorWins is
// meaningless when Tie is set.
type PvPOutcome struct {
	Tie          bool
	CreatorWins  bool
	CreatorRoll  int
	OpponentRoll int
}

// RPSOutcome compares two rock-paper-scissors choices.
func RPSOutcome(creator, opponent RPSChoice) PvPOutcome {
	if creator == opponent {
		return PvPOutcome{Tie: true}
	}
	beats := map[RPSChoice]RPSChoice{RPSRock: RPSScissors, RPSScissors: RPSPaper, RPSPaper: RPSRock}
	return PvPOutcome{CreatorWins: beats[creator] == opponent}
}

// DiceOutcome rolls 1-100 for both sides and re-rolls ties, so a dice duel
// always produces a winner.
func DiceOutcome(roll func() (int, error)) (PvPOutcome, error) {
	for {
		creator, err := roll()
		if err != nil {
			return PvPOutcome{}, err
		}
		opponent, err := roll()
		if err != nil {
			return PvPOutcome{}, err
		}
		if creator != opponent {
			return PvPOutcome{CreatorWins: creator > opponent, CreatorRoll: creator, OpponentRoll: opponent}, nil
		}
	}
}

// PvPFee is the house cut of a decisive challenge, taken from the combined pot.
func PvPFee(stake, feeBPS int64) int64 {
	return stake * 2 * feeBPS / 10000
}

func secureRoll() (int, error) {
	value, err := rand.Int(rand.Reader, big.NewInt(100))
	if err != nil {
		return 0, err
	}
	return int(value.Int64()) + 1, nil
}

// drawScratchPrize picks a prize index with probability proportional to its
// weight using crypto/rand.
func drawScratchPrize(prizes []ScratchPrize) (int, error) {
	total := int64(0)
	for _, prize := range prizes {
		total += prize.Weight
	}
	if total <= 0 {
		return 0, ErrInput
	}
	value, err := rand.Int(rand.Reader, big.NewInt(total))
	if err != nil {
		return 0, err
	}
	target := value.Int64()
	for index, prize := range prizes {
		if target < prize.Weight {
			return index, nil
		}
		target -= prize.Weight
	}
	return len(prizes) - 1, nil
}
