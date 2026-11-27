package trackeroperations

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/peergo/peergo/contracts/go/trackeroperationsv1"
)

func (client *Client) SeedingTotals(ctx context.Context, userIDs []string) (trackeroperationsv1.UserSeedingTotals, error) {
	if len(userIDs) == 0 || len(userIDs) > trackeroperationsv1.MaxSeedingTotalsUsers {
		return nil, errors.New("Tracker seeding totals query is invalid")
	}
	query := url.Values{"user_id": userIDs}
	endpoint := client.origin + trackeroperationsv1.SeedingTotalsPath + "?" + query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+client.serviceToken)
	request.Header.Set("Accept", "application/json")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("read Tracker seeding totals: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, responseLimit))
		return nil, fmt.Errorf("Tracker seeding totals returned status %d", response.StatusCode)
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, responseLimit+1))
	if err != nil || len(payload) > responseLimit {
		return nil, errors.New("Tracker returned an invalid seeding totals response")
	}
	var totals trackeroperationsv1.UserSeedingTotals
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&totals); err != nil || !totals.Valid(userIDs) {
		return nil, errors.New("Tracker returned an invalid seeding totals response")
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, errors.New("Tracker seeding totals response has trailing data")
	}
	return totals, nil
}
