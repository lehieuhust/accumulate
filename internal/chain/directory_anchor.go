package chain

import (
	"bytes"
	"fmt"
	"sort"

	"gitlab.com/accumulatenetwork/accumulate/config"
	"gitlab.com/accumulatenetwork/accumulate/internal/database"
	"gitlab.com/accumulatenetwork/accumulate/internal/errors"
	"gitlab.com/accumulatenetwork/accumulate/internal/logging"
	"gitlab.com/accumulatenetwork/accumulate/pkg/url"
	"gitlab.com/accumulatenetwork/accumulate/protocol"
	"gitlab.com/accumulatenetwork/accumulate/smt/managed"
)

// Process the anchor from DN -> BVN

type DirectoryAnchor struct{}

func (DirectoryAnchor) Type() protocol.TransactionType {
	return protocol.TransactionTypeDirectoryAnchor
}

func (DirectoryAnchor) Execute(st *StateManager, tx *Delivery) (protocol.TransactionResult, error) {
	return (DirectoryAnchor{}).Validate(st, tx)
}

func (DirectoryAnchor) Validate(st *StateManager, tx *Delivery) (protocol.TransactionResult, error) {
	// Unpack the payload
	body, ok := tx.Transaction.Body.(*protocol.DirectoryAnchor)
	if !ok {
		return nil, fmt.Errorf("invalid payload: want %T, got %T", new(protocol.DirectoryAnchor), tx.Transaction.Body)
	}

	st.logger.Info("Received anchor", "module", "anchoring", "source", body.Source, "root", logging.AsHex(body.RootChainAnchor).Slice(0, 4), "bpt", logging.AsHex(body.StateTreeAnchor).Slice(0, 4), "block", body.MinorBlockIndex)

	// Verify the origin
	if _, ok := st.Origin.(*protocol.AnchorLedger); !ok {
		return nil, fmt.Errorf("invalid principal: want %v, got %v", protocol.AccountTypeAnchorLedger, st.Origin.Type())
	}

	// Verify the source URL is from the DN
	if !protocol.IsDnUrl(body.Source) {
		return nil, fmt.Errorf("invalid source: not the DN")
	}

	// Trigger a major block?
	if st.NetworkType != config.Directory {
		st.State.MakeMajorBlock = body.MakeMajorBlock
		st.State.MakeMajorBlockTime = body.MakeMajorBlockTime
	}

	// Add the anchor to the chain - use the partition name as the chain name
	record := st.batch.Account(st.OriginUrl).AnchorChain(protocol.Directory)
	index, err := st.State.ChainUpdates.AddChainEntry2(st.batch, record.Root(), body.RootChainAnchor[:], body.RootChainIndex, body.MinorBlockIndex, false)
	if err != nil {
		return nil, err
	}
	status, err := st.batch.Transaction(st.txHash[:]).Status().Get()
	if err != nil {
		return nil, err
	}
	st.State.DidReceiveAnchor(protocol.Directory, body, index, status)

	// And the BPT root
	_, err = st.State.ChainUpdates.AddChainEntry2(st.batch, record.BPT(), body.StateTreeAnchor[:], 0, 0, false)
	if err != nil {
		return nil, err
	}

	// Process updates when present
	if len(body.Updates) > 0 && st.NetworkType != config.Directory {
		err := processNetworkAccountUpdates(st, tx, body.Updates)
		if err != nil {
			return nil, err
		}
	}

	// Acknowledge anchors and synthetic transactions
	var anchorLedger *protocol.AnchorLedger
	err = st.batch.Account(st.AnchorPool()).Main().GetAs(&anchorLedger)
	if err != nil {
		return nil, fmt.Errorf("load anchor ledger: %w", err)
	}
	var synthLedger *protocol.SyntheticLedger
	err = st.batch.Account(st.Synthetic()).Main().GetAs(&synthLedger)
	if err != nil {
		return nil, fmt.Errorf("load anchor ledger: %w", err)
	}
	var didUpdateAnchor, didUpdateSynth bool
	for _, receipt := range body.Receipts {
		if st.PartitionUrl().Equal(receipt.Anchor.Source) {
			ledger := anchorLedger.Partition(body.Source)
			if receipt.SequenceNumber > ledger.Acknowledged {
				ledger.Acknowledged = receipt.SequenceNumber
				didUpdateAnchor = true
			}
		}

		anchorTxl := receipt.Anchor.SynthFrom(st.PartitionUrl().URL)
		localTxl := synthLedger.Partition(receipt.Anchor.Source)
		if anchorTxl.Delivered > localTxl.Acknowledged {
			localTxl.Acknowledged = anchorTxl.Delivered
			didUpdateSynth = true
		}
	}
	if didUpdateAnchor {
		err = st.batch.Account(st.AnchorPool()).Main().Put(anchorLedger)
		if err != nil {
			return nil, fmt.Errorf("store anchor ledger: %w", err)
		}
	}
	if didUpdateSynth {
		err = st.batch.Account(st.Synthetic()).Main().Put(synthLedger)
		if err != nil {
			return nil, fmt.Errorf("store anchor ledger: %w", err)
		}
	}

	if st.NetworkType != config.Directory {
		err = processReceiptsFromDirectory(st, tx, body)
		if err != nil {
			return nil, err
		}
	}

	return nil, nil
}

