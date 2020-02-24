package suites

import (
	"context"
	"testing"

	"github.com/filecoin-project/go-address"

	abi_spec "github.com/filecoin-project/specs-actors/actors/abi"
	big_spec "github.com/filecoin-project/specs-actors/actors/abi/big"
	builtin_spec "github.com/filecoin-project/specs-actors/actors/builtin"
	account_spec "github.com/filecoin-project/specs-actors/actors/builtin/account"
	init_spec "github.com/filecoin-project/specs-actors/actors/builtin/init"
	reward_spec "github.com/filecoin-project/specs-actors/actors/builtin/reward"
	exitcode_spec "github.com/filecoin-project/specs-actors/actors/runtime/exitcode"

	"github.com/filecoin-project/chain-validation/chain"
	"github.com/filecoin-project/chain-validation/chain/types"
	"github.com/filecoin-project/chain-validation/drivers"
	"github.com/filecoin-project/chain-validation/state"
	"github.com/filecoin-project/chain-validation/suites/utils"
)

func TestAccountActorCreation(t *testing.T, factory state.Factories) {
	defaultMiner := utils.NewBLSAddr(t, 123)

	builder := drivers.NewBuilder(context.Background(), factory).
		WithDefaultGasLimit(big_spec.NewInt(1_000_000)).
		WithDefaultGasPrice(big_spec.NewInt(1)).
		WithDefaultMiner(defaultMiner).
		WithActorState([]drivers.ActorState{
			{
				Addr:    builtin_spec.InitActorAddr,
				Balance: big_spec.Zero(),
				Code:    builtin_spec.InitActorCodeID,
				State:   init_spec.ConstructState(drivers.EmptyMapCid, "chain-validation"),
			},
			{
				Addr:    builtin_spec.RewardActorAddr,
				Balance: TotalNetworkBalance,
				Code:    builtin_spec.RewardActorCodeID,
				State:   reward_spec.ConstructState(drivers.EmptyMultiMapCid),
			},
			{
				Addr:    builtin_spec.BurntFundsActorAddr,
				Balance: big_spec.Zero(),
				Code:    builtin_spec.AccountActorCodeID,
				State:   &account_spec.State{Address: builtin_spec.BurntFundsActorAddr},
			},
		})

	testCases := []struct {
		desc string

		existingActorType address.Protocol
		existingActorBal  abi_spec.TokenAmount

		newActorAddr    address.Address
		newActorInitBal abi_spec.TokenAmount

		expGasCost  abi_spec.TokenAmount
		expExitCode exitcode_spec.ExitCode
	}{
		{
			"success create SECP256K1 account actor",
			address.SECP256K1,
			abi_spec.NewTokenAmount(10_000_000),

			utils.NewSECP256K1Addr(t, "publickeyfoo"),
			abi_spec.NewTokenAmount(10_000),

			abi_spec.NewTokenAmount(0),
			exitcode_spec.Ok,
		},
		{
			"success create BLS account actor",
			address.SECP256K1,
			abi_spec.NewTokenAmount(10_000_000),

			utils.NewBLSAddr(t, 1),
			abi_spec.NewTokenAmount(10_000),

			abi_spec.NewTokenAmount(0),
			exitcode_spec.Ok,
		},
		{
			"fail create SECP256K1 account actor insufficient balance",
			address.SECP256K1,
			abi_spec.NewTokenAmount(9_999),

			utils.NewSECP256K1Addr(t, "publickeybar"),
			abi_spec.NewTokenAmount(10_000),

			abi_spec.NewTokenAmount(0),
			exitcode_spec.SysErrInsufficientFunds,
		},
		{
			"fail create BLS account actor insufficient balance",
			address.BLS,
			abi_spec.NewTokenAmount(9_999),

			utils.NewSECP256K1Addr(t, "publickeybaz"),
			abi_spec.NewTokenAmount(10_000),

			abi_spec.NewTokenAmount(0),
			exitcode_spec.SysErrInsufficientFunds,
		},
		// TODO add edge case tests that have insufficient balance after gas fees
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			td := builder.Build(t)

			existingAccountAddr := td.NewAccountActor(tc.existingActorType, tc.existingActorBal)
			td.ApplyMessageExpectReceipt(
				td.Producer.Transfer(tc.newActorAddr, existingAccountAddr, chain.Value(tc.newActorInitBal), chain.Nonce(0)),
				types.MessageReceipt{ExitCode: tc.expExitCode, ReturnValue: EmptyReturnValue, GasUsed: tc.expGasCost},
			)

			// new actor balance will only exist if message was applied successfully.
			if tc.expExitCode.IsSuccess() {
				td.AssertBalance(tc.newActorAddr, tc.newActorInitBal)
				td.AssertBalanceWithGas(existingAccountAddr, tc.existingActorBal, tc.expGasCost)
			}
		})
	}
}

func TestInitActorSequentialIDAddressCreate(t *testing.T, factory state.Factories) {
	defaultMiner := utils.NewBLSAddr(t, 123)

	td := drivers.NewBuilder(context.Background(), factory).
		WithDefaultGasLimit(big_spec.NewInt(1_000_000)).
		WithDefaultGasPrice(big_spec.NewInt(1)).
		WithDefaultMiner(defaultMiner).
		WithActorState([]drivers.ActorState{
			{
				Addr:    builtin_spec.InitActorAddr,
				Balance: big_spec.Zero(),
				Code:    builtin_spec.InitActorCodeID,
				State:   init_spec.ConstructState(drivers.EmptyMapCid, "chain-validation"),
			},
			{
				Addr:    builtin_spec.RewardActorAddr,
				Balance: TotalNetworkBalance,
				Code:    builtin_spec.RewardActorCodeID,
				State:   reward_spec.ConstructState(drivers.EmptyMultiMapCid),
			},
			{
				Addr:    builtin_spec.BurntFundsActorAddr,
				Balance: big_spec.Zero(),
				Code:    builtin_spec.AccountActorCodeID,
				State:   &account_spec.State{Address: builtin_spec.BurntFundsActorAddr},
			},
		}).Build(t)

	var initialBal = abi_spec.NewTokenAmount(200_000_000_000)
	var toSend = abi_spec.NewTokenAmount(10_000)

	sender := td.NewAccountActor(drivers.SECP, initialBal)
	senderID := utils.NewIDAddr(t, 100)

	receiver := td.NewAccountActor(drivers.SECP, initialBal)

	firstPaychAddr := utils.NewIDAddr(t, 102)
	secondPaychAddr := utils.NewIDAddr(t, 103)

	firstInitRet := td.ComputeInitActorExecReturn(senderID, 0, firstPaychAddr)
	secondInitRet := td.ComputeInitActorExecReturn(senderID, 1, secondPaychAddr)

	td.ApplyMessageExpectReceipt(
		td.Producer.CreatePaymentChannelActor(receiver, sender, chain.Value(toSend), chain.Nonce(0)),
		types.MessageReceipt{ExitCode: exitcode_spec.Ok, ReturnValue: chain.MustSerialize(&firstInitRet), GasUsed: big_spec.Zero()},
	)

	td.ApplyMessageExpectReceipt(
		td.Producer.CreatePaymentChannelActor(receiver, sender, chain.Value(toSend), chain.Nonce(1)),
		types.MessageReceipt{ExitCode: exitcode_spec.Ok, ReturnValue: chain.MustSerialize(&secondInitRet), GasUsed: big_spec.Zero()},
	)

}