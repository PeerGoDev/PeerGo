package reseedreward

const OfferUpcoming = "upcoming"

// Offer is the member-visible terms for an upcoming or recovered round.
// It carries neither the winner's identity nor internal policy identifiers.
type Offer struct {
	Status        string
	RequiredHours int32
	MagicAmount   int64
}

func OfferFromProjection(status string, requiredHours int32, magicAmount int64) *Offer {
	if status == "" {
		return nil
	}
	return &Offer{Status: status, RequiredHours: requiredHours, MagicAmount: magicAmount}
}
