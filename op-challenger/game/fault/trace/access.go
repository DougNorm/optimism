package trace

import (
	"context"

	"github.com/DougNorm/optimism/op-challenger/game/fault/types"
	"github.com/ethereum/go-ethereum/common"
)

func NewSimpleTraceAccessor(trace types.TraceProvider) *Accessor {
	selector := func(_ context.Context, _ types.Game, _ types.Claim, _ types.Position) (types.TraceProvider, error) {
		return trace, nil
	}
	return NewAccessor(selector)
}

type ProviderSelector func(ctx context.Context, game types.Game, ref types.Claim, pos types.Position) (types.TraceProvider, error)

func NewAccessor(selector ProviderSelector) *Accessor {
	return &Accessor{selector}
}

type Accessor struct {
	selector ProviderSelector
}

func (t *Accessor) Get(ctx context.Context, game types.Game, ref types.Claim, pos types.Position) (common.Hash, error) {
	provider, err := t.selector(ctx, game, ref, pos)
	if err != nil {
		return common.Hash{}, err
	}
	return provider.Get(ctx, pos)
}

func (t *Accessor) GetStepData(ctx context.Context, game types.Game, ref types.Claim, pos types.Position) (prestate []byte, proofData []byte, preimageData *types.PreimageOracleData, err error) {
	provider, err := t.selector(ctx, game, ref, pos)
	if err != nil {
		return nil, nil, nil, err
	}
	return provider.GetStepData(ctx, pos)
}

var _ types.TraceAccessor = (*Accessor)(nil)
