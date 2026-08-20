package settlementtrafficv1

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	t.Parallel()
	event := validEvent()
	encoded, err := Encode(event)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(encoded)
	if err != nil || !reflect.DeepEqual(decoded, event) {
		t.Fatalf("Decode() = %+v, %v", decoded, err)
	}
}

func TestEncodeDecodeAcceptsRetainedEventWithoutExplanation(t *testing.T) {
	t.Parallel()
	event := validEvent()
	event.Explanation = nil
	encoded, err := Encode(event)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(encoded)
	if err != nil || decoded.Explanation != nil {
		t.Fatalf("Decode(legacy event) = %+v, %v", decoded, err)
	}
}

func TestDecodeRejectsNonCanonicalAndUnknownData(t *testing.T) {
	t.Parallel()
	event := validEvent()
	encoded, err := Encode(event)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(append([]byte(" "), encoded...)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Decode(noncanonical) error = %v", err)
	}
	if _, err := Decode(append(encoded[:len(encoded)-1], []byte(`,"extra":true}`)...)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Decode(unknown) error = %v", err)
	}
}

func TestValidateRejectsNonUTCInterval(t *testing.T) {
	t.Parallel()
	event := validEvent()
	event.IntervalEndsAt = event.IntervalEndsAt.In(time.FixedZone("offset", 8*60*60))
	if err := Validate(event); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateRejectsExplanationThatDoesNotReconcile(t *testing.T) {
	t.Parallel()
	event := validEvent()
	event.Explanation.Segments[1].CreditedUploaded++
	if err := Validate(event); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateAcceptsExplicitlyOmittedLargeExplanation(t *testing.T) {
	t.Parallel()
	event := validEvent()
	event.Explanation = &Explanation{
		Status: ExplanationOmitted, SegmentCount: MaxExplanationSegments + 1,
	}
	if err := Validate(event); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestMaximumPublicExplanationFitsTheOutboxLimit(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, time.August, 9, 10, 0, 0, 123456789, time.UTC)
	const perSegment int64 = 1_234_567_890_123_456
	event := validEvent()
	event.IntervalStartsAt = start
	event.IntervalEndsAt = start.Add(MaxExplanationSegments * time.Second)
	event.OccurredAt = event.IntervalEndsAt.Add(time.Second)
	event.RawUploaded = perSegment * MaxExplanationSegments
	event.RawDownloaded = perSegment * MaxExplanationSegments
	event.CreditedUploaded = perSegment * MaxExplanationSegments
	event.ChargedDownloaded = perSegment * MaxExplanationSegments
	event.Explanation = &Explanation{
		Status: ExplanationComplete, SegmentCount: MaxExplanationSegments,
		Segments: make([]Segment, MaxExplanationSegments),
	}
	for index := range event.Explanation.Segments {
		event.Explanation.Segments[index] = Segment{
			StartsAt:    start.Add(time.Duration(index) * time.Second),
			EndsAt:      start.Add(time.Duration(index+1) * time.Second),
			RawUploaded: perSegment, RawDownloaded: perSegment,
			CreditedUploaded: perSegment, ChargedDownloaded: perSegment,
		}
	}
	encoded, err := Encode(event)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > MaxEventBytes {
		t.Fatalf("encoded explanation is %d bytes, limit %d", len(encoded), MaxEventBytes)
	}
}

func validEvent() Event {
	start := time.Date(2026, time.August, 9, 10, 0, 0, 0, time.UTC)
	return Event{
		SchemaVersion:     SchemaVersion,
		EventID:           "0198f20a-6da8-7e51-9c64-111111111111",
		OccurredAt:        start.Add(2 * time.Minute),
		UserID:            "0198f20a-6da8-4e51-9c64-111111111111",
		TorrentID:         7,
		IntervalStartsAt:  start,
		IntervalEndsAt:    start.Add(time.Minute),
		RawUploaded:       100,
		RawDownloaded:     80,
		CreditedUploaded:  200,
		ChargedDownloaded: 0,
		SettlementSHA256:  "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Explanation: &Explanation{
			Status: ExplanationComplete, SegmentCount: 2,
			Segments: []Segment{
				{
					StartsAt: start, EndsAt: start.Add(30 * time.Second),
					RawUploaded: 40, RawDownloaded: 30, CreditedUploaded: 80, ChargedDownloaded: 0,
				},
				{
					StartsAt: start.Add(30 * time.Second), EndsAt: start.Add(time.Minute),
					RawUploaded: 60, RawDownloaded: 50, CreditedUploaded: 120, ChargedDownloaded: 0,
				},
			},
		},
	}
}
