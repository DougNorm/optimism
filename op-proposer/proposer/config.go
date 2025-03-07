package proposer

import (
	"time"

	"github.com/urfave/cli/v2"

	"github.com/DougNorm/optimism/op-proposer/flags"
	oplog "github.com/DougNorm/optimism/op-service/log"
	opmetrics "github.com/DougNorm/optimism/op-service/metrics"
	oppprof "github.com/DougNorm/optimism/op-service/pprof"
	oprpc "github.com/DougNorm/optimism/op-service/rpc"
	"github.com/DougNorm/optimism/op-service/txmgr"
)

// CLIConfig is a well typed config that is parsed from the CLI params.
// This also contains config options for auxiliary services.
// It is transformed into a `Config` before the L2 output submitter is started.
type CLIConfig struct {
	/* Required Params */

	// L1EthRpc is the HTTP provider URL for L1.
	L1EthRpc string

	// RollupRpc is the HTTP provider URL for the rollup node.
	RollupRpc string

	// L2OOAddress is the L2OutputOracle contract address.
	L2OOAddress string

	// PollInterval is the delay between querying L2 for more transaction
	// and creating a new batch.
	PollInterval time.Duration

	// AllowNonFinalized can be set to true to propose outputs
	// for L2 blocks derived from non-finalized L1 data.
	AllowNonFinalized bool

	TxMgrConfig txmgr.CLIConfig

	RPCConfig oprpc.CLIConfig

	LogConfig oplog.CLIConfig

	MetricsConfig opmetrics.CLIConfig

	PprofConfig oppprof.CLIConfig
}

func (c *CLIConfig) Check() error {
	if err := c.RPCConfig.Check(); err != nil {
		return err
	}
	if err := c.MetricsConfig.Check(); err != nil {
		return err
	}
	if err := c.PprofConfig.Check(); err != nil {
		return err
	}
	if err := c.TxMgrConfig.Check(); err != nil {
		return err
	}
	return nil
}

// NewConfig parses the Config from the provided flags or environment variables.
func NewConfig(ctx *cli.Context) *CLIConfig {
	return &CLIConfig{
		// Required Flags
		L1EthRpc:     ctx.String(flags.L1EthRpcFlag.Name),
		RollupRpc:    ctx.String(flags.RollupRpcFlag.Name),
		L2OOAddress:  ctx.String(flags.L2OOAddressFlag.Name),
		PollInterval: ctx.Duration(flags.PollIntervalFlag.Name),
		TxMgrConfig:  txmgr.ReadCLIConfig(ctx),
		// Optional Flags
		AllowNonFinalized: ctx.Bool(flags.AllowNonFinalizedFlag.Name),
		RPCConfig:         oprpc.ReadCLIConfig(ctx),
		LogConfig:         oplog.ReadCLIConfig(ctx),
		MetricsConfig:     opmetrics.ReadCLIConfig(ctx),
		PprofConfig:       oppprof.ReadCLIConfig(ctx),
	}
}
