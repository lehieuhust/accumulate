package snapshot

import (
	"compress/gzip"
	"io"

	"gitlab.com/accumulatenetwork/accumulate/internal/database"
	"gitlab.com/accumulatenetwork/accumulate/internal/errors"
	"gitlab.com/accumulatenetwork/accumulate/pkg/url"
	"gitlab.com/accumulatenetwork/accumulate/protocol"
)

func Collect(batch *database.Batch, file io.WriteSeeker, preserveAccountHistory func(account *database.Account) (bool, error)) (*Writer, error) {
	w, err := Create(file, new(Header))
	if err != nil {
		return nil, errors.Wrap(errors.StatusUnknownError, err)
	}

	// Restoring accounts will fail if they reference transactions that have not
	// yet been restored, so the transaction section must come first. However we
	// need to scan the BPT in order to know what transactions need to be saved.
	var accounts []*url.URL
	txnHashes := new(HashSet)
	err = batch.VisitAccounts(func(record *database.Account) error {
		accounts = append(accounts, record.Url())
		pending, err := record.Pending().Get()
		if err != nil {
			return errors.Format(errors.StatusUnknownError, "load pending: %w", err)
		}
		for _, txid := range pending {
			txnHashes.Add(txid.Hash())
		}
		return nil
	})
	if err != nil {
		return nil, errors.Wrap(errors.StatusUnknownError, err)
	}

	// Collect transaction and signature hashes
	sigHashes := new(HashSet)
	for _, u := range accounts {
		record := batch.Account(u)
		preserve, err := preserveAccountHistory(record)
		if err != nil {
			return nil, errors.Wrap(errors.StatusUnknownError, err)
		}
		if !preserve {
			continue
		}

		err = txnHashes.CollectFromChain(record, record.MainChain())
		if err != nil {
			return nil, errors.Wrap(errors.StatusUnknownError, err)
		}

		err = txnHashes.CollectFromChain(record, record.ScratchChain())
		if err != nil {
			return nil, errors.Wrap(errors.StatusUnknownError, err)
		}

		err = sigHashes.CollectFromChain(record, record.SignatureChain())
		if err != nil {
			return nil, errors.Wrap(errors.StatusUnknownError, err)
		}
	}

	// Save transactions
	err = w.CollectTransactions(batch, txnHashes.Hashes, func(t *Transaction) error {
		for _, set := range t.SignatureSets {
			for _, entry := range set.Entries {
				sigHashes.Add(entry.SignatureHash)
			}
		}
		return nil
	})
	if err != nil {
		return nil, errors.Wrap(errors.StatusUnknownError, err)
	}

	// Save signatures
	err = w.CollectSignatures(batch, sigHashes.Hashes, nil)
	if err != nil {
		return nil, errors.Wrap(errors.StatusUnknownError, err)
	}

	// Save accounts
	err = w.CollectAccounts(batch, preserveAccountHistory, nil)
	if err != nil {
		return nil, errors.Wrap(errors.StatusUnknownError, err)
	}

	return w, nil
}

func (w *Writer) CollectAccounts(batch *database.Batch, preserveHistory func(account *database.Account) (bool, error), visit func(*Account) error) error {
	sw, err := w.Open(SectionTypeAccounts)
	if err != nil {
		return errors.Format(errors.StatusUnknownError, "open accounts section: %w", err)
	}

	err = batch.SaveAccounts(sw, func(record *database.Account) ([]byte, error) {
		preserve, err := preserveHistory(record)
		if err != nil {
			return nil, errors.Wrap(errors.StatusUnknownError, err)
		}

		// Always preserve chains for now
		acct, err := CollectAccount(record, preserve)
		if err != nil {
			return nil, errors.Format(errors.StatusUnknownError, "collect account: %w", err)
		}

		if visit != nil {
			err = visit(acct)
			if err != nil {
				return nil, errors.Format(errors.StatusUnknownError, "visit account: %w", err)
			}
		}

		b, err := acct.MarshalBinary()
		if err != nil {
			return nil, errors.Format(errors.StatusEncodingError, "marshal account: %w", err)
		}
		return b, nil
	})
	if err != nil {
		return errors.Wrap(errors.StatusUnknownError, err)
	}

	err = sw.Close()
	return errors.Wrap(errors.StatusUnknownError, err)
}

