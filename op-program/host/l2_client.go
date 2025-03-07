package host

import (
	"context"
	"fmt"

	"github.com/DougNorm/optimism/op-service/client"
	"github.com/DougNorm/optimism/op-service/eth"
	"github.com/DougNorm/optimism/op-service/sources"
	"github.com/DougNorm/optimism/op-service/sources/caching"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

type L2Client struct {
	*sources.L2Client

	// l2Head is the L2 block hash that we use to fetch L2 output
	l2Head common.Hash
}

type L2ClientConfig struct {
	*sources.L2ClientConfig
	L2Head common.Hash
}

func NewL2Client(client client.RPC, log log.Logger, metrics caching.Metrics, config *L2ClientConfig) (*L2Client, error) {
	l2Client, err := sources.NewL2Client(client, log, metrics, config.L2ClientConfig)
	if err != nil {
		return nil, err
	}
	return &L2Client{
		L2Client: l2Client,
		l2Head:   config.L2Head,
	}, nil
}

func (s *L2Client) OutputByRoot(ctx context.Context, l2OutputRoot common.Hash) (eth.Output, error) {
	output, err := s.OutputV0AtBlock(ctx, s.l2Head)
	if err != nil {
		return nil, err
	}
	if eth.OutputRoot(output) != eth.Bytes32(l2OutputRoot) {
		// For fault proofs, we only reference outputs at the l2 head at boot time
		// The caller shouldn't be requesting outputs at any other block
		return nil, fmt.Errorf("unknown output root")
	}
	return output, nil
}
