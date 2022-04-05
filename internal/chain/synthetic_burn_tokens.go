package chain

import (
	"fmt"

	"gitlab.com/accumulatenetwork/accumulate/protocol"
)

type SyntheticBurnTokens struct{}

func (SyntheticBurnTokens) Type() protocol.TransactionType {
	return protocol.TransactionTypeSyntheticBurnTokens
}

func (SyntheticBurnTokens) Validate(st *StateManager, tx *protocol.Envelope) (protocol.TransactionResult, error) {
	body, ok := tx.Transaction.Body.(*protocol.SyntheticBurnTokens)
	if !ok {
		return nil, fmt.Errorf("invalid payload: want %T, got %T", new(protocol.SyntheticBurnTokens), tx.Transaction.Body)
	}

	account, ok := st.Origin.(*protocol.TokenIssuer)
	if !ok {
		return nil, fmt.Errorf("invalid origin record: want chain type %v, got %v", protocol.AccountTypeTokenIssuer, st.Origin.Type())
	}

	account.Issued.Sub(&account.Issued, &body.Amount)

	st.Update(account)
	return nil, nil
}