func processReceiptsFromDirectory(st *StateManager, tx *Delivery, body *protocol.DirectoryAnchor) error {
	var deliveries []*Delivery
	var sequence = map[*Delivery]int{}

	// Process pending transactions from the DN
	d, err := loadSynthTxns(st, tx, body.RootChainAnchor[:], body.Source, nil, sequence)
	if err != nil {
		return err
	}
	deliveries = append(deliveries, d...)

	// Process receipts
	for i, receipt := range body.Receipts {
		receipt := receipt // See docs/developer/rangevarref.md
		if !bytes.Equal(receipt.RootChainReceipt.Anchor, body.RootChainAnchor[:]) {
			return fmt.Errorf("receipt %d is invalid: result does not match the anchor", i)
		}

		st.logger.Info("Received receipt", "module", "anchoring", "from", logging.AsHex(receipt.RootChainReceipt.Start).Slice(0, 4), "to", logging.AsHex(body.RootChainAnchor).Slice(0, 4), "block", body.MinorBlockIndex, "source", body.Source)

		d, err := loadSynthTxns(st, tx, receipt.RootChainReceipt.Start, body.Source, receipt.RootChainReceipt, sequence)
		if err != nil {
			return err
		}
		deliveries = append(deliveries, d...)
	}

	// Submit the receipts, sorted
	sort.Slice(deliveries, func(i, j int) bool {
		return sequence[deliveries[i]] < sequence[deliveries[j]]
	})
	for _, d := range deliveries {
		st.State.ProcessAdditionalTransaction(d)
	}
	return nil
}

func loadSynthTxns(st *StateManager, tx *Delivery, anchor []byte, source *url.URL, receipt *managed.Receipt, sequence map[*Delivery]int) ([]*Delivery, error) {
	synth, err := st.batch.Account(st.Ledger()).GetSyntheticForAnchor(*(*[32]byte)(anchor))
	if err != nil {
		return nil, fmt.Errorf("failed to load pending synthetic transactions for anchor %X: %w", anchor[:4], err)
	}

	var deliveries []*Delivery
	for _, txid := range synth {
		h := txid.Hash()
		sig, err := getSyntheticSignature(st.batch, st.batch.Transaction(h[:]))
		if err != nil {
			return nil, err
		}

		var d *Delivery
		if receipt != nil {
			d = tx.NewSyntheticReceipt(txid.Hash(), source, receipt)
		} else {
			d = tx.NewSyntheticFromSequence(txid.Hash())
		}
		sequence[d] = int(sig.SequenceNumber)
		deliveries = append(deliveries, d)
	}
	return deliveries, nil
}

func processNetworkAccountUpdates(st *StateManager, delivery *Delivery, updates []protocol.NetworkAccountUpdate) error {
	for _, update := range updates {
		txn := new(protocol.Transaction)
		txn.Body = update.Body

		switch update.Name {
		case protocol.Operators:
			txn.Header.Principal = st.OperatorsPage()
		default:
			txn.Header.Principal = st.NodeUrl(update.Name)
		}

		st.State.ProcessAdditionalTransaction(delivery.NewInternal(txn))
	}
	return nil
}

func getSyntheticSignature(batch *database.Batch, transaction *database.Transaction) (*protocol.PartitionSignature, error) {
	status, err := transaction.GetStatus()
	if err != nil {
		return nil, errors.Format(errors.StatusUnknownError, "load status: %w", err)
	}

	for _, signer := range status.Signers {
		sigset, err := transaction.ReadSignaturesForSigner(signer)
		if err != nil {
			return nil, errors.Format(errors.StatusUnknownError, "load signature set %v: %w", signer.GetUrl(), err)
		}

		for _, entry := range sigset.Entries() {
			state, err := batch.Transaction(entry.SignatureHash[:]).GetState()
			if err != nil {
				return nil, errors.Format(errors.StatusUnknownError, "load signature %x: %w", entry.SignatureHash[:8], err)
			}

			sig, ok := state.Signature.(*protocol.PartitionSignature)
			if ok {
				return sig, nil
			}
		}
	}
	return nil, errors.New(errors.StatusInternalError, "cannot find synthetic signature")
}
