package outputs

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/DougNorm/optimism/op-challenger/config"
	"github.com/DougNorm/optimism/op-challenger/game/fault/contracts"
	"github.com/DougNorm/optimism/op-challenger/game/fault/trace"
	"github.com/DougNorm/optimism/op-challenger/game/fault/trace/cannon"
	"github.com/DougNorm/optimism/op-challenger/game/fault/trace/split"
	"github.com/DougNorm/optimism/op-challenger/game/fault/types"
	"github.com/DougNorm/optimism/op-challenger/metrics"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

func NewOutputCannonTraceAccessor(
	ctx context.Context,
	logger log.Logger,
	m metrics.Metricer,
	cfg *config.Config,
	l2Client cannon.L2HeaderSource,
	contract cannon.L1HeadSource,
	dir string,
	gameDepth uint64,
	prestateBlock uint64,
	poststateBlock uint64,
) (*trace.Accessor, error) {
	// TODO(client-pod#43): Load depths from the contract
	topDepth := gameDepth / 2
	bottomDepth := gameDepth - topDepth
	outputProvider, err := NewTraceProvider(ctx, logger, cfg.RollupRpc, topDepth, prestateBlock, poststateBlock)
	if err != nil {
		return nil, err
	}

	cannonCreator := func(ctx context.Context, localContext common.Hash, agreed contracts.Proposal, claimed contracts.Proposal) (types.TraceProvider, error) {
		logger := logger.New("pre", agreed.OutputRoot, "post", claimed.OutputRoot, "localContext", localContext)
		subdir := filepath.Join(dir, localContext.Hex())
		localInputs, err := cannon.FetchLocalInputsFromProposals(ctx, contract, l2Client, agreed, claimed)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch cannon local inputs: %w", err)
		}
		provider := cannon.NewTraceProvider(logger, m, cfg, localContext, localInputs, subdir, bottomDepth)
		return provider, nil
	}

	cache := NewProviderCache(m, "output_cannon_provider", cannonCreator)
	selector := split.NewSplitProviderSelector(outputProvider, int(topDepth), OutputRootSplitAdapter(outputProvider, cache.GetOrCreate))
	return trace.NewAccessor(selector), nil
}
