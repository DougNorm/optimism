package proposer

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v2"

	"github.com/DougNorm/optimism/op-proposer/flags"
	opservice "github.com/DougNorm/optimism/op-service"
	"github.com/DougNorm/optimism/op-service/cliapp"
	oplog "github.com/DougNorm/optimism/op-service/log"
)

// Main is the entrypoint into the L2OutputSubmitter.
// This method returns a cliapp.LifecycleAction, to create an op-service CLI-lifecycle-managed L2Output-submitter
func Main(version string) cliapp.LifecycleAction {
	return func(cliCtx *cli.Context, closeApp context.CancelCauseFunc) (cliapp.Lifecycle, error) {
		if err := flags.CheckRequired(cliCtx); err != nil {
			return nil, err
		}
		cfg := NewConfig(cliCtx)
		if err := cfg.Check(); err != nil {
			return nil, fmt.Errorf("invalid CLI flags: %w", err)
		}

		l := oplog.NewLogger(oplog.AppOut(cliCtx), cfg.LogConfig)
		oplog.SetGlobalLogHandler(l.GetHandler())
		opservice.ValidateEnvVars(flags.EnvVarPrefix, flags.Flags, l)

		l.Info("Initializing L2Output Submitter")
		return ProposerServiceFromCLIConfig(cliCtx.Context, version, cfg, l)
	}
}
