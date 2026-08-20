package policy

import "fmt"

// Snapshot is every traffic-affecting rule resolved for one user, torrent, and
// immutable policy interval. Access grants and H&R rules are intentionally not
// part of this value because they answer different questions.
type Snapshot struct {
	Revision  RuleRef
	Profile   Profile
	Promotion ResolvedPromotion
	Benefits  Benefits
	Seedbox   *SeedboxPenalty
	Speed     *SpeedPenalty
}

func (snapshot Snapshot) validate() error {
	if err := snapshot.Revision.validate(); err != nil {
		return err
	}
	if snapshot.Revision.Source != SourcePolicyRevision {
		return fmt.Errorf("%w: snapshot revision has source %q", ErrInvalidRule, snapshot.Revision.Source)
	}
	if err := snapshot.Profile.validate(); err != nil {
		return err
	}
	if err := snapshot.Promotion.validate(); err != nil {
		return err
	}
	if snapshot.Promotion.Profile != snapshot.Profile {
		return fmt.Errorf("%w: promotion profile %q does not match snapshot %q", ErrInvalidRule, snapshot.Promotion.Profile, snapshot.Profile)
	}

	benefits := snapshot.Benefits
	if benefits.Group != nil {
		if err := benefits.Group.validate(SourceUserGroup); err != nil {
			return err
		}
	}
	if benefits.AccountTier != nil {
		if err := benefits.AccountTier.validate(SourceVIP, SourceDonor); err != nil {
			return err
		}
	}
	if benefits.PersonalFreeleech != nil {
		if err := validateReferenceSource(*benefits.PersonalFreeleech, SourcePersonalFreeleech); err != nil {
			return err
		}
	}
	if benefits.FreeleechToken != nil {
		if err := validateReferenceSource(*benefits.FreeleechToken, SourceFreeleechToken); err != nil {
			return err
		}
	}
	if benefits.Uploader != nil {
		if err := benefits.Uploader.validate(SourceUploader); err != nil {
			return err
		}
		if benefits.Uploader.Factors.Download != OneX {
			return fmt.Errorf("%w: uploader benefit cannot change download", ErrInvalidRule)
		}
	}
	if benefits.Medal != nil {
		if err := benefits.Medal.validate(SourceMedal); err != nil {
			return err
		}
	}
	if snapshot.Seedbox != nil {
		if err := snapshot.Seedbox.validate(); err != nil {
			return err
		}
	}
	if snapshot.Speed != nil {
		if err := snapshot.Speed.validate(); err != nil {
			return err
		}
	}

	if snapshot.Profile == ProfilePtYesV1 && (benefits.PersonalFreeleech != nil || benefits.FreeleechToken != nil) {
		return fmt.Errorf("%w: PtYes compatibility does not accept personal freeleech or tokens", ErrUnsupportedFeature)
	}
	if snapshot.Profile == ProfilePtYesV1 && benefits.AccountTier != nil &&
		(benefits.AccountTier.Rule.Source != SourceVIP ||
			benefits.AccountTier.Factors.Upload != OneX || benefits.AccountTier.Factors.Download != 0) {
		return fmt.Errorf("%w: PtYes compatibility accepts only the legacy VIP free-download account tier", ErrUnsupportedFeature)
	}
	if snapshot.Profile == ProfilePtYesV1 && benefits.Group != nil &&
		(benefits.Group.Factors.Upload != OneX || benefits.Group.Factors.Download != 0) {
		return fmt.Errorf("%w: PtYes compatibility accepts only the retention free-download group benefit", ErrUnsupportedFeature)
	}
	return nil
}

func validateReferenceSource(reference RuleRef, source Source) error {
	if err := reference.validate(); err != nil {
		return err
	}
	if reference.Source != source {
		return fmt.Errorf("%w: expected source %q, got %q", ErrInvalidRule, source, reference.Source)
	}
	return nil
}

type Operation string

const (
	OperationReplace         Operation = "replace"
	OperationFavorableMerge  Operation = "favorable_merge"
	OperationForceFree       Operation = "force_free"
	OperationMultiply        Operation = "multiply"
	OperationPenalty         Operation = "penalty"
	OperationPenaltyOverride Operation = "penalty_override"
)

// Application is compact, immutable explanation data for the ledger entry.
// Factors describe the rule itself; Result stores the exact rounded byte totals.
type Application struct {
	Rule      RuleRef
	Operation Operation
	Factors   Factors
}

type Result struct {
	PolicyRevision    RuleRef
	Profile           Profile
	RawUploaded       uint64
	RawDownloaded     uint64
	CreditedUploaded  uint64
	ChargedDownloaded uint64
	Applications      []Application
}

// SettleDelta applies one immutable policy snapshot to a trustworthy raw delta.
func SettleDelta(snapshot Snapshot, rawUploaded, rawDownloaded uint64) (Result, error) {
	if err := snapshot.validate(); err != nil {
		return Result{}, err
	}

	result := Result{
		PolicyRevision: snapshot.Revision,
		Profile:        snapshot.Profile,
		RawUploaded:    rawUploaded,
		RawDownloaded:  rawDownloaded,
	}
	if snapshot.Speed != nil {
		return settleSpeedPenalty(result, *snapshot.Speed)
	}
	if snapshot.Profile == ProfilePtYesV1 {
		return settlePtYes(result, snapshot)
	}
	return settlePeerGo(result, snapshot)
}

