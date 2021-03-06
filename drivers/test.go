package drivers

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/filecoin-project/go-bitfield"

	"github.com/filecoin-project/go-address"
	abi_spec "github.com/filecoin-project/specs-actors/actors/abi"
	big_spec "github.com/filecoin-project/specs-actors/actors/abi/big"
	builtin_spec "github.com/filecoin-project/specs-actors/actors/builtin"
	account_spec "github.com/filecoin-project/specs-actors/actors/builtin/account"
	cron_spec "github.com/filecoin-project/specs-actors/actors/builtin/cron"
	init_spec "github.com/filecoin-project/specs-actors/actors/builtin/init"
	market_spec "github.com/filecoin-project/specs-actors/actors/builtin/market"
	"github.com/filecoin-project/specs-actors/actors/builtin/miner"
	multisig_spec "github.com/filecoin-project/specs-actors/actors/builtin/multisig"
	power_spec "github.com/filecoin-project/specs-actors/actors/builtin/power"
	reward_spec "github.com/filecoin-project/specs-actors/actors/builtin/reward"
	"github.com/filecoin-project/specs-actors/actors/builtin/system"
	runtime_spec "github.com/filecoin-project/specs-actors/actors/runtime"
	"github.com/filecoin-project/specs-actors/actors/runtime/exitcode"
	adt_spec "github.com/filecoin-project/specs-actors/actors/util/adt"
	cid "github.com/ipfs/go-cid"
	datastore "github.com/ipfs/go-datastore"
	blockstore "github.com/ipfs/go-ipfs-blockstore"
	cbor "github.com/ipfs/go-ipld-cbor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/chain-validation/chain"
	"github.com/filecoin-project/chain-validation/chain/types"
	"github.com/filecoin-project/chain-validation/state"
	"github.com/filecoin-project/chain-validation/tracker"
)

var (

	// initialized by calling initializeStoreWithAdtRoots
	EmptyArrayCid        cid.Cid
	EmptyDeadlinesCid    cid.Cid
	EmptyVestingFundsCid cid.Cid
	EmptyMapCid          cid.Cid
	EmptyMultiMapCid     cid.Cid
	EmptyBitfieldCid     cid.Cid
)

var (
	DefaultInitActorState          ActorState
	DefaultRewardActorState        ActorState
	DefaultBurntFundsActorState    ActorState
	DefaultStoragePowerActorState  ActorState
	DefaultStorageMarketActorState ActorState
	DefaultSystemActorState        ActorState
	DefaultCronActorState          ActorState
	DefaultBuiltinActorsState      []ActorState
)

const (
	TestSealProofType = abi_spec.RegisteredSealProof_StackedDrg2KiBV1
)

