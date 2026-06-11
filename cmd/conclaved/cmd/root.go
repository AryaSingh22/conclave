package cmd

import (
	"os"

	dbm "github.com/cosmos/cosmos-db"
	"github.com/spf13/cobra"

	"cosmossdk.io/log"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/config"
	"github.com/cosmos/cosmos-sdk/server"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	"github.com/cosmos/cosmos-sdk/types/module"
	signingtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	authtxconfig "github.com/cosmos/cosmos-sdk/x/auth/tx/config"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/genutil"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	"github.com/cosmos/cosmos-sdk/x/gov"
	govclient "github.com/cosmos/cosmos-sdk/x/gov/client"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	paramsclient "github.com/cosmos/cosmos-sdk/x/params/client"

	"github.com/AryaSingh22/conclave/app"
)

// NewRootCmd creates the root command for conclaved.
//
// A throwaway in-memory app instance is built first to derive the encoding
// config, the module basic manager (including the manually wired IBC
// modules), and the autocli options — the standard pattern for hybrid
// depinject apps whose module set is not fully expressible in the app config.
func NewRootCmd() *cobra.Command {
	tempApp := app.NewConclaveApp(log.NewNopLogger(), dbm.NewMemDB(), nil, true, simtestutil.EmptyAppOptions{})

	basicManager := module.NewBasicManagerFromManager(
		tempApp.ModuleManager,
		map[string]module.AppModuleBasic{
			genutiltypes.ModuleName: genutil.NewAppModuleBasic(genutiltypes.DefaultMessageValidator),
			govtypes.ModuleName: gov.NewAppModuleBasic(
				[]govclient.ProposalHandler{
					paramsclient.ProposalHandler,
				},
			),
		})

	initClientCtx := client.Context{}.
		WithCodec(tempApp.AppCodec()).
		WithInterfaceRegistry(tempApp.InterfaceRegistry()).
		WithTxConfig(tempApp.TxConfig()).
		WithLegacyAmino(tempApp.LegacyAmino()).
		WithInput(os.Stdin).
		WithAccountRetriever(authtypes.AccountRetriever{}).
		WithHomeDir(app.DefaultNodeHome).
		WithViper("") // env variable prefix is set by svrcmd.Execute

	rootCmd := &cobra.Command{
		Use:           "conclaved",
		Short:         "Conclave — a consensus-native sealed-bid exchange for autonomous agents",
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			// set the default command outputs
			cmd.SetOut(cmd.OutOrStdout())
			cmd.SetErr(cmd.ErrOrStderr())

			clientCtx := initClientCtx.WithCmdContext(cmd.Context())
			clientCtx, err := client.ReadPersistentCommandFlags(clientCtx, cmd.Flags())
			if err != nil {
				return err
			}

			clientCtx, err = config.ReadFromClientConfig(clientCtx)
			if err != nil {
				return err
			}

			// re-create the tx config with textual sign mode enabled, which
			// requires a live gRPC coin-metadata query
			if !clientCtx.Offline {
				txConfigOpts := authtx.ConfigOptions{
					EnabledSignModes:           append(authtx.DefaultSignModes, signingtypes.SignMode_SIGN_MODE_TEXTUAL),
					TextualCoinMetadataQueryFn: authtxconfig.NewGRPCCoinMetadataQueryFn(clientCtx),
				}
				txConfig, err := authtx.NewTxConfigWithOptions(clientCtx.Codec, txConfigOpts)
				if err != nil {
					return err
				}
				clientCtx = clientCtx.WithTxConfig(txConfig)
			}

			if err := client.SetCmdClientContextHandler(clientCtx, cmd); err != nil {
				return err
			}

			customAppTemplate, customAppConfig := initAppConfig()
			customCMTConfig := initCometBFTConfig()

			return server.InterceptConfigsPreRunHandler(cmd, customAppTemplate, customAppConfig, customCMTConfig)
		},
	}

	initRootCmd(rootCmd, tempApp.TxConfig(), basicManager)

	autoCliOpts := tempApp.AutoCliOpts()
	autoCliOpts.ClientCtx = initClientCtx
	if err := autoCliOpts.EnhanceRootCommand(rootCmd); err != nil {
		panic(err)
	}

	return rootCmd
}
