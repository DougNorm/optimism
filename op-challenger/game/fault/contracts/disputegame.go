package contracts

import (
	"context"
	"fmt"
	"math/big"

	"github.com/DougNorm/optimism/op-challenger/game/fault/types"
	gameTypes "github.com/DougNorm/optimism/op-challenger/game/types"
	"github.com/DougNorm/optimism/op-service/sources/batching"
	"github.com/DougNorm/optimism/op-service/txmgr"
	"github.com/ethereum/go-ethereum/common"
)

const (
	methodGameDuration     = "GAME_DURATION"
	methodMaxGameDepth     = "MAX_GAME_DEPTH"
	methodAbsolutePrestate = "ABSOLUTE_PRESTATE"
	methodStatus           = "status"
	methodClaimCount       = "claimDataLen"
	methodClaim            = "claimData"
	methodL1Head           = "l1Head"
	methodResolve          = "resolve"
	methodResolveClaim     = "resolveClaim"
	methodAttack           = "attack"
	methodDefend           = "defend"
	methodStep             = "step"
	methodAddLocalData     = "addLocalData"
	methodVM               = "VM"
)

type disputeGameContract struct {
	multiCaller *batching.MultiCaller
	contract    *batching.BoundContract
}

// contractProposal matches the structure for output root proposals used by the contracts.
// It must exactly match the contract structure. The exposed API uses Proposal to decouple the contract
// and challenger representations of the proposal data.
type contractProposal struct {
	Index         *big.Int
	L2BlockNumber *big.Int
	OutputRoot    common.Hash
}

type Proposal struct {
	L2BlockNumber *big.Int
	OutputRoot    common.Hash
}

func asProposal(p contractProposal) Proposal {
	return Proposal{
		L2BlockNumber: p.L2BlockNumber,
		OutputRoot:    p.OutputRoot,
	}
}

func (f *disputeGameContract) GetGameDuration(ctx context.Context) (uint64, error) {
	result, err := f.multiCaller.SingleCall(ctx, batching.BlockLatest, f.contract.Call(methodGameDuration))
	if err != nil {
		return 0, fmt.Errorf("failed to fetch game duration: %w", err)
	}
	return result.GetUint64(0), nil
}

func (f *disputeGameContract) GetMaxGameDepth(ctx context.Context) (uint64, error) {
	result, err := f.multiCaller.SingleCall(ctx, batching.BlockLatest, f.contract.Call(methodMaxGameDepth))
	if err != nil {
		return 0, fmt.Errorf("failed to fetch max game depth: %w", err)
	}
	return result.GetBigInt(0).Uint64(), nil
}

func (f *disputeGameContract) GetAbsolutePrestateHash(ctx context.Context) (common.Hash, error) {
	result, err := f.multiCaller.SingleCall(ctx, batching.BlockLatest, f.contract.Call(methodAbsolutePrestate))
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to fetch absolute prestate hash: %w", err)
	}
	return result.GetHash(0), nil
}

func (f *disputeGameContract) GetL1Head(ctx context.Context) (common.Hash, error) {
	result, err := f.multiCaller.SingleCall(ctx, batching.BlockLatest, f.contract.Call(methodL1Head))
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to fetch L1 head: %w", err)
	}
	return result.GetHash(0), nil
}

func (f *disputeGameContract) GetStatus(ctx context.Context) (gameTypes.GameStatus, error) {
	result, err := f.multiCaller.SingleCall(ctx, batching.BlockLatest, f.contract.Call(methodStatus))
	if err != nil {
		return 0, fmt.Errorf("failed to fetch status: %w", err)
	}
	return gameTypes.GameStatusFromUint8(result.GetUint8(0))
}

func (f *disputeGameContract) GetClaimCount(ctx context.Context) (uint64, error) {
	result, err := f.multiCaller.SingleCall(ctx, batching.BlockLatest, f.contract.Call(methodClaimCount))
	if err != nil {
		return 0, fmt.Errorf("failed to fetch claim count: %w", err)
	}
	return result.GetBigInt(0).Uint64(), nil
}

func (f *disputeGameContract) GetClaim(ctx context.Context, idx uint64) (types.Claim, error) {
	result, err := f.multiCaller.SingleCall(ctx, batching.BlockLatest, f.contract.Call(methodClaim, new(big.Int).SetUint64(idx)))
	if err != nil {
		return types.Claim{}, fmt.Errorf("failed to fetch claim %v: %w", idx, err)
	}
	return f.decodeClaim(result, int(idx)), nil
}

