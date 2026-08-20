package progression

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
)

type contributionSettlementRepositoryStub struct {
	uploadResults  []ContributionSettlementResult
	publishResults []ContributionSettlementResult
	accountResults []ContributionSettlementResult
	uploadError    error
}

func (stub *contributionSettlementRepositoryStub) SettleNextUpload(context.Context, time.Time) (ContributionSettlementResult, bool, error) {
	if stub.uploadError != nil {
		return ContributionSettlementResult{}, false, stub.uploadError
	}
	if len(stub.uploadResults) == 0 {
		return ContributionSettlementResult{}, false, nil
	}
	result := stub.uploadResults[0]
	stub.uploadResults = stub.uploadResults[1:]
	return result, true, nil
}

func (stub *contributionSettlementRepositoryStub) SettleNextTorrentPublish(context.Context, time.Time) (ContributionSettlementResult, bool, error) {
	if len(stub.publishResults) == 0 {
		return ContributionSettlementResult{}, false, nil
	}
	result := stub.publishResults[0]
	stub.publishResults = stub.publishResults[1:]
	return result, true, nil
}

func (stub *contributionSettlementRepositoryStub) SettleNextAccountDay(context.Context, time.Time) (ContributionSettlementResult, bool, error) {
	if len(stub.accountResults) == 0 {
		return ContributionSettlementResult{}, false, nil
	}
	result := stub.accountResults[0]
	stub.accountResults = stub.accountResults[1:]
	return result, true, nil
}

func TestContributionAmountKeepsRousiMilliRatesExact(t *testing.T) {
	tests := []struct {
		units int64
		rate  int64
		want  string
	}{
		{units: 1, rate: 100, want: "0.1"},
		{units: 11, rate: 100, want: "1.1"},
		{units: 1, rate: 2000, want: "2"},
		{units: 365, rate: 1000, want: "365"},
	}
	for _, test := range tests {
		amount, err := contributionAmount(test.units, test.rate)
		if err != nil || amount.String() != test.want {
			t.Fatalf("contributionAmount(%d,%d)=%q error=%v, want %q", test.units, test.rate, amount.String(), err, test.want)
		}
	}
}

func TestContributionSettlementWorkerBalancesAllSources(t *testing.T) {
	userID := uuid.New()
	result := func(kind ContributionSettlementKind) ContributionSettlementResult {
		return ContributionSettlementResult{Kind: kind, UserID: userID, SourceReference: string(kind) + ":1", PolicyRevision: "rousi-contribution-v1", ExperienceAmount: "1"}
	}
	stub := &contributionSettlementRepositoryStub{
		uploadResults:  []ContributionSettlementResult{result(ContributionSettlementUpload), result(ContributionSettlementUpload)},
		publishResults: []ContributionSettlementResult{result(ContributionSettlementPublish)},
		accountResults: []ContributionSettlementResult{result(ContributionSettlementAccountDay)},
	}
	worker, err := NewContributionSettlementWorker(stub, ContributionSettlementWorkerConfig{
		BatchSize: 4, Now: func() time.Time { return time.Date(2026, 8, 20, 1, 0, 0, 0, time.UTC) },
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	processed, err := worker.RunOnce(context.Background())
	if err != nil || processed != 4 || len(stub.uploadResults) != 0 || len(stub.publishResults) != 0 || len(stub.accountResults) != 0 {
		t.Fatalf("RunOnce() processed=%d remaining=%d/%d/%d error=%v", processed, len(stub.uploadResults), len(stub.publishResults), len(stub.accountResults), err)
	}
}

func TestContributionSettlementWorkerReturnsRepositoryFailure(t *testing.T) {
	stub := &contributionSettlementRepositoryStub{uploadError: errors.New("database unavailable")}
	worker, err := NewContributionSettlementWorker(stub, ContributionSettlementWorkerConfig{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	processed, err := worker.RunOnce(context.Background())
	if processed != 0 || err == nil {
		t.Fatalf("RunOnce() processed=%d error=%v", processed, err)
	}
}
