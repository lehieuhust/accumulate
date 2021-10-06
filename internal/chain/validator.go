package chain

import (
	"fmt"

	"github.com/AccumulateNetwork/accumulated/types"

	"github.com/AccumulateNetwork/accumulated/types/api/transactions"
	"github.com/AccumulateNetwork/accumulated/types/state"
)

func NewBlockValidator() *validator {
	// We will be moving towards the account chain validator for token
	// transactions and eventually data tx
	b := new(validator)
	b.add(ADI{})
	b.add(new(AnonToken))
	b.add(SynIdentityCreate{})
	b.add(TokenIssuance{})
	b.add(TokenChainCreate{})
	b.add(TokenTx{})
	return b
}

type validator struct {
	chainCreate map[types.TxType]Chain
	chainUpdate map[types.ChainType]Chain
}

var _ Validator = (*validator)(nil)

type createChain interface {
	createChain() types.TxType
	Chain
}

type updateChain interface {
	updateChain() types.ChainType
	Chain
}

func (v *validator) add(chain Chain) {
	if v.chainCreate == nil {
		v.chainCreate = map[types.TxType]Chain{}
		v.chainUpdate = map[types.ChainType]Chain{}
	}

	var used bool
	if chain, ok := chain.(createChain); ok {
		if _, ok := v.chainCreate[chain.createChain()]; ok {
			panic(fmt.Errorf("duplicate  create chain for %d", chain.createChain()))
		}
		v.chainCreate[chain.createChain()], used = chain, true
	}

	if chain, ok := chain.(updateChain); ok {
		if _, ok := v.chainUpdate[chain.updateChain()]; ok {
			panic(fmt.Errorf("duplicate identity create chain for %d", chain.updateChain()))
		}
		v.chainUpdate[chain.updateChain()], used = chain, true
	}

	if !used {
		panic(fmt.Errorf("unsupported chain type %T", chain))
	}
}

// BeginBlock will set block parameters
func (v *validator) BeginBlock() {
	for _, c := range v.chainCreate {
		c.BeginBlock()
	}
	for _, c := range v.chainUpdate {
		c.BeginBlock()
	}
}

func (v *validator) CheckTx(st *state.StateEntry, tx *transactions.GenTransaction) error {
	// TODO shouldn't this be checking the subchains?
	return nil
}

func (v *validator) DeliverTx(st *state.StateEntry, tx *transactions.GenTransaction) (*DeliverTxResult, error) {
	txType := types.TxType(tx.TransactionType())

	if err := tx.SetRoutingChainID(); err != nil {
		return nil, err
	}

	// No chain state exists for tx.ChainID
	if st.ChainHeader == nil {
		chain := v.chainCreate[txType]
		if chain == nil {
			return nil, fmt.Errorf("cannot create identity: unsupported TX type: %d", txType)
		}
		return chain.DeliverTx(st, tx)
	}

	// Chain and its ADI both exist
	chain := v.chainUpdate[st.ChainHeader.Type]
	if chain == nil {
		return nil, fmt.Errorf("cannot update chain: unsupported chain type: %d", st.ChainHeader.Type)
	}
	return chain.DeliverTx(st, tx)
}

func (*validator) Commit() {}