func init() {
	ms := newMockStore()
	if err := initializeStoreWithAdtRoots(ms); err != nil {
		panic(err)
	}

	DefaultInitActorState = ActorState{
		Addr:    builtin_spec.InitActorAddr,
		Balance: big_spec.Zero(),
		Code:    builtin_spec.InitActorCodeID,
		State:   init_spec.ConstructState(EmptyMapCid, "chain-validation"),
	}

	firstRewardState := reward_spec.ConstructState(big_spec.Zero())
	firstRewardState.ThisEpochReward = big_spec.NewInt(1e17)

	DefaultRewardActorState = ActorState{
		Addr:    builtin_spec.RewardActorAddr,
		Balance: TotalNetworkBalance,
		Code:    builtin_spec.RewardActorCodeID,
		State:   firstRewardState,
	}

	DefaultBurntFundsActorState = ActorState{
		Addr:    builtin_spec.BurntFundsActorAddr,
		Balance: big_spec.Zero(),
		Code:    builtin_spec.AccountActorCodeID,
		State:   &account_spec.State{Address: builtin_spec.BurntFundsActorAddr},
	}

	DefaultStoragePowerActorState = ActorState{
		Addr:    builtin_spec.StoragePowerActorAddr,
		Balance: big_spec.Zero(),
		Code:    builtin_spec.StoragePowerActorCodeID,
		State:   power_spec.ConstructState(EmptyMapCid, EmptyMultiMapCid),
	}

	DefaultStorageMarketActorState = ActorState{
		Addr:    builtin_spec.StorageMarketActorAddr,
		Balance: big_spec.Zero(),
		Code:    builtin_spec.StorageMarketActorCodeID,
		State: &market_spec.State{
			Proposals:        EmptyArrayCid,
			States:           EmptyArrayCid,
			PendingProposals: EmptyMapCid,
			EscrowTable:      EmptyMapCid,
			LockedTable:      EmptyMapCid,
			NextID:           abi_spec.DealID(0),
			DealOpsByEpoch:   EmptyMultiMapCid,
			LastCron:         0,
		},
	}

	DefaultSystemActorState = ActorState{
		Addr:    builtin_spec.SystemActorAddr,
		Balance: big_spec.Zero(),
		Code:    builtin_spec.SystemActorCodeID,
		State:   &system.State{},
	}

	DefaultCronActorState = ActorState{
		Addr:    builtin_spec.CronActorAddr,
		Balance: big_spec.Zero(),
		Code:    builtin_spec.CronActorCodeID,
		State: &cron_spec.State{Entries: []cron_spec.Entry{
			{
				Receiver:  builtin_spec.StoragePowerActorAddr,
				MethodNum: builtin_spec.MethodsPower.OnEpochTickEnd,
			},
		}},
	}

	DefaultBuiltinActorsState = []ActorState{
		DefaultInitActorState,
		DefaultRewardActorState,
		DefaultBurntFundsActorState,
		DefaultStoragePowerActorState,
		DefaultStorageMarketActorState,
		DefaultSystemActorState,
		DefaultCronActorState,
	}
}

func initializeStoreWithAdtRoots(store adt_spec.Store) error {
	var err error
	EmptyArrayCid, err = adt_spec.MakeEmptyArray(store).Root()
	if err != nil {
		return err
	}

	EmptyMapCid, err = adt_spec.MakeEmptyMap(store).Root()
	if err != nil {
		return err
	}

	EmptyMultiMapCid, err = adt_spec.MakeEmptyMultimap(store).Root()
	if err != nil {
		return err
	}

	EmptyDeadlinesCid, err = store.Put(context.TODO(), miner.ConstructDeadline(EmptyArrayCid))
	if err != nil {
		return err
	}

	EmptyVestingFundsCid, err = store.Put(context.Background(), miner.ConstructVestingFunds())
	if err != nil {
		return err
	}

	EmptyBitfieldCid, err = store.Put(context.TODO(), bitfield.New())
	if err != nil {
		return err
	}

	return nil
}

type mockStore struct {
	ctx context.Context
	cbor.IpldStore
}

func newMockStore() *mockStore {
	bs := blockstore.NewBlockstore(datastore.NewMapDatastore())
	cst := cbor.NewCborStore(bs)
	return &mockStore{
		ctx:       context.Background(),
		IpldStore: cst,
	}
}

func (m mockStore) Context() context.Context {
	return m.ctx
}

type TestDriverBuilder struct {
	ctx     context.Context
	factory state.Factories

	actorStates []ActorState

	defaultGasFeeCap  abi_spec.TokenAmount
	defaultGasPremium abi_spec.TokenAmount
	defaultGasLimit   int64
}

func NewBuilder(ctx context.Context, factory state.Factories) *TestDriverBuilder {
	return &TestDriverBuilder{
		factory: factory,
		ctx:     ctx,
	}
}

type ActorState struct {
	Addr    address.Address
	Balance abi_spec.TokenAmount
	Code    cid.Cid
	State   runtime_spec.CBORMarshaler
}

func (b *TestDriverBuilder) WithActorState(acts ...ActorState) *TestDriverBuilder {
	b.actorStates = append(b.actorStates, acts...)
	return b
}

func (b *TestDriverBuilder) WithDefaultGasLimit(limit int64) *TestDriverBuilder {
	b.defaultGasLimit = limit
	return b
}

