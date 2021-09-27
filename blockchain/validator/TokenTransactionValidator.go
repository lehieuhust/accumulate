package validator

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"math/big"
	"time"

	cfg "github.com/tendermint/tendermint/config"

	"github.com/AccumulateNetwork/accumulated/types"
	"github.com/AccumulateNetwork/accumulated/types/api/transactions"
	pb "github.com/AccumulateNetwork/accumulated/types/proto"
	"github.com/AccumulateNetwork/accumulated/types/state"
	"github.com/AccumulateNetwork/accumulated/types/synthetic"
)

type TokenTransactionValidator struct {
	ValidatorContext
}

//this token validator belings in both a Token (coinbase) and Token Account validator.
func NewTokenTransactionValidator() *TokenTransactionValidator {
	v := &TokenTransactionValidator{}
	v.SetInfo(types.ChainTypeToken, pb.AccInstruction_Token_Transaction)
	v.ValidatorContext.ValidatorInterface = v
	return v
}

// canTransact is a helper function to parse and check for errors in the transaction data
func canSendTokens(currentState *state.StateEntry, tas *state.TokenAccount, withdrawal *transactions.TokenSend) error {

	if currentState.ChainState == nil {
		return fmt.Errorf("no account exists for the chain")
	}
	if currentState.IdentityState == nil {
		return fmt.Errorf("no identity exists for the chain")
	}

	if withdrawal == nil {
		//defensive check / shouldn't get where.
		return fmt.Errorf("withdrawl account doesn't exist")
	}

	//verify the tx.from is from the same chain
	fromChainId := types.GetChainIdFromChainPath(&withdrawal.AccountURL)
	if bytes.Compare(currentState.ChainId[:], fromChainId[:]) != 0 {
		return fmt.Errorf("from state object transaction account doesn't match transaction")
	}

	//now check to see if we can transact
	//really only need to provide one input...
	amt := types.Amount{}
	var outAmt big.Int
	for _, val := range withdrawal.Outputs {
		amt.Add(amt.AsBigInt(), outAmt.SetUint64(val.Amount))
	}

	//make sure the user has enough in the account to perform the transaction
	if tas.GetBalance().Cmp(amt.AsBigInt()) < 0 {
		///insufficient balance
		return fmt.Errorf("insufficient balance")
	}
	return nil
}

func (v *TokenTransactionValidator) Initialize(config *cfg.Config, db *state.StateDB) error {
	v.db = db
	return nil
}

// Check will perform a sanity check to make sure transaction seems reasonable
func (v *TokenTransactionValidator) Check(currentState *state.StateEntry, submission *transactions.GenTransaction) error {

	if currentState.IdentityState == nil {
		return fmt.Errorf("identity state does not exist for anonymous transaction")
	}

	withdrawal := transactions.TokenSend{} //api.TokenTx{}
	_, err := withdrawal.Unmarshal(submission.Transaction)
	if err != nil {
		return fmt.Errorf("error with send token in TokenTransactionValidator.Check, %v", err)
	}

	tas := &state.TokenAccount{}
	err = tas.UnmarshalBinary(currentState.ChainState.Entry)
	if err != nil {
		return fmt.Errorf("cannot unmarshal token account, %v", err)
	}
	err = canSendTokens(currentState, tas, &withdrawal)
	return err
}

// BeginBlock Sets time and height information for beginning of block
func (v *TokenTransactionValidator) BeginBlock(height int64, time *time.Time) error {
	v.lastHeight = v.currentHeight
	v.lastTime = v.currentTime
	v.currentHeight = height
	v.currentTime = *time

	return nil
}

// Validate validates a token transaction
func (v *TokenTransactionValidator) Validate(currentState *state.StateEntry, submission *transactions.GenTransaction) (*ResponseValidateTX, error) {

	//need to do everything done in "check" and also create a synthetic transaction to add tokens.
	withdrawal := transactions.TokenSend{} //api.TokenTx{}
	_, err := withdrawal.Unmarshal(submission.Transaction)
	if err != nil {
		return nil, fmt.Errorf("error with send token in TokenTransactionValidator.Validate, %v", err)
	}

	ids := &state.AdiState{}
	err = ids.UnmarshalBinary(currentState.IdentityState.Entry)
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling adi")
	}

	tas := &state.TokenAccount{}
	err = tas.UnmarshalBinary(currentState.IdentityState.Entry)
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling token account")
	}

	err = canSendTokens(currentState, tas, &withdrawal)

	if ids == nil {
		return nil, fmt.Errorf("invalid identity state retrieved for token transaction")
	}

	if err != nil {
		return nil, err
	}

	keyHash := sha256.Sum256(submission.Signature[0].PublicKey)
	if !ids.VerifyKey(keyHash[:]) {
		return nil, fmt.Errorf("key not authorized for signing transaction")
	}

	//need to do a nonce check.
	if submission.SigInfo.Nonce <= ids.Nonce {
		return nil, fmt.Errorf("invalid nonce in transaction, cannot proceed")
	}

	txid := submission.TransactionHash()

	ret := ResponseValidateTX{}
	ret.Submissions = make([]*transactions.GenTransaction, len(withdrawal.Outputs)+1)

	txAmt := big.NewInt(0)
	var amt big.Int
	for i, val := range withdrawal.Outputs {
		amt.SetUint64(val.Amount)

		//accumulate the total amount of the transaction
		txAmt.Add(txAmt, &amt)

		//extract the target identity and chain from the url
		adi, chainPath, err := types.ParseIdentityChainPath(&val.Dest)
		if err != nil {
			return nil, err
		}
		destUrl := types.String(chainPath)

		//get the identity id from the adi
		idChain := types.GetIdentityChainFromIdentity(&adi)
		if idChain == nil {
			return nil, fmt.Errorf("Invalid identity chain for %s", adi)
		}

		//populate the synthetic transaction, each submission will be signed by BVC leader and dispatched
		sub := transactions.GenTransaction{}
		ret.Submissions[i] = &sub

		//set the identity chain for the destination
		sub.Routing = types.GetAddressFromIdentity(&adi)

		//set the chain id for the destination
		sub.ChainID = types.GetChainIdFromChainPath(&chainPath).Bytes()

		//set the transaction instruction type to a synthetic token deposit
		depositTx := synthetic.NewTokenTransactionDeposit(txid[:], &tas.ChainUrl, &destUrl)
		err = depositTx.SetDeposit(&tas.TokenUrl.String, &amt)
		if err != nil {
			return nil, fmt.Errorf("unable to set deposit for synthetic token deposit transaction")
		}

		sub.Transaction, err = depositTx.MarshalBinary()
		if err != nil {
			return nil, fmt.Errorf("unable to set sender info for synthetic token deposit transaction")
		}
	}

	//subtract the transfer amount from the balance
	err = tas.SubBalance(txAmt)
	if err != nil {
		return nil, err
	}

	//issue a state change...
	tasso, err := tas.MarshalBinary()

	if err != nil {
		return nil, err
	}

	//return a transaction state object
	ret.AddStateData(currentState.ChainId, tasso)

	return &ret, nil
}

// EndBlock
func (v *TokenTransactionValidator) EndBlock(mdRoot []byte) error {
	_ = mdRoot
	return nil
}