package config

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/log"

	"github.com/DougNorm/optimism/op-chain-ops/genesis"
	"github.com/DougNorm/optimism/op-e2e/external"
	oplog "github.com/DougNorm/optimism/op-service/log"
)

var (
	// All of the following variables are set in the init function
	// and read from JSON files on disk that are generated by the
	// foundry deploy script. The are globally exported to be used
	// in end to end tests.

	// L1Allocs represents the L1 genesis block state.
	L1Allocs *state.Dump
	// L1Deployments maps contract names to accounts in the L1
	// genesis block state.
	L1Deployments *genesis.L1Deployments
	// DeployConfig represents the deploy config used by the system.
	DeployConfig *genesis.DeployConfig
	// ExternalL2Shim is the shim to use if external ethereum client testing is
	// enabled
	ExternalL2Shim string
	// ExternalL2TestParms is additional metadata for executing external L2
	// tests.
	ExternalL2TestParms external.TestParms
	// EthNodeVerbosity is the level of verbosity to output
	EthNodeVerbosity int
)

func init() {
	var l1AllocsPath, l1DeploymentsPath, deployConfigPath, externalL2 string

	cwd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	root, err := findMonorepoRoot(cwd)
	if err != nil {
		panic(err)
	}

	defaultL1AllocsPath := filepath.Join(root, ".devnet", "allocs-l1.json")
	defaultL1DeploymentsPath := filepath.Join(root, ".devnet", "addresses.json")
	defaultDeployConfigPath := filepath.Join(root, "packages", "contracts-bedrock", "deploy-config", "devnetL1.json")

	flag.StringVar(&l1AllocsPath, "l1-allocs", defaultL1AllocsPath, "")
	flag.StringVar(&l1DeploymentsPath, "l1-deployments", defaultL1DeploymentsPath, "")
	flag.StringVar(&deployConfigPath, "deploy-config", defaultDeployConfigPath, "")
	flag.StringVar(&externalL2, "externalL2", "", "Enable tests with external L2")
	flag.IntVar(&EthNodeVerbosity, "ethLogVerbosity", int(log.LvlInfo), "The level of verbosity to use for the eth node logs")
	testing.Init() // Register test flags before parsing
	flag.Parse()

	// Setup global logger
	lvl := log.Lvl(EthNodeVerbosity)
	if lvl < log.LvlCrit {
		log.Root().SetHandler(log.DiscardHandler())
	} else if lvl > log.LvlTrace { // clip to trace level
		lvl = log.LvlTrace
	}
	// We cannot attach a testlog logger,
	// because the global logger is shared between different independent parallel tests.
	// Tests that write to a testlogger of another finished test fail.
	h := oplog.NewLogHandler(os.Stdout, oplog.CLIConfig{
		Level:  lvl,
		Color:  false, // some CI logs do not handle colors well
		Format: oplog.FormatTerminal,
	})
	oplog.SetGlobalLogHandler(h)

	if err := allExist(l1AllocsPath, l1DeploymentsPath, deployConfigPath); err != nil {
		return
	}

	L1Allocs, err = genesis.NewStateDump(l1AllocsPath)
	if err != nil {
		panic(err)
	}
	L1Deployments, err = genesis.NewL1Deployments(l1DeploymentsPath)
	if err != nil {
		panic(err)
	}
	DeployConfig, err = genesis.NewDeployConfig(deployConfigPath)
	if err != nil {
		panic(err)
	}

	// Do not use clique in the in memory tests. Otherwise block building
	// would be much more complex.
	DeployConfig.L1UseClique = false
	// Set the L1 genesis block timestamp to now
	DeployConfig.L1GenesisBlockTimestamp = hexutil.Uint64(time.Now().Unix())
	DeployConfig.FundDevAccounts = true
	// Speed up the in memory tests
	DeployConfig.L1BlockTime = 2
	DeployConfig.L2BlockTime = 1

	if L1Deployments != nil {
		DeployConfig.SetDeployments(L1Deployments)
	}

	if externalL2 != "" {
		if err := initExternalL2(externalL2); err != nil {
			panic(fmt.Errorf("could not initialize external L2: %w", err))
		}
	}
}

func initExternalL2(externalL2 string) error {
	var err error
	ExternalL2Shim, err = filepath.Abs(filepath.Join(externalL2, "shim"))
	if err != nil {
		return fmt.Errorf("could not compute abs of externalL2Nodes shim: %w", err)
	}

	_, err = os.Stat(ExternalL2Shim)
	if err != nil {
		return fmt.Errorf("failed to stat externalL2Nodes path: %w", err)
	}

	file, err := os.Open(filepath.Join(externalL2, "test_parms.json"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("could not open external L2 test parms: %w", err)
	}
	defer file.Close()

	if err := json.NewDecoder(file).Decode(&ExternalL2TestParms); err != nil {
		return fmt.Errorf("could not decode external L2 test parms: %w", err)
	}

	return nil
}

func allExist(filenames ...string) error {
	for _, filename := range filenames {
		if _, err := os.Stat(filename); err != nil {
			fmt.Printf("file %s does not exist, skipping genesis generation\n", filename)
			return err
		}
	}
	return nil
}

// findMonorepoRoot will recursively search upwards for a go.mod file.
// This depends on the structure of the monorepo having a go.mod file at the root.
func findMonorepoRoot(startDir string) (string, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", err
	}
	for {
		modulePath := filepath.Join(dir, "go.mod")
		if _, err := os.Stat(modulePath); err == nil {
			return dir, nil
		}
		parentDir := filepath.Dir(dir)
		// Check if we reached the filesystem root
		if parentDir == dir {
			break
		}
		dir = parentDir
	}
	return "", fmt.Errorf("monorepo root not found")
}
