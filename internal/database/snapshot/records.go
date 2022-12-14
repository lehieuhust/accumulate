package snapshot

import (
	"gitlab.com/accumulatenetwork/accumulate/internal/database"
	"gitlab.com/accumulatenetwork/accumulate/internal/errors"
	"gitlab.com/accumulatenetwork/accumulate/smt/managed"
)

// TODO: Check for existing records when restoring?

func CollectSignature(record *database.Transaction) (*Signature, error) {
	state, err := record.Main().Get()
	if err != nil {
		return nil, errors.Wrap(errors.StatusUnknownError, err)
	}
	if state.Signature == nil || state.Transaction != nil {
		return nil, errors.Format(errors.StatusBadRequest, "signature is not a signature")
	}

	sig := new(Signature)
	sig.Signature = state.Signature
	sig.Txid = state.Txid
	return sig, nil
}

func (s *Signature) Restore(batch *database.Batch) error {
	err := batch.Transaction(s.Signature.Hash()).Main().Put(&database.SigOrTxn{Signature: s.Signature, Txid: s.Txid})
	return errors.Wrap(errors.StatusUnknownError, err)
}

func CollectTransaction(record *database.Transaction) (*Transaction, error) {
	state, err := record.Main().Get()
	if err != nil {
		return nil, errors.Wrap(errors.StatusUnknownError, err)
	}
	if state.Transaction == nil || state.Signature != nil {
		return nil, errors.Format(errors.StatusBadRequest, "transaction is not a transaction")
	}

	txn := new(Transaction)
	txn.Transaction = state.Transaction
	txn.Status = loadState(&err, true, record.Status().Get)

	if err != nil {
		return nil, errors.Wrap(errors.StatusUnknownError, err)
	}

	for _, signer := range txn.Status.Signers {
		record, err := record.ReadSignaturesForSigner(signer)
		if err != nil {
			return nil, errors.Format(errors.StatusUnknownError, "load %v signature set: %w", signer.GetUrl(), err)
		}

		set := new(TxnSigSet)
		set.Signer = signer.GetUrl()
		set.Version = record.Version()
		set.Entries = record.Entries()
		txn.SignatureSets = append(txn.SignatureSets, set)
	}

	return txn, nil
}

func (t *Transaction) Restore(batch *database.Batch) error {
	var err error
	record := batch.Transaction(t.Transaction.GetHash())
	saveState(&err, record.Main().Put, &database.SigOrTxn{Transaction: t.Transaction})
	saveState(&err, record.Status().Put, t.Status)

	if err != nil {
		return errors.Wrap(errors.StatusUnknownError, err)
	}

	for _, set := range t.SignatureSets {
		err = record.RestoreSignatureSets(set.Signer, set.Version, set.Entries)
		if err != nil {
			return errors.Wrap(errors.StatusUnknownError, err)
		}
	}

	return nil
}

func CollectAccount(record *database.Account, fullChainHistory bool) (*Account, error) {
	var err error
	acct := new(Account)
	acct.Url = record.Url()
	acct.Main = loadState(&err, true, record.Main().Get)
	acct.Pending = loadState(&err, true, record.Pending().Get)
	acct.Directory = loadState(&err, true, record.Directory().Get)

	if err != nil {
		return nil, errors.Wrap(errors.StatusUnknownError, err)
	}

	for _, meta := range loadState(&err, false, record.Chains().Get) {
		record, err := record.GetChainByName(meta.Name)
		if err != nil {
			return nil, errors.Format(errors.StatusUnknownError, "load %s chain state: %w", meta.Name, err)
		}

		chain := new(Chain)
		chain.Name = meta.Name
		chain.Type = meta.Type
		acct.Chains = append(acct.Chains, chain)

		state := record.CurrentState()
		if fullChainHistory {
			chain.Entries, err = record.Entries(0, state.Count)
			if err != nil {
				return nil, errors.Format(errors.StatusUnknownError, "load %s chain entries: %w", meta.Name, err)
			}
			continue
		}

		chain.Count = uint64(state.Count)
		chain.Pending = make([][]byte, len(state.Pending))
		for i, v := range state.Pending {
			if len(v) == 0 {
				continue
			}
			chain.Pending[i] = v
		}
	}

	return acct, nil
}

func (a *Account) Restore(batch *database.Batch) error {
	var err error
	record := batch.Account(a.Url)
	saveState(&err, record.Main().Put, a.Main)
	saveState(&err, record.Directory().Put, a.Directory)
	saveState(&err, record.Pending().Put, a.Pending)

	return errors.Wrap(errors.StatusUnknownError, err)
}

func (c *Chain) Restore(account *database.Account) error {
	mgr, err := account.GetChainByName(c.Name)
	if err != nil {
		return errors.Format(errors.StatusUnknownError, "store %s chain head: %w", c.Name, err)
	}
	err = mgr.RestoreHead(&managed.MerkleState{Count: int64(c.Count), Pending: c.Pending})
	if err != nil {
		return errors.Format(errors.StatusUnknownError, "store %s chain head: %w", c.Name, err)
	}
	for i, entry := range c.Entries {
		err := mgr.AddEntry(entry, false)
		if err != nil {
			return errors.Format(errors.StatusUnknownError, "store %s chain entry %d: %w", c.Name, c.Count+uint64(i), err)
		}
	}
	return nil
}

func zero[T any]() (z T) { return z }

func loadState[T any](lastErr *error, allowMissing bool, get func() (T, error)) T {
	if *lastErr != nil {
		return zero[T]()
	}

	v, err := get()
	if allowMissing && errors.Is(err, errors.StatusNotFound) {
		return zero[T]()
	}
	if err != nil {
		*lastErr = err
		return zero[T]()
	}

	return v
}

func saveState[T any](lastErr *error, put func(T) error, v T) {
	if *lastErr != nil || any(v) == nil {
		return
	}

	err := put(v)
	if err != nil {
		*lastErr = err
	}
}
