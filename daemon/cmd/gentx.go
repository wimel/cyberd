package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/cybercongress/cyberd/app"
	"github.com/cybercongress/cyberd/util"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"io"
	"io/ioutil"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/context"
	"github.com/cosmos/cosmos-sdk/client/keys"
	"github.com/cosmos/cosmos-sdk/client/utils"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/server"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth"
	authtxb "github.com/cosmos/cosmos-sdk/x/auth/client/txbuilder"
	"github.com/cosmos/cosmos-sdk/x/staking/client/cli"
	cfg "github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/crypto"
	tmcli "github.com/tendermint/tendermint/libs/cli"
)

const (
	defaultAmount                  = "100cyb"
	defaultCommissionRate          = "0.1"
	defaultCommissionMaxRate       = "0.2"
	defaultCommissionMaxChangeRate = "0.01"
	defaultMinSelfDelegation       = "1"
)

// GenTxCmd builds the cyberd gentx command.
// nolint: errcheck
func GenTxCmd(ctx *server.Context, cdc *codec.Codec) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gentx",
		Short: "Generate a genesis tx carrying a self delegation and add it to genesis.json",
		Long: fmt.Sprintf(`This command is an alias of the 'cyberdcli create-validator' command'.

It creates a genesis piece carrying a self delegation with the
following delegation and commission default parameters:

	delegation amount:           %s
	commission rate:             %s
	commission max rate:         %s
	commission max change rate:  %s
`, defaultAmount, defaultCommissionRate, defaultCommissionMaxRate, defaultCommissionMaxChangeRate),
		RunE: func(cmd *cobra.Command, args []string) error {

			config := ctx.Config
			config.SetRoot(viper.GetString(tmcli.HomeFlag))
			nodeID, valPubKey, err := InitializeNodeValidatorFiles(ctx.Config)
			if err != nil {
				return err
			}
			ip, err := server.ExternalIP()
			if err != nil {
				return err
			}

			doc, state, err := app.LoadGenesisState(ctx, cdc)
			if err != nil {
				return err
			}

			kb, err := keys.NewKeyBaseFromDir(viper.GetString(flagClientHome))
			if err != nil {
				return err
			}

			name := viper.GetString(client.FlagName)
			key, err := kb.Get(name)
			if err != nil {
				return err
			}

			// Read --pubkey, if empty take it from priv_validator.json
			if valPubKeyString := viper.GetString(cli.FlagPubKey); valPubKeyString != "" {
				valPubKey, err = sdk.GetConsPubKeyBech32(valPubKeyString)
				if err != nil {
					return err
				}
			}

			// Set flags for creating gentx
			prepareFlagsForTxCreateValidator(config, nodeID, ip, doc.ChainID, valPubKey)

			// Fetch the amount of coins staked
			amount := viper.GetString(cli.FlagAmount)
			coins, err := sdk.ParseCoins(amount)
			if err != nil {
				return err
			}

			err = accountInGenesis(state, key.GetAddress(), coins)
			if err != nil {
				return err
			}

			// Run cyberd tx create-validator
			txBldr := authtxb.NewTxBuilderFromCLI().WithTxEncoder(utils.GetTxEncoder(cdc))
			cliCtx := context.NewCLIContext().WithCodec(cdc)
			txBldr, msg, err := cli.BuildCreateValidatorMsg(cliCtx, txBldr)
			if err != nil {
				return err
			}

			// write the unsigned transaction to the buffer
			w := bytes.NewBuffer([]byte{})
			cliCtx = cliCtx.WithOutput(w)
			if err = utils.PrintUnsignedStdTx(txBldr, cliCtx, []sdk.Msg{msg}, true); err != nil {
				return err
			}

			// read the transaction
			stdTx, err := readUnsignedGenTxFile(cdc, w)
			if err != nil {
				return err
			}

			// sign the transaction and write it to the output file
			signedTx, err := utils.SignStdTx(txBldr, cliCtx, name, stdTx, false, true)
			if err != nil {
				return err
			}

			txAsJson, err := cdc.MarshalJSON(signedTx)
			if err != nil {
				return err
			}

			if len(state.GenTxs) == 0 {
				state.GenTxs = []json.RawMessage{}
			}
			state.GenTxs = append(state.GenTxs, txAsJson)
			stateJson, err := app.CyberdAppGenStateJSON(cdc, doc, state.GenTxs)
			if err != nil {
				return err
			}

			return util.ExportGenesisFile(config.GenesisFile(), doc.ChainID, doc.Validators, stateJson)
		},
	}

	cmd.Flags().String(tmcli.HomeFlag, app.DefaultNodeHome, "node's home directory")
	cmd.Flags().String(flagClientHome, app.DefaultCLIHome, "client's home directory")
	cmd.Flags().String(client.FlagName, "", "name of private key with which to sign the gentx")
	cmd.Flags().String(client.FlagOutputDocument, "",
		"write the genesis transaction JSON document to the given file instead of the default location")
	cmd.Flags().String(cli.FlagMoniker, "", "validator display name")
	cmd.Flags().AddFlagSet(cli.FsCommissionCreate)
	cmd.Flags().AddFlagSet(cli.FsAmount)
	cmd.Flags().AddFlagSet(cli.FsPk)
	cmd.MarkFlagRequired(client.FlagName)
	cmd.MarkFlagRequired(cli.FlagMoniker)
	return cmd
}