func (b *TestDriverBuilder) WithDefaultGasFeeCap(feeCap int64) *TestDriverBuilder {
	b.defaultGasFeeCap = abi_spec.NewTokenAmount(feeCap)
	return b
}

func (b *TestDriverBuilder) WithDefaultGasPremium(premium int64) *TestDriverBuilder {
	b.defaultGasPremium = abi_spec.NewTokenAmount(premium)
	return b
}

func (b *TestDriverBuilder) Build(t testing.TB) *TestDriver {
	syscalls := NewChainValidationSysCalls()
	stateWrapper, applier := b.factory.NewStateAndApplier(syscalls)
	sd := NewStateDriver(t, stateWrapper, b.factory.NewKeyManager())
	stateWrapper.NewVM()

	err := initializeStoreWithAdtRoots(AsStore(sd.st))
	require.NoError(t, err)

	for _, acts := range b.actorStates {
		_, _, err := sd.State().CreateActor(acts.Code, acts.Addr, acts.Balance, acts.State)
		require.NoError(t, err)
	}

	minerActorIDAddr := sd.newMinerAccountActor(TestSealProofType, abi_spec.ChainEpoch(0))

	exeCtx := types.NewExecutionContext(1, minerActorIDAddr)
	producer := chain.NewMessageProducer(b.defaultGasFeeCap, b.defaultGasPremium, b.defaultGasLimit)
	validator := chain.NewValidator(applier)

	return &TestDriver{
		T:               t,
		StateDriver:     sd,
		MessageProducer: producer,
		validator:       validator,
		ExeCtx:          exeCtx,

		Config: b.factory.NewValidationConfig(),

		StateTracker: tracker.NewStateTracker(t),

		SysCalls: syscalls,
	}
}

type TestDriver struct {
	*StateDriver

	T                    testing.TB
	MessageProducer      *chain.MessageProducer
	TipSetMessageBuilder *TipSetMessageBuilder
	validator            *chain.Validator
	ExeCtx               *types.ExecutionContext

	Config state.ValidationConfig

	StateTracker *tracker.StateTracker

	SysCalls *ChainValidationSysCalls
}

func (td *TestDriver) Complete() {
	//
	// Gas expectation recording.
	// Uncomment the following line to persist the actual gas values used to file as the new set
	// of expectations.
	//td.StateTracker.Record()
}

//
// Unsigned Message Appliers
//

func (td *TestDriver) ApplyMessage(msg *types.Message) types.ApplyMessageResult {
	result := td.applyMessage(msg)
	td.validateState(msg, result)
	return result
}

func (td *TestDriver) ApplyOk(msg *types.Message) types.ApplyMessageResult {
	return td.ApplyExpect(msg, EmptyReturnValue)
}

func (td *TestDriver) ApplyExpect(msg *types.Message, retval []byte) types.ApplyMessageResult {
	return td.applyMessageExpectCodeAndReturn(msg, exitcode.Ok, retval)
}

func (td *TestDriver) ApplyFailure(msg *types.Message, code exitcode.ExitCode) types.ApplyMessageResult {
	return td.applyMessageExpectCodeAndReturn(msg, code, EmptyReturnValue)
}

func (td *TestDriver) applyMessageExpectCodeAndReturn(msg *types.Message, code exitcode.ExitCode, retval []byte) types.ApplyMessageResult {
	result := td.applyMessage(msg)
	td.validateResult(result, code, retval)
	td.validateState(msg, result)
	return result
}

func (td *TestDriver) applyMessage(msg *types.Message) (result types.ApplyMessageResult) {
	defer func() {
		if r := recover(); r != nil {
			td.T.Fatalf("message application panicked: %v", r)
		}
	}()

	result, err := td.validator.ApplyMessage(td.ExeCtx.Epoch, msg)
	require.NoError(td.T, err)

	td.StateTracker.TrackResult(result)
	return result
}

//
// Signed Message Appliers
//

func (td *TestDriver) ApplySigned(msg *types.Message) types.ApplyMessageResult {
	result := td.applyMessageSigned(msg)
	td.validateState(msg, result)
	return result
}

