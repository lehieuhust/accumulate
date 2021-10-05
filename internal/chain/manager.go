package chain

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/AccumulateNetwork/accumulated/internal/abci"
	accapi "github.com/AccumulateNetwork/accumulated/internal/api"
	"github.com/AccumulateNetwork/accumulated/smt/common"
	"github.com/AccumulateNetwork/accumulated/types"
	"github.com/AccumulateNetwork/accumulated/types/api"
	"github.com/AccumulateNetwork/accumulated/types/api/transactions"
	"github.com/AccumulateNetwork/accumulated/types/state"
)

const chainWGSize = 4

type Manager struct {
	db    *state.StateDB
	chain Validator
	key   ed25519.PrivateKey
	query *accapi.Query

	wg      *sync.WaitGroup
	mu      *sync.Mutex
	chainWG map[uint64]*sync.WaitGroup
	leader  bool
	height  int64
}

type Validator interface {
	// BeginBlock marks the beginning of a block.
	BeginBlock()

	// CheckTx partially validates the transaction.
	CheckTx(*state.StateEntry, *transactions.GenTransaction) error

	// DeliverTx fully validates the transaction.
	DeliverTx(*state.StateEntry, *transactions.GenTransaction) (*DeliverTxResult, error)

	// Commit commits the block.
	Commit()
}

var _ abci.Chain = (*Manager)(nil)

func NewManager(query *accapi.Query, db *state.StateDB, key ed25519.PrivateKey, chain Validator) (*Manager, error) {
	m := new(Manager)
	m.db = db
	m.chain = chain
	m.key = key
	m.wg = new(sync.WaitGroup)
	m.mu = new(sync.Mutex)
	m.query = query

	fmt.Printf("Loaded height=%d hash=%X\n", db.BlockIndex(), db.EnsureRootHash())
	return m, nil
}

func (m *Manager) Query(q *api.Query) ([]byte, error) {
	if q.Content != nil {
		// TODO handle error
		tx, pendingTx, _ := m.db.GetTx(q.Content)
		return append(common.SliceBytes(tx), common.SliceBytes(pendingTx)...), nil
	}

	chainState, err := m.db.GetCurrentEntry(q.ChainId)
	if err != nil {
		return nil, fmt.Errorf("failed to locate chain entry: %v", err)
	}

	err = chainState.As(new(state.Chain))
	if err != nil {
		return nil, fmt.Errorf("unable to extract chain header: %v", err)
	}

	return chainState.Entry, nil
}

// BeginBlock implements ./abci.Chain
func (m *Manager) BeginBlock(req abci.BeginBlockRequest) {
	m.leader = req.IsLeader
	m.height = req.Height
	m.chainWG = make(map[uint64]*sync.WaitGroup, chainWGSize)
	m.chain.BeginBlock()
}

// CheckTx implements ./abci.Chain
func (m *Manager) CheckTx(tx *transactions.GenTransaction) error {
	err := tx.SetRoutingChainID()
	if err != nil {
		return err
	}

	m.mu.Lock()
	st, err := m.db.LoadChainAndADI(tx.ChainID)
	m.mu.Unlock()
	if err != nil {
		return fmt.Errorf("failed to get state: %v", err)
	}

	err = m.isSane(st, tx)
	if err != nil {
		return err
	}

	return m.chain.CheckTx(st, tx)
}