func (w *Writer) CollectTransactions(batch *database.Batch, hashes [][32]byte, visit func(*Transaction) error) error {
	var txns []*Transaction
	for _, h := range hashes {
		h := h // See docs/developer/rangevarref.md
		txn, err := CollectTransaction(batch.Transaction(h[:]))
		if err != nil {
			return errors.Format(errors.StatusUnknownError, "collect transaction %x: %w", h[:4], err)
		}

		if visit != nil {
			err = visit(txn)
			if err != nil {
				return errors.Format(errors.StatusUnknownError, "visit transaction: %w", err)
			}
		}

		txns = append(txns, txn)
	}

	return w.WriteTransactions(txns, false)
}

func (w *Writer) WriteTransactions(txns []*Transaction, gz bool) error {
	if len(txns) == 0 {
		return nil
	}

	b, err := (&txnSection{Transactions: txns}).MarshalBinary()
	if err != nil {
		return errors.Format(errors.StatusUnknownError, "marshal transactions: %w", err)
	}

	typ := SectionTypeTransactions
	if gz {
		typ = SectionTypeGzTransactions
	}
	sw, err := w.Open(typ)
	if err != nil {
		return errors.Format(errors.StatusUnknownError, "open transactions section: %w", err)
	}

	if gz {
		w := gzip.NewWriter(sw)
		_, err = w.Write(b)
		if err != nil {
			return errors.Format(errors.StatusUnknownError, "write transactions: %w", err)
		}
		err = w.Close()
		if err != nil {
			return errors.Format(errors.StatusUnknownError, "close gzip writer: %w", err)
		}
	} else {
		_, err = sw.Write(b)
		if err != nil {
			return errors.Format(errors.StatusUnknownError, "write transactions: %w", err)
		}
	}

	err = sw.Close()
	return errors.Wrap(errors.StatusUnknownError, err)
}

func (w *Writer) CollectSignatures(batch *database.Batch, hashes [][32]byte, visit func(*Signature) error) error {
	var sigs []*Signature
	for _, h := range hashes {
		h := h // See docs/developer/rangevarref.md
		sig, err := CollectSignature(batch.Transaction(h[:]))
		if err != nil {
			return errors.Format(errors.StatusUnknownError, "collect signature %x: %w", h[:4], err)
		}

		if visit != nil {
			err = visit(sig)
			if err != nil {
				return errors.Format(errors.StatusUnknownError, "visit signature: %w", err)
			}
		}

		sigs = append(sigs, sig)
	}

	if len(sigs) == 0 {
		return nil
	}

	b, err := (&sigSection{Signatures: sigs}).MarshalBinary()
	if err != nil {
		return errors.Format(errors.StatusUnknownError, "marshal signatures: %w", err)
	}

	sw, err := w.Open(SectionTypeSignatures)
	if err != nil {
		return errors.Format(errors.StatusUnknownError, "open signatures section: %w", err)
	}

	_, err = sw.Write(b)
	if err != nil {
		return errors.Format(errors.StatusUnknownError, "write signatures: %w", err)
	}

	err = sw.Close()
	return errors.Wrap(errors.StatusUnknownError, err)
}

type HashSet struct {
	seen   map[[32]byte]bool
	Hashes [][32]byte
}

func (s *HashSet) Add(h [32]byte) {
	if s.seen == nil {
		s.seen = map[[32]byte]bool{}
	}
	if s.seen[h] {
		return
	}
	s.seen[h] = true
	s.Hashes = append(s.Hashes, h)
}

func (s *HashSet) CollectFromChain(a *database.Account, c *database.Chain2) error {
	// Calling c.Get() modifies the account, so don't do that if the chain
	// doesn't exist
	_, err := a.Chains().Find(&protocol.ChainMetadata{Name: c.Name(), Type: c.Type()})
	switch {
	case err == nil:
		// Found
	case errors.Is(err, errors.StatusNotFound):
		return nil
	default:
		return errors.Format(errors.StatusUnknownError, "load chains index: %w", err)
	}

	chain, err := c.Get()
	if err != nil {
		return errors.Format(errors.StatusUnknownError, "load chain %s head: %w", c.Name(), err)
	}
	entries, err := chain.Entries(0, chain.Height())
	if err != nil {
		return errors.Format(errors.StatusUnknownError, "load chain %s entries: %w", c.Name(), err)
	}
	for _, h := range entries {
		s.Add(*(*[32]byte)(h))
	}
	return nil
}