func (td *TestDriver) ApplySignedOk(msg *types.Message) types.ApplyMessageResult {
	return td.ApplySignedExpect(msg, EmptyReturnValue)
}

func (td *TestDriver) ApplySignedExpect(msg *types.Message, retval []byte) types.ApplyMessageResult {
	return td.applyMessageSignedExpectCodeAndReturn(msg, exitcode.Ok, retval)
}

func (td *TestDriver) ApplySignedFailure(msg *types.Message, code exitcode.ExitCode) types.ApplyMessageResult {
	return td.applyMessageExpectCodeAndReturn(msg, code, EmptyReturnValue)
}

func (td *TestDriver) applyMessageSignedExpectCodeAndReturn(msg *types.Message, code exitcode.ExitCode, retval []byte) types.ApplyMessageResult {
	result := td.applyMessageSigned(msg)
	td.validateResult(result, code, retval)
	td.validateState(msg, result)
	return result
}
func (td *TestDriver) applyMessageSigned(msg *types.Message) (result types.ApplyMessageResult) {
	defer func() {
		if r := recover(); r != nil {
			td.T.Fatalf("message application panicked: %v", r)
		}
	}()
	serMsg, err := msg.Serialize()
	require.NoError(td.T, err)

	msgSig, err := td.Wallet().Sign(msg.From, serMsg)
	require.NoError(td.T, err)

	smsgs := &types.SignedMessage{
		Message:   *msg,
		Signature: msgSig,
	}
	result, err = td.validator.ApplySignedMessage(td.ExeCtx.Epoch, smsgs)
	require.NoError(td.T, err)

	td.StateTracker.TrackResult(result)
	return result
}

func (td *TestDriver) validateResult(result types.ApplyMessageResult, code exitcode.ExitCode, retval []byte) {
	if td.Config.ValidateExitCode() {
		assert.Equal(td.T, code, result.Receipt.ExitCode, "Expected ExitCode: %s Actual ExitCode: %s", code.Error(), result.Receipt.ExitCode.Error())
	}
	if td.Config.ValidateReturnValue() {
		assert.Equal(td.T, retval, result.Receipt.ReturnValue, "Expected ReturnValue: %v Actual ReturnValue: %v", retval, result.Receipt.ReturnValue)
	}
}

func (td *TestDriver) validateState(msg *types.Message, result types.ApplyMessageResult) {
	if td.Config.ValidateGas() {
		expectedGasUsed, ok := td.StateTracker.NextExpectedGas()
		if ok {
			assert.Equal(td.T, expectedGasUsed, result.Receipt.GasUsed, "Expected GasUsed: %d Actual GasUsed: %d", expectedGasUsed, result.Receipt.GasUsed)
		} else {
			td.T.Logf("WARNING (not a test failure): failed to find expected gas cost for message: %+v", msg)
		}
	}
	if td.Config.ValidateStateRoot() {
		expectedRoot, found := td.StateTracker.NextExpectedStateRoot()
		actualRoot := td.State().Root()
		if found {
			assert.Equal(td.T, expectedRoot, actualRoot, "Expected StateRoot: %s Actual StateRoot: %s", expectedRoot, actualRoot)
		} else {
			td.T.Log("WARNING: failed to find expected state  root for message number")
		}
	}
}

func (td *TestDriver) AssertNoActor(addr address.Address) {
	_, err := td.State().Actor(addr)
	assert.Error(td.T, err, "expected no such actor %s", addr)
}

func (td *TestDriver) GetBalance(addr address.Address) abi_spec.TokenAmount {
	actr, err := td.State().Actor(addr)
	require.NoError(td.T, err)
	return actr.Balance()
}

func (td *TestDriver) GetHead(addr address.Address) cid.Cid {
	actr, err := td.State().Actor(addr)
	require.NoError(td.T, err)
	return actr.Head()
}