// DeliverTx implements ./abci.Chain
func (m *Manager) DeliverTx(tx *transactions.GenTransaction) error {
	m.wg.Add(1)

	// If this is done async (`go m.deliverTxAsync(tx)`), how would an error
	// get back to the ABCI callback?
	// go func() {

	defer m.wg.Done()

	if tx.Transaction == nil || tx.SigInfo == nil || len(tx.ChainID) != 32 {
		return fmt.Errorf("malformed transaction")
	}

	tx.TransactionHash()

	m.mu.Lock()
	group, ok := m.chainWG[tx.Routing%chainWGSize]
	if !ok {
		group = new(sync.WaitGroup)
		m.chainWG[tx.Routing%chainWGSize] = group
	}

	group.Wait()
	group.Add(1)
	defer group.Done()
	m.mu.Unlock()

	st, err := m.db.LoadChainAndADI(tx.ChainID)
	if err != nil {
		return fmt.Errorf("failed to get state: %v", err)
	}

	// //placeholder for special validation rules for synthetic transactions.
	// if tx.TransactionType()&0xF0 > 0 {
	// 	//need to verify the sender is a legit bvc validator also need the dbvc receipt
	// 	//so if the transaction is a synth tx, then we need to verify the sender is a BVC validator and
	// 	//not an impostor. Need to figure out how to do this. Right now we just assume the synth request
	// 	//sender is legit.
	// }

	err = m.isSane(st, tx)
	if err != nil {
		return err
	}

	// First configure the pending state which is the basis for the transaction
	txPending := state.NewPendingTransaction(tx)

	// Validate
	// TODO txValidated should return a list of chainId's the transaction touched.
	txValidated, err := m.chain.DeliverTx(st, tx)
	if err != nil {
		return fmt.Errorf("rejected by chain: %v", err)
	}

	// Check if the transaction was accepted
	var txAccepted *state.Transaction
	if err == nil {
		// If we get here, we were successful in validating.  So, we need to
		// split the transaction in 2, the body (i.e. TxAccepted), and the
		// validation material (i.e. TxPending).  The body of the transaction
		// gets put on the main chain, and the validation material gets put on
		// the pending chain which is purged after about 2 weeks
		txAccepted, txPending = state.NewTransaction(txPending)
	}

	// Store the tx state
	var chainId types.Bytes32
	copy(chainId[:], tx.ChainID)
	err = m.db.AddPendingTx(&chainId, txPending, txAccepted)
	if err != nil {
		return err
	}

	if txValidated == nil {
		return errors.New("no chain validation")
	}

	// Batch synthetic transactions generated by the validator
	if m.leader {
		err = m.submitSyntheticTx(txValidated)
		if err != nil {
			return err
		}
	}

	return nil
}

// BeginBlock implements ./abci.Chain
func (m *Manager) EndBlock(req abci.EndBlockRequest) {}

// Commit implements ./abci.Chain
func (m *Manager) Commit() ([]byte, error) {
	m.wg.Wait()
	m.chain.Commit()

	mdRoot, numStateChanges, err := m.db.WriteStates(m.height)
	if err != nil {
		// This should never happen
		panic(fmt.Errorf("fatal error, block not set, %v", err))
	}

	// If we have no transactions this block then don't publish anything
	if m.leader && numStateChanges > 0 {
		// Now we create a synthetic transaction and publish to the directory
		// block validator
		dbvc := DeliverTxResult{}
		dbvc.Submissions = make([]*transactions.GenTransaction, 1)
		dbvc.Submissions[0] = &transactions.GenTransaction{}
		dcAdi := "dc"
		dbvc.Submissions[0].ChainID = types.GetChainIdFromChainPath(&dcAdi).Bytes()
		dbvc.Submissions[0].Routing = types.GetAddressFromIdentity(&dcAdi)
		//dbvc.Submissions[0].Transaction = ...

		//broadcast the root
		//m.processValidatedSubmissionRequest(&dbvc)
	}

	m.query.BatchSend()

	fmt.Printf("DB time %f\n", m.db.TimeBucket)
	m.db.TimeBucket = 0
	return mdRoot, nil
}

// isSane will check the nonce (if applicable), check the public key for the transaction,
// and verify the signature of the transaction.
func (m *Manager) isSane(st *state.StateEntry, tx *transactions.GenTransaction) error {
	// if st.IsValid(state.MaskChainState | state.MaskAdiState) {
	// 	//1. verify nonce
	// 	//2. verify public key against state
	// }

	if !tx.ValidateSig() {
		return fmt.Errorf("transaction %d has an invalid signature", tx.TransactionType())
	}
	return nil
}

func (m *Manager) submitSyntheticTx(vtx *DeliverTxResult) error {
	// Need to pass this to a threaded batcher / dispatcher to do both signing
	// and sending of synth tx. No need to spend valuable time here doing that.
	for _, tx := range vtx.Submissions {
		// Generate a synthetic tx and send to the router. Need to track txid to
		// make sure they get processed.

		// We may want to reconsider making this a go call since using grpc
		// could delay things considerably. We only need to make sure it is
		// processed by the next EndBlock so place in pending queue. If we are
		// the leader then we are responsible for dispatching the synth tx.

		if tx.Transaction == nil {
			// This should never happen
			panic("submission is missing its synthetic transaction")
		}

		if tx.SigInfo == nil {
			// This should never happen
			panic("synthetic transaction is missing its signature info")
		}

		ed := new(transactions.ED25519Sig)
		tx.SigInfo.Nonce = uint64(time.Now().Unix())
		ed.PublicKey = m.key[32:]
		err := ed.Sign(tx.SigInfo.Nonce, m.key, tx.TransactionHash())
		if err != nil {
			// TODO why is this a panic?
			panic(err)
		}

		tx.Signature = append(tx.Signature, ed)
		_, err = m.query.BroadcastTx(tx)
		if err != nil {
			return err
		}
	}

	return nil
}
