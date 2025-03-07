package genesis

import (
	"fmt"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"

	"github.com/DougNorm/optimism/op-bindings/predeploys"
	"github.com/DougNorm/optimism/op-chain-ops/immutables"
	"github.com/DougNorm/optimism/op-chain-ops/state"
	"github.com/DougNorm/optimism/op-service/eth"
)

// BuildL2Genesis will build the L2 genesis block.
func BuildL2Genesis(config *DeployConfig, l1StartBlock *types.Block) (*core.Genesis, error) {
	genspec, err := NewL2Genesis(config, l1StartBlock)
	if err != nil {
		return nil, err
	}

	db := state.NewMemoryStateDB(genspec)
	if config.FundDevAccounts {
		log.Info("Funding developer accounts in L2 genesis")
		FundDevAccounts(db)
	}

	SetPrecompileBalances(db)

	storage, err := NewL2StorageConfig(config, l1StartBlock)
	if err != nil {
		return nil, err
	}

	immutableConfig, err := NewL2ImmutableConfig(config, l1StartBlock)
	if err != nil {
		return nil, err
	}

	// Set up the proxies
	err = setProxies(db, predeploys.ProxyAdminAddr, BigL2PredeployNamespace, 2048)
	if err != nil {
		return nil, err
	}

	// Set up the implementations that contain immutables
	deployResults, err := immutables.Deploy(immutableConfig)
	if err != nil {
		return nil, err
	}
	for name, predeploy := range predeploys.Predeploys {
		if predeploy.Enabled != nil && !predeploy.Enabled(config) {
			log.Warn("Skipping disabled predeploy.", "name", name, "address", predeploy.Address)
			continue
		}
		codeAddr := predeploy.Address
		if !predeploy.ProxyDisabled {
			codeAddr, err = AddressToCodeNamespace(predeploy.Address)
			if err != nil {
				return nil, fmt.Errorf("error converting to code namespace: %w", err)
			}
			db.CreateAccount(codeAddr)
			db.SetState(predeploy.Address, ImplementationSlot, eth.AddressAsLeftPaddedHash(codeAddr))
			log.Info("Set proxy", "name", name, "address", predeploy.Address, "implementation", codeAddr)
		} else if db.Exist(predeploy.Address) {
			db.DeleteState(predeploy.Address, AdminSlot)
		}
		if err := setupPredeploy(db, deployResults, storage, name, predeploy.Address, codeAddr); err != nil {
			return nil, err
		}
		code := db.GetCode(codeAddr)
		if len(code) == 0 {
			return nil, fmt.Errorf("code not set for %s", name)
		}
	}

	return db.Genesis(), nil
}