// AssertBalance checks an actor has an expected balance.
func (td *TestDriver) AssertBalance(addr address.Address, expected abi_spec.TokenAmount) {
	actr, err := td.State().Actor(addr)
	require.NoError(td.T, err)
	assert.Equal(td.T, expected, actr.Balance(), fmt.Sprintf("expected actor %s balance: %s, actual balance: %s", addr, expected, actr.Balance()))
}

// Checks that after executing a message, the sender actor's balance is as expected, given
// - the actor's previous balance
// - the gas limit of the executed message
// - the gas price of the executed message
// - the value transferred by the executed message
func (td *TestDriver) AssertActorChange(addr address.Address, prevBalance abi_spec.TokenAmount, gasLimit int64, gasPrice big_spec.Int, transferred big_spec.Int, rct types.MessageReceipt, callSeqNum uint64) {
	actr, err := td.State().Actor(addr)
	require.NoError(td.T, err)

	expected := big_spec.Sub(prevBalance, td.CalcMessageCost(gasLimit, gasPrice, transferred, rct))

	assert.Equal(td.T, expected.String(), actr.Balance().String(), fmt.Sprintf("expected actor %s balance: %s, actual balance: %s", addr, expected, actr.Balance()))
	assert.Equal(td.T, callSeqNum, actr.CallSeqNum(), fmt.Sprintf("expected actor %s callSeqNum: %d, actual : %d", addr, callSeqNum, actr.CallSeqNum()))
}

func (td *TestDriver) AssertHead(addr address.Address, expected cid.Cid) {
	head := td.GetHead(addr)
	assert.Equal(td.T, expected, head, "expected actor %s head %s, actual %s", addr, expected, head)
}

func (td *TestDriver) AssertBalanceCallback(addr address.Address, thing func(actorBalance abi_spec.TokenAmount) bool) {
	actr, err := td.State().Actor(addr)
	require.NoError(td.T, err)
	assert.True(td.T, thing(actr.Balance()))
}

func (td *TestDriver) AssertMultisigTransaction(multisigAddr address.Address, txnID multisig_spec.TxnID, txn multisig_spec.Transaction) {
	var msState multisig_spec.State
	td.GetActorState(multisigAddr, &msState)

	txnMap, err := adt_spec.AsMap(AsStore(td.State()), msState.PendingTxns)
	require.NoError(td.T, err)

	var actualTxn multisig_spec.Transaction
	found, err := txnMap.Get(txnID, &actualTxn)
	require.NoError(td.T, err)
	require.True(td.T, found)

	assert.Equal(td.T, txn, actualTxn)
}

func (td *TestDriver) AssertMultisigContainsTransaction(multisigAddr address.Address, txnID multisig_spec.TxnID, contains bool) {
	var msState multisig_spec.State
	td.GetActorState(multisigAddr, &msState)

	txnMap, err := adt_spec.AsMap(AsStore(td.State()), msState.PendingTxns)
	require.NoError(td.T, err)

	var actualTxn multisig_spec.Transaction
	found, err := txnMap.Get(txnID, &actualTxn)
	require.NoError(td.T, err)

	assert.Equal(td.T, contains, found)

}

func (td *TestDriver) AssertMultisigState(multisigAddr address.Address, expected multisig_spec.State) {
	var msState multisig_spec.State
	td.GetActorState(multisigAddr, &msState)
	assert.NotNil(td.T, msState)
	assert.Equal(td.T, expected.InitialBalance, msState.InitialBalance, fmt.Sprintf("expected InitialBalance: %v, actual InitialBalance: %v", expected.InitialBalance, msState.InitialBalance))
	assert.Equal(td.T, expected.NextTxnID, msState.NextTxnID, fmt.Sprintf("expected NextTxnID: %v, actual NextTxnID: %v", expected.NextTxnID, msState.NextTxnID))
	assert.Equal(td.T, expected.NumApprovalsThreshold, msState.NumApprovalsThreshold, fmt.Sprintf("expected NumApprovalsThreshold: %v, actual NumApprovalsThreshold: %v", expected.NumApprovalsThreshold, msState.NumApprovalsThreshold))
	assert.Equal(td.T, expected.StartEpoch, msState.StartEpoch, fmt.Sprintf("expected StartEpoch: %v, actual StartEpoch: %v", expected.StartEpoch, msState.StartEpoch))
	assert.Equal(td.T, expected.UnlockDuration, msState.UnlockDuration, fmt.Sprintf("expected UnlockDuration: %v, actual UnlockDuration: %v", expected.UnlockDuration, msState.UnlockDuration))

	for _, e := range expected.Signers {
		assert.Contains(td.T, msState.Signers, e, fmt.Sprintf("expected Signer: %v, actual Signer: %v", e, msState.Signers))
	}
}