func (f *disputeGameContract) GetAllClaims(ctx context.Context) ([]types.Claim, error) {
	count, err := f.GetClaimCount(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load claim count: %w", err)
	}

	calls := make([]*batching.ContractCall, count)
	for i := uint64(0); i < count; i++ {
		calls[i] = f.contract.Call(methodClaim, new(big.Int).SetUint64(i))
	}

	results, err := f.multiCaller.Call(ctx, batching.BlockLatest, calls...)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch claim data: %w", err)
	}

	var claims []types.Claim
	for idx, result := range results {
		claims = append(claims, f.decodeClaim(result, idx))
	}
	return claims, nil
}

func (f *disputeGameContract) vm(ctx context.Context) (*VMContract, error) {
	result, err := f.multiCaller.SingleCall(ctx, batching.BlockLatest, f.contract.Call(methodVM))
	if err != nil {
		return nil, fmt.Errorf("failed to fetch VM addr: %w", err)
	}
	vmAddr := result.GetAddress(0)
	return NewVMContract(vmAddr, f.multiCaller)
}

func (f *disputeGameContract) AttackTx(parentContractIndex uint64, pivot common.Hash) (txmgr.TxCandidate, error) {
	call := f.contract.Call(methodAttack, new(big.Int).SetUint64(parentContractIndex), pivot)
	return call.ToTxCandidate()
}

func (f *disputeGameContract) DefendTx(parentContractIndex uint64, pivot common.Hash) (txmgr.TxCandidate, error) {
	call := f.contract.Call(methodDefend, new(big.Int).SetUint64(parentContractIndex), pivot)
	return call.ToTxCandidate()
}

func (f *disputeGameContract) StepTx(claimIdx uint64, isAttack bool, stateData []byte, proof []byte) (txmgr.TxCandidate, error) {
	call := f.contract.Call(methodStep, new(big.Int).SetUint64(claimIdx), isAttack, stateData, proof)
	return call.ToTxCandidate()
}

func (f *disputeGameContract) CallResolveClaim(ctx context.Context, claimIdx uint64) error {
	call := f.resolveClaimCall(claimIdx)
	_, err := f.multiCaller.SingleCall(ctx, batching.BlockLatest, call)
	if err != nil {
		return fmt.Errorf("failed to call resolve claim: %w", err)
	}
	return nil
}

func (f *disputeGameContract) ResolveClaimTx(claimIdx uint64) (txmgr.TxCandidate, error) {
	call := f.resolveClaimCall(claimIdx)
	return call.ToTxCandidate()
}

func (f *disputeGameContract) resolveClaimCall(claimIdx uint64) *batching.ContractCall {
	return f.contract.Call(methodResolveClaim, new(big.Int).SetUint64(claimIdx))
}

func (f *disputeGameContract) CallResolve(ctx context.Context) (gameTypes.GameStatus, error) {
	call := f.resolveCall()
	result, err := f.multiCaller.SingleCall(ctx, batching.BlockLatest, call)
	if err != nil {
		return gameTypes.GameStatusInProgress, fmt.Errorf("failed to call resolve: %w", err)
	}
	return gameTypes.GameStatusFromUint8(result.GetUint8(0))
}

func (f *disputeGameContract) ResolveTx() (txmgr.TxCandidate, error) {
	call := f.resolveCall()
	return call.ToTxCandidate()
}

func (f *disputeGameContract) resolveCall() *batching.ContractCall {
	return f.contract.Call(methodResolve)
}

func (f *disputeGameContract) decodeClaim(result *batching.CallResult, contractIndex int) types.Claim {
	parentIndex := result.GetUint32(0)
	countered := result.GetBool(1)
	claim := result.GetHash(2)
	position := result.GetBigInt(3)
	clock := result.GetBigInt(4)
	return types.Claim{
		ClaimData: types.ClaimData{
			Value:    claim,
			Position: types.NewPositionFromGIndex(position),
		},
		Countered:           countered,
		Clock:               clock.Uint64(),
		ContractIndex:       contractIndex,
		ParentContractIndex: int(parentIndex),
	}
}
