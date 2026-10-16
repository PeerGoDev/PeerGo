package hnr

import (
	"encoding/json"
	"math/rand"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestIncrementalMatchesReplayAcrossRestartsLateClientsAndDuplicates(t *testing.T) {
	for seed := int64(0); seed < 50; seed++ {
		random := rand.New(rand.NewSource(seed))
		at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
		o := Obligation{ID: uuid.New(), CompletedAt: at, InitialUploaded: 5, RawDownloaded: 10000,
			RequiredSeedSeconds: 3600, RequiredRatioBasisPoints: 10000, MaxIntervalCreditSeconds: 2100, State: StateTracking}
		var all []RawInterval
		var state *IncrementalState
		for batch := 0; batch < 20; batch++ {
			var delta []SequencedInterval
			for j := 0; j < 10; j++ {
				start := at.Add(time.Duration(random.Intn(7200)-60)*time.Second + time.Duration(random.Intn(1000))*time.Microsecond)
				interval := RawInterval{EventID: uuid.New(), StartsAt: start, EndsAt: start.Add(time.Duration(1+random.Intn(4000)) * time.Second),
					RawUploaded: int64(random.Intn(500))}
				if random.Intn(5) == 0 {
					interval.PreviousLeft = 100
				}
				all = append(all, interval)
				delta = append(delta, SequencedInterval{RawInterval: interval, Stream: "ANNOUNCE", Sequence: int64(batch*10 + j + 1)})
			}
			next, got, err := AdvanceIncremental(o, state, delta)
			if err != nil {
				t.Fatal(seed, err)
			}
			want, err := Evaluate(o, all)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("seed=%d batch=%d incremental=%+v replay=%+v", seed, batch, got, want)
			}
			// Duplicate work must not add time or bytes, including after JSON persistence.
			saved, _ := json.Marshal(next)
			var restored IncrementalState
			if err = json.Unmarshal(saved, &restored); err != nil {
				t.Fatal(err)
			}
			replayed, again, err := AdvanceIncremental(o, &restored, delta)
			if err != nil || !reflect.DeepEqual(again, want) {
				t.Fatalf("duplicate changed result: %+v %v", again, err)
			}
			state = &replayed
		}
	}
}

func TestIncrementalFailedInputLeavesCheckpointUnchanged(t *testing.T) {
	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	o := Obligation{ID: uuid.New(), CompletedAt: at, RequiredSeedSeconds: 100, MaxIntervalCreditSeconds: 2100, State: StateTracking}
	first := SequencedInterval{RawInterval: RawInterval{EventID: uuid.New(), StartsAt: at, EndsAt: at.Add(time.Second)}, Stream: "A", Sequence: 1}
	state, _, err := AdvanceIncremental(o, nil, []SequencedInterval{first})
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(state)
	bad := first
	bad.Sequence = 2
	bad.RawUploaded = -1
	if _, _, err = AdvanceIncremental(o, &state, []SequencedInterval{bad}); err == nil {
		t.Fatal("invalid input accepted")
	}
	after, _ := json.Marshal(state)
	if string(before) != string(after) {
		t.Fatal("failed update mutated committed checkpoint")
	}
}