func accountInGenesis(genesisState app.GenesisState, key sdk.AccAddress, coins sdk.Coins) error {

	accountIsInGenesis := false
	bondDenom := genesisState.StakingData.Params.BondDenom

	for _, acc := range genesisState.Accounts {
		if acc.Address.Equals(key) {

			// Ensure account contains enough funds of default bond denom
			if coins.AmountOf(bondDenom).GT(sdk.NewInt(acc.Amount)) {
				return fmt.Errorf(
					"account %v is in genesis, but it only has %v%v available to stake, not %v%v",
					key.String(), acc.Amount, bondDenom, coins.AmountOf(bondDenom), bondDenom,
				)
			}
			accountIsInGenesis = true
			break
		}
	}

	if accountIsInGenesis {
		return nil
	}

	return fmt.Errorf("account %s in not in the app_state.accounts array of genesis.json", key)
}

func prepareFlagsForTxCreateValidator(config *cfg.Config, nodeID, ip, chainID string,
	valPubKey crypto.PubKey) {
	viper.Set(tmcli.HomeFlag, viper.GetString(flagClientHome)) // --home
	viper.Set(client.FlagChainID, chainID)
	viper.Set(client.FlagFrom, viper.GetString(client.FlagName))   // --from
	viper.Set(cli.FlagNodeID, nodeID)                              // --node-id
	viper.Set(cli.FlagIP, ip)                                      // --ip
	viper.Set(cli.FlagPubKey, sdk.MustBech32ifyConsPub(valPubKey)) // --pubkey
	viper.Set(cli.FlagGenesisFormat, true)                         // --genesis-format

	if viper.GetString(cli.FlagAmount) == "" {
		viper.Set(cli.FlagAmount, defaultAmount)
	}
	if viper.GetString(cli.FlagMoniker) == "" {
		viper.Set(cli.FlagMoniker, viper.GetString(client.FlagName))
	}
	if viper.GetString(cli.FlagCommissionRate) == "" {
		viper.Set(cli.FlagCommissionRate, defaultCommissionRate)
	}
	if viper.GetString(cli.FlagCommissionMaxRate) == "" {
		viper.Set(cli.FlagCommissionMaxRate, defaultCommissionMaxRate)
	}
	if viper.GetString(cli.FlagCommissionMaxChangeRate) == "" {
		viper.Set(cli.FlagCommissionMaxChangeRate, defaultCommissionMaxChangeRate)
	}
	if viper.GetString(cli.FlagMinSelfDelegation) == "" {
		viper.Set(cli.FlagMinSelfDelegation, defaultMinSelfDelegation)
	}
}

func readUnsignedGenTxFile(cdc *codec.Codec, r io.Reader) (auth.StdTx, error) {
	var stdTx auth.StdTx
	bytes, err := ioutil.ReadAll(r)
	if err != nil {
		return stdTx, err
	}
	err = cdc.UnmarshalJSON(bytes, &stdTx)
	return stdTx, err
}
