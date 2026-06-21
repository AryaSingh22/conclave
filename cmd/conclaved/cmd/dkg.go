package cmd

import (
	"context"
	"encoding/hex"
	"fmt"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/server"
	sdk "github.com/cosmos/cosmos-sdk/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"

	"github.com/AryaSingh22/conclave/app/dkgdaemon"
	"github.com/AryaSingh22/conclave/app/dkgshare"
	"github.com/AryaSingh22/conclave/crypto/threshold"
)

// DKG daemon start flags (Task 1.7, D-070).
const (
	flagDKGDaemon     = "dkg-daemon"
	flagDKGFrom       = "dkg-from"
	flagDKGFaultyDeal = "dkg-faulty-deal"
)

// dkgKeygenCmd generates this node's long-term DKG keypair and stores the secret
// node-locally (<home>/config/dkg_longterm_key, 0600). The matching public is
// printed and is what the node registers ON-CHAIN during a FORMING epoch's
// register window (Task 1.7 A2, D-070) — it is NOT placed in genesis. Run once
// per node before `start --dkg-daemon`.
func dkgKeygenCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dkg-keygen",
		Short: "Generate this node's long-term DKG key (node-local secret + on-chain-registered public)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			home := clientCtx.HomeDir
			secret, public, err := threshold.GenerateLongTermKey()
			if err != nil {
				return err
			}
			if err := dkgshare.WriteLongTermSecret(home, secret); err != nil {
				return err
			}
			cmd.Printf("DKG long-term key generated.\n  secret: %s\n  public: %s\n",
				dkgshare.LongTermKeyPath(home), hex.EncodeToString(public))
			return nil
		},
	}
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// addDKGStartFlags registers the DKG-daemon flags on the start command.
func addDKGStartFlags(startCmd *cobra.Command) {
	startCmd.Flags().Bool(flagDKGDaemon, false, "run the off-chain DKG daemon in-process (validators only)")
	startCmd.Flags().String(flagDKGFrom, "", "keyring key name the DKG daemon signs transport txs with (this validator's account)")
	startCmd.Flags().Bool(flagDKGFaultyDeal, false, "ADVERSARIAL: submit a faulty deal and refuse to justify (faulty-dealer test only)")
	// the daemon reads this to open the validator's keyring; start does not define
	// it otherwise, so register it here (default test for the dev localnet).
	startCmd.Flags().String(flags.FlagKeyringBackend, keyring.BackendTest, "keyring backend the DKG daemon uses to load the validator key")
}

// startDKGDaemon is the server PostSetup hook (in-process only): if --dkg-daemon
// is set it loads the node-local long-term key, resolves this validator's
// identity from the keyring, and launches the daemon under the node's cancellable
// context (Task 1.7, D-070). The secret share it later writes never leaves the
// node process.
func startDKGDaemon(svrCtx *server.Context, clientCtx client.Context, ctx context.Context, g *errgroup.Group) error {
	v := svrCtx.Viper
	if !v.GetBool(flagDKGDaemon) {
		return nil
	}
	logger := svrCtx.Logger
	home := clientCtx.HomeDir
	if home == "" {
		home = svrCtx.Config.RootDir
	}

	if clientCtx.Client == nil {
		logger.Error("dkg daemon: no node RPC client (enable api or grpc) — daemon not started")
		return nil
	}

	secret, err := dkgshare.ReadLongTermSecret(home)
	if err != nil {
		logger.Error("dkg daemon: no long-term key (run 'conclaved dkg-keygen') — daemon not started", "err", err)
		return nil
	}

	fromName := v.GetString(flagDKGFrom)
	if fromName == "" {
		logger.Error("dkg daemon: --dkg-from is required — daemon not started")
		return nil
	}
	backend := v.GetString(flags.FlagKeyringBackend)
	if backend == "" {
		backend = keyring.BackendTest
	}
	kr, err := keyring.New("conclaved", backend, home, clientCtx.Input, clientCtx.Codec)
	if err != nil {
		return fmt.Errorf("dkg daemon: keyring: %w", err)
	}
	rec, err := kr.Key(fromName)
	if err != nil {
		return fmt.Errorf("dkg daemon: key %q: %w", fromName, err)
	}
	addr, err := rec.GetAddress()
	if err != nil {
		return err
	}
	valoper := sdk.ValAddress(addr).String()

	chainID := clientCtx.ChainID
	if chainID == "" {
		if ag, err := genutiltypes.AppGenesisFromFile(svrCtx.Config.GenesisFile()); err == nil {
			chainID = ag.ChainID
		}
	}

	shares, err := dkgshare.NewFileProvider(dkgShareDir(home))
	if err != nil {
		return fmt.Errorf("dkg daemon: share store: %w", err)
	}

	daemonCtx := clientCtx.WithKeyring(kr).WithChainID(chainID).WithSkipConfirmation(true)
	d, err := dkgdaemon.New(daemonCtx, logger, dkgdaemon.Config{
		Valoper:        valoper,
		FromName:       fromName,
		FromAddr:       addr,
		LongTermSecret: secret,
		Shares:         shares,
		FaultyDeal:     v.GetBool(flagDKGFaultyDeal),
	})
	if err != nil {
		return err
	}
	logger.Info("launching DKG daemon", "valoper", valoper, "from", fromName, "faulty", v.GetBool(flagDKGFaultyDeal))
	g.Go(func() error { return d.Run(ctx) })
	return nil
}

func dkgShareDir(home string) string {
	return home + "/data/" + dkgshare.DirName
}
