package rpc

import (
	"errors"
	"math"

	opservice "github.com/DougNorm/optimism/op-service"
	"github.com/urfave/cli/v2"
)

const (
	ListenAddrFlagName  = "rpc.addr"
	PortFlagName        = "rpc.port"
	EnableAdminFlagName = "rpc.enable-admin"
)

func CLIFlags(envPrefix string) []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:    ListenAddrFlagName,
			Usage:   "rpc listening address",
			Value:   "0.0.0.0", // TODO(CLI-4159): Switch to 127.0.0.1
			EnvVars: opservice.PrefixEnvVar(envPrefix, "RPC_ADDR"),
		},
		&cli.IntFlag{
			Name:    PortFlagName,
			Usage:   "rpc listening port",
			Value:   8545,
			EnvVars: opservice.PrefixEnvVar(envPrefix, "RPC_PORT"),
		},
		&cli.BoolFlag{
			Name:    EnableAdminFlagName,
			Usage:   "Enable the admin API",
			EnvVars: opservice.PrefixEnvVar(envPrefix, "RPC_ENABLE_ADMIN"),
		},
	}
}

type CLIConfig struct {
	ListenAddr  string
	ListenPort  int
	EnableAdmin bool
}

func (c CLIConfig) Check() error {
	if c.ListenPort < 0 || c.ListenPort > math.MaxUint16 {
		return errors.New("invalid RPC port")
	}

	return nil
}

func ReadCLIConfig(ctx *cli.Context) CLIConfig {
	return CLIConfig{
		ListenAddr:  ctx.String(ListenAddrFlagName),
		ListenPort:  ctx.Int(PortFlagName),
		EnableAdmin: ctx.Bool(EnableAdminFlagName),
	}
}
