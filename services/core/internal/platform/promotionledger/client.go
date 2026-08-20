// Package promotionledger preserves the existing composition name while the
// HTTP mechanics are shared with every immutable Settlement control command.
package promotionledger

import (
	"time"

	"github.com/peergo/peergo/services/core/internal/platform/settlementcontrol"
)

type Client struct{ *settlementcontrol.Client }

func NewClient(baseURL, serviceToken string, timeout time.Duration) (*Client, error) {
	client, err := settlementcontrol.NewClient(baseURL, "/internal/v1/settlement/promotion-rules", serviceToken, timeout)
	if err != nil {
		return nil, err
	}
	return &Client{Client: client}, nil
}

var _ settlementcontrol.Sink = (*Client)(nil)