func (td *TestDriver) ComputeInitActorExecReturn(from address.Address, originatorCallSeq uint64, newActorAddressCount uint64, expectedNewAddr address.Address) init_spec.ExecReturn {
	td.T.Helper()
	return computeInitActorExecReturn(td.T, from, originatorCallSeq, newActorAddressCount, expectedNewAddr)
}

func computeInitActorExecReturn(t testing.TB, from address.Address, originatorCallSeq uint64, newActorAddressCount uint64, expectedNewAddr address.Address) init_spec.ExecReturn {
	t.Helper()
	buf := new(bytes.Buffer)
	if from.Protocol() == address.ID {
		t.Fatal("cannot compute init actor address return from ID address", from)
	}

	require.NoError(t, from.MarshalCBOR(buf))
	require.NoError(t, binary.Write(buf, binary.BigEndian, originatorCallSeq))
	require.NoError(t, binary.Write(buf, binary.BigEndian, newActorAddressCount))

	out, err := address.NewActorAddress(buf.Bytes())
	require.NoError(t, err)

	return init_spec.ExecReturn{
		IDAddress:     expectedNewAddr,
		RobustAddress: out,
	}
}

func (td *TestDriver) MustCreateAndVerifyMultisigActor(nonce uint64, value abi_spec.TokenAmount, multisigAddr address.Address, from address.Address, params *multisig_spec.ConstructorParams, code exitcode.ExitCode, retval []byte) {
	/* Create the Multisig actor*/
	td.applyMessageExpectCodeAndReturn(
		td.MessageProducer.CreateMultisigActor(from, params.Signers, params.UnlockDuration, params.NumApprovalsThreshold, chain.Nonce(nonce), chain.Value(value)),
		code, retval)
	/* Assert the actor state was setup as expected */
	pendingTxMapRoot, err := adt_spec.MakeEmptyMap(newMockStore()).Root()
	require.NoError(td.T, err)
	initialBalance := big_spec.Zero()
	startEpoch := abi_spec.ChainEpoch(0)
	if params.UnlockDuration > 0 {
		initialBalance = value
		startEpoch = td.ExeCtx.Epoch
	}
	td.AssertMultisigState(multisigAddr, multisig_spec.State{
		NextTxnID:      0,
		InitialBalance: initialBalance,
		StartEpoch:     startEpoch,

		Signers:               params.Signers,
		UnlockDuration:        params.UnlockDuration,
		NumApprovalsThreshold: params.NumApprovalsThreshold,

		PendingTxns: pendingTxMapRoot,
	})
	td.AssertBalance(multisigAddr, value)
}

type RewardSummary struct {
	Treasury           abi_spec.TokenAmount
	SimpleSupply       abi_spec.TokenAmount
	BaselineSupply     abi_spec.TokenAmount
	NextPerEpochReward abi_spec.TokenAmount
	NextPerBlockReward abi_spec.TokenAmount
}

func (td *TestDriver) GetRewardSummary() *RewardSummary {
	var rst reward_spec.State
	td.GetActorState(builtin_spec.RewardActorAddr, &rst)

	return &RewardSummary{
		Treasury:           td.GetBalance(builtin_spec.RewardActorAddr),
		NextPerEpochReward: rst.ThisEpochReward,
		NextPerBlockReward: big_spec.Div(rst.ThisEpochReward, big_spec.NewInt(builtin_spec.ExpectedLeadersPerEpoch)),
	}
}