func settleSpeedPenalty(result Result, penalty SpeedPenalty) (Result, error) {
	uploadFactor := OneX
	if penalty.SuppressUpload {
		uploadFactor = 0
	}
	var err error
	result.CreditedUploaded, err = uploadFactor.Apply(result.RawUploaded)
	if err != nil {
		return Result{}, err
	}
	result.ChargedDownloaded, err = penalty.DownloadFactor.Apply(result.RawDownloaded)
	if err != nil {
		return Result{}, err
	}
	result.Applications = append(result.Applications, Application{
		Rule: penalty.Rule, Operation: OperationPenaltyOverride,
		Factors: Factors{Upload: uploadFactor, Download: penalty.DownloadFactor},
	})
	return result, nil
}

func settlePeerGo(result Result, snapshot Snapshot) (Result, error) {
	factors := snapshot.Promotion.Factors
	for _, match := range snapshot.Promotion.Matches {
		result.Applications = append(result.Applications, Application{Rule: match.Rule, Operation: OperationFavorableMerge, Factors: match.Factors})
	}

	grants := []*FactorGrant{snapshot.Benefits.Group, snapshot.Benefits.AccountTier, snapshot.Benefits.Uploader, snapshot.Benefits.Medal}
	for _, grant := range grants {
		if grant == nil {
			continue
		}
		factors = favorable(factors, grant.Factors)
		result.Applications = append(result.Applications, Application{Rule: grant.Rule, Operation: OperationFavorableMerge, Factors: grant.Factors})
	}
	if snapshot.Benefits.PersonalFreeleech != nil {
		factors.Download = 0
		result.Applications = append(result.Applications, Application{
			Rule: *snapshot.Benefits.PersonalFreeleech, Operation: OperationForceFree,
			Factors: Factors{Upload: OneX, Download: 0},
		})
	}
	if snapshot.Benefits.FreeleechToken != nil {
		factors.Download = 0
		result.Applications = append(result.Applications, Application{
			Rule: *snapshot.Benefits.FreeleechToken, Operation: OperationForceFree,
			Factors: Factors{Upload: OneX, Download: 0},
		})
	}

	var err error
	result.CreditedUploaded, err = factors.Upload.Apply(result.RawUploaded)
	if err != nil {
		return Result{}, err
	}
	result.ChargedDownloaded, err = factors.Download.Apply(result.RawDownloaded)
	if err != nil {
		return Result{}, err
	}
	if snapshot.Seedbox != nil {
		result.CreditedUploaded, err = snapshot.Seedbox.UploadFactor.Apply(result.CreditedUploaded)
		if err != nil {
			return Result{}, err
		}
		result.ChargedDownloaded, err = snapshot.Seedbox.DownloadFactor.Apply(result.ChargedDownloaded)
		if err != nil {
			return Result{}, err
		}
		result.Applications = append(result.Applications, Application{
			Rule: snapshot.Seedbox.Rule, Operation: OperationPenalty,
			Factors: Factors{Upload: snapshot.Seedbox.UploadFactor, Download: snapshot.Seedbox.DownloadFactor},
		})
	}
	return result, nil
}

func settlePtYes(result Result, snapshot Snapshot) (Result, error) {
	factors := snapshot.Promotion.Factors
	for _, match := range snapshot.Promotion.Matches {
		result.Applications = append(result.Applications, Application{Rule: match.Rule, Operation: OperationReplace, Factors: match.Factors})
	}
	if snapshot.Benefits.Uploader != nil {
		factors = favorable(factors, snapshot.Benefits.Uploader.Factors)
		result.Applications = append(result.Applications, Application{
			Rule: snapshot.Benefits.Uploader.Rule, Operation: OperationFavorableMerge, Factors: snapshot.Benefits.Uploader.Factors,
		})
	}
	if snapshot.Benefits.Group != nil {
		factors = favorable(factors, snapshot.Benefits.Group.Factors)
		result.Applications = append(result.Applications, Application{
			Rule: snapshot.Benefits.Group.Rule, Operation: OperationFavorableMerge, Factors: snapshot.Benefits.Group.Factors,
		})
	}
	if snapshot.Benefits.AccountTier != nil {
		factors = favorable(factors, snapshot.Benefits.AccountTier.Factors)
		result.Applications = append(result.Applications, Application{
			Rule: snapshot.Benefits.AccountTier.Rule, Operation: OperationFavorableMerge, Factors: snapshot.Benefits.AccountTier.Factors,
		})
	}

	var err error
	result.CreditedUploaded, err = factors.Upload.Apply(result.RawUploaded)
	if err != nil {
		return Result{}, err
	}
	result.ChargedDownloaded, err = factors.Download.Apply(result.RawDownloaded)
	if err != nil {
		return Result{}, err
	}
	if snapshot.Benefits.Medal != nil {
		result.CreditedUploaded, err = snapshot.Benefits.Medal.Factors.Upload.Apply(result.CreditedUploaded)
		if err != nil {
			return Result{}, err
		}
		result.ChargedDownloaded, err = snapshot.Benefits.Medal.Factors.Download.Apply(result.ChargedDownloaded)
		if err != nil {
			return Result{}, err
		}
		result.Applications = append(result.Applications, Application{
			Rule: snapshot.Benefits.Medal.Rule, Operation: OperationMultiply, Factors: snapshot.Benefits.Medal.Factors,
		})
	}
	if snapshot.Seedbox != nil {
		result.CreditedUploaded, err = snapshot.Seedbox.UploadFactor.Apply(result.CreditedUploaded)
		if err != nil {
			return Result{}, err
		}
		result.ChargedDownloaded, err = snapshot.Seedbox.DownloadFactor.Apply(result.ChargedDownloaded)
		if err != nil {
			return Result{}, err
		}
		result.Applications = append(result.Applications, Application{
			Rule: snapshot.Seedbox.Rule, Operation: OperationPenalty,
			Factors: Factors{Upload: snapshot.Seedbox.UploadFactor, Download: snapshot.Seedbox.DownloadFactor},
		})
	}
	return result, nil
}
