package main

import (
	"context"
	"os"

	"github.com/urfave/cli/v2"

	"github.com/DougNorm/optimism/op-batcher/batcher"
	"github.com/DougNorm/optimism/op-batcher/flags"
	"github.com/DougNorm/optimism/op-batcher/metrics"
	opservice "github.com/DougNorm/optimism/op-service"
	"github.com/DougNorm/optimism/op-service/cliapp"
	oplog "github.com/DougNorm/optimism/op-service/log"
	"github.com/DougNorm/optimism/op-service/metrics/doc"
	"github.com/DougNorm/optimism/op-service/opio"
	"github.com/ethereum/go-ethereum/log"
)

var (
	Version   = "v0.10.14"
	GitCommit = ""
	GitDate   = ""
)

func main() {
	oplog.SetupDefaults()

	app := cli.NewApp()
	app.Flags = cliapp.ProtectFlags(flags.Flags)
	app.Version = opservice.FormatVersion(Version, GitCommit, GitDate, "")
	app.Name = "op-batcher"
	app.Usage = "Batch Submitter Service"
	app.Description = "Service for generating and submitting L2 tx batches to L1"
	app.Action = cliapp.LifecycleCmd(batcher.Main(Version))
	app.Commands = []*cli.Command{
		{
			Name:        "doc",
			Subcommands: doc.NewSubcommands(metrics.NewMetrics("default")),
		},
	}

	ctx := opio.WithInterruptBlocker(context.Background())
	err := app.RunContext(ctx, os.Args)
	if err != nil {
		log.Crit("Application failed", "message", err)
	}
}
