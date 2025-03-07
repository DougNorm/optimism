package main

import (
	"os"

	opservice "github.com/DougNorm/optimism/op-service"
	"github.com/urfave/cli/v2"

	"github.com/DougNorm/optimism/op-proposer/flags"
	"github.com/DougNorm/optimism/op-proposer/metrics"
	"github.com/DougNorm/optimism/op-proposer/proposer"
	"github.com/DougNorm/optimism/op-service/cliapp"
	oplog "github.com/DougNorm/optimism/op-service/log"
	"github.com/DougNorm/optimism/op-service/metrics/doc"
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
	app.Name = "op-proposer"
	app.Usage = "L2Output Submitter"
	app.Description = "Service for generating and submitting L2 Output checkpoints to the L2OutputOracle contract"
	app.Action = cliapp.LifecycleCmd(proposer.Main(Version))
	app.Commands = []*cli.Command{
		{
			Name:        "doc",
			Subcommands: doc.NewSubcommands(metrics.NewMetrics("default")),
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		log.Crit("Application failed", "message", err)
	}
}
