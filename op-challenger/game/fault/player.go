package fault

import (
	"bytes"
	"context"
	"fmt"

	"github.com/DougNorm/optimism/op-challenger/game/fault/responder"
	"github.com/DougNorm/optimism/op-challenger/game/fault/types"
	gameTypes "github.com/DougNorm/optimism/op-challenger/game/types"
	"github.com/DougNorm/optimism/op-challenger/metrics"
	"github.com/DougNorm/optimism/op-service/txmgr"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

type actor func(ctx context.Context) error

type GameInfo interface {
	GetStatus(context.Context) (gameTypes.GameStatus, error)
	GetClaimCount(context.Context) (uint64, error)
}

type GamePlayer struct {
	act    actor
	loader GameInfo
	logger log.Logger
	status gameTypes.GameStatus
}

type GameContract interface {
	responder.GameContract
	GameInfo
	ClaimLoader
	GetStatus(ctx context.Context) (gameTypes.GameStatus, error)
	GetMaxGameDepth(ctx context.Context) (uint64, error)
}

type resourceCreator func(ctx context.Context, logger log.Logger, gameDepth uint64, dir string) (types.TraceAccessor, error)

func NewGamePlayer(
	ctx context.Context,
	logger log.Logger,
	m metrics.Metricer,
	dir string,
	addr common.Address,
	txMgr txmgr.TxManager,
	loader GameContract,
	creator resourceCreator,
) (*GamePlayer, error) {
	logger = logger.New("game", addr)

	status, err := loader.GetStatus(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch game status: %w", err)
	}
	if status != gameTypes.GameStatusInProgress {
		logger.Info("Game already resolved", "status", status)
		// Game is already complete so skip creating the trace provider, loading game inputs etc.
		return &GamePlayer{
			logger: logger,
			loader: loader,
			status: status,
			// Act function does nothing because the game is already complete
			act: func(ctx context.Context) error {
				return nil
			},
		}, nil
	}

	gameDepth, err := loader.GetMaxGameDepth(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch the game depth: %w", err)
	}

	accessor, err := creator(ctx, logger, gameDepth, dir)
	if err != nil {
		return nil, fmt.Errorf("failed to create trace accessor: %w", err)
	}

	responder, err := responder.NewFaultResponder(logger, txMgr, loader)
	if err != nil {
		return nil, fmt.Errorf("failed to create the responder: %w", err)
	}

	agent := NewAgent(m, loader, int(gameDepth), accessor, responder, logger)
	return &GamePlayer{
		act:    agent.Act,
		loader: loader,
		logger: logger,
		status: status,
	}, nil
}

func (g *GamePlayer) Status() gameTypes.GameStatus {
	return g.status
}

func (g *GamePlayer) ProgressGame(ctx context.Context) gameTypes.GameStatus {
	if g.status != gameTypes.GameStatusInProgress {
		// Game is already complete so don't try to perform further actions.
		g.logger.Trace("Skipping completed game")
		return g.status
	}
	g.logger.Trace("Checking if actions are required")
	if err := g.act(ctx); err != nil {
		g.logger.Error("Error when acting on game", "err", err)
	}
	status, err := g.loader.GetStatus(ctx)
	if err != nil {
		g.logger.Warn("Unable to retrieve game status", "err", err)
		return gameTypes.GameStatusInProgress
	}
	g.logGameStatus(ctx, status)
	g.status = status
	return status
}

func (g *GamePlayer) logGameStatus(ctx context.Context, status gameTypes.GameStatus) {
	if status == gameTypes.GameStatusInProgress {
		claimCount, err := g.loader.GetClaimCount(ctx)
		if err != nil {
			g.logger.Error("Failed to get claim count for in progress game", "err", err)
			return
		}
		g.logger.Info("Game info", "claims", claimCount, "status", status)
		return
	}
	g.logger.Info("Game resolved", "status", status)
}

type PrestateLoader interface {
	GetAbsolutePrestateHash(ctx context.Context) (common.Hash, error)
}

// ValidateAbsolutePrestate validates the absolute prestate of the fault game.
func ValidateAbsolutePrestate(ctx context.Context, trace types.TraceProvider, loader PrestateLoader) error {
	providerPrestateHash, err := trace.AbsolutePreStateCommitment(ctx)
	if err != nil {
		return fmt.Errorf("failed to get the trace provider's absolute prestate: %w", err)
	}
	onchainPrestate, err := loader.GetAbsolutePrestateHash(ctx)
	if err != nil {
		return fmt.Errorf("failed to get the onchain absolute prestate: %w", err)
	}
	if !bytes.Equal(providerPrestateHash[:], onchainPrestate[:]) {
		return fmt.Errorf("trace provider's absolute prestate does not match onchain absolute prestate: Provider: %s | Chain %s", providerPrestateHash.Hex(), onchainPrestate.Hex())
	}
	return nil
}
