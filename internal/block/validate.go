package block

import (
	"strings"

	"gitlab.com/accumulatenetwork/accumulate/internal/chain"
	"gitlab.com/accumulatenetwork/accumulate/internal/database"
	"gitlab.com/accumulatenetwork/accumulate/internal/errors"
	"gitlab.com/accumulatenetwork/accumulate/pkg/url"
	"gitlab.com/accumulatenetwork/accumulate/protocol"
	"gitlab.com/accumulatenetwork/accumulate/smt/storage"
)

func (x *Executor) ValidateEnvelopeSet(batch *database.Batch, deliveries []*chain.Delivery, captureError func(error, *chain.Delivery, *protocol.TransactionStatus)) []*protocol.TransactionStatus {
	results := make([]*protocol.TransactionStatus, len(deliveries))
	for i, delivery := range deliveries {
		status := new(protocol.TransactionStatus)
		results[i] = status

		var err error
		status.Result, err = x.ValidateEnvelope(batch, delivery)

		// Wait until after ValidateEnvelope, because the transaction may get
		// loaded by LoadTransaction
		status.TxID = delivery.Transaction.ID()

		if err != nil {
			status.Set(err)
			if captureError != nil {
				captureError(err, delivery, status)
			}
		}
	}

	return results
}

// ValidateEnvelope verifies that the envelope is valid. It checks the basics,
// like the envelope has signatures and a hash and/or a transaction. It
// validates signatures, ensuring they match the transaction hash, reference a
// signator, etc. And more.
//
// ValidateEnvelope should not modify anything. Right now it updates signer
// timestamps and credits, but that will be moved to ProcessSignature.
func (x *Executor) ValidateEnvelope(batch *database.Batch, delivery *chain.Delivery) (protocol.TransactionResult, error) {
	// If the transaction is borked, the transaction type is probably invalid,
	// so check that first. "Invalid transaction type" is a more useful error
	// than "invalid signature" if the real error is the transaction got borked.
	txnType := delivery.Transaction.Body.Type()
	if txnType == protocol.TransactionTypeSystemWriteData {
		// SystemWriteData transactions are purely internal transactions
		return nil, errors.Format(errors.StatusBadRequest, "unsupported transaction type: %v", txnType)
	}
	if txnType != protocol.TransactionTypeRemote {
		_, ok := x.executors[txnType]
		if !ok {
			return nil, errors.Format(errors.StatusBadRequest, "unsupported transaction type: %v", txnType)
		}
	}

	// Load the transaction
	_, err := delivery.LoadTransaction(batch)
	if err != nil {
		return nil, errors.Wrap(errors.StatusUnknownError, err)
	}

	// Get a temp status - DO NOT STORE THIS
	status, err := batch.Transaction(delivery.Transaction.GetHash()).Status().Get()
	if err != nil {
		return nil, errors.Format(errors.StatusUnknownError, "load status: %w", err)
	}

	// Check that the signatures are valid
	for i, signature := range delivery.Signatures {
		err = x.ValidateSignature(batch, delivery, status, signature)
		if err != nil {
			return nil, errors.Format(errors.StatusUnknownError, "signature %d: %w", i, err)
		}
	}

	switch {
	case txnType.IsUser():
		err = nil
	case txnType.IsSynthetic(), txnType.IsSystem():
		err = validateSyntheticTransactionSignatures(delivery.Transaction, delivery.Signatures)
	default:
		// Should be unreachable
		return nil, errors.Format(errors.StatusInternalError, "transaction type %v is not user, synthetic, or internal", txnType)
	}
	if err != nil {
		return nil, errors.Wrap(errors.StatusUnknownError, err)
	}

	// Only validate the transaction when we first receive it
	if delivery.Transaction.Body.Type() == protocol.TransactionTypeRemote {
		return new(protocol.EmptyResult), nil
	}

	// Do not run transaction-specific validation for a synthetic transaction. A
	// synthetic transaction will be rejected by `m.validate` unless it is
	// signed by a BVN and can be proved to have been included in a DN block. If
	// `m.validate` succeeeds, we know the transaction came from a BVN, thus it
	// is safe and reasonable to allow the transaction to be delivered.
	//
	// This is important because if a synthetic transaction is rejected during
	// CheckTx, it does not get recorded. If the synthetic transaction is not
	// recorded, the BVN that sent it and the client that sent the original
	// transaction cannot verify that the synthetic transaction was received.
	if delivery.Transaction.Body.Type().IsSynthetic() {
		return new(protocol.EmptyResult), nil
	}

	// Load the first signer
	firstSig := delivery.Signatures[0]
	if _, ok := firstSig.(*protocol.ReceiptSignature); ok {
		return nil, errors.Format(errors.StatusBadRequest, "invalid transaction: initiated by receipt signature")
	}

	// Lite token address => lite identity
	var signerUrl *url.URL
	if _, ok := firstSig.(*protocol.PartitionSignature); ok {
		signerUrl = x.Describe.OperatorsPage()
	} else {
		signerUrl = firstSig.GetSigner()
		if key, _, _ := protocol.ParseLiteTokenAddress(signerUrl); key != nil {
			signerUrl = signerUrl.RootIdentity()
		}
	}

	var signer protocol.Signer
	err = batch.Account(signerUrl).GetStateAs(&signer)
	if err != nil {
		return nil, errors.Format(errors.StatusUnknownError, "load signer: %w", err)
	}

	// Do not validate remote transactions
	if !delivery.Transaction.Header.Principal.LocalTo(signer.GetUrl()) {
		return new(protocol.EmptyResult), nil
	}

	// Load the principal
	principal, err := batch.Account(delivery.Transaction.Header.Principal).GetState()
	switch {
	case err == nil:
		// Ok
	case !errors.Is(err, storage.ErrNotFound):
		return nil, errors.Format(errors.StatusUnknownError, "load principal: %w", err)
	case delivery.Transaction.Body.Type().IsUser():
		val, ok := getValidator[chain.PrincipalValidator](x, delivery.Transaction.Body.Type())
		if !ok || !val.AllowMissingPrincipal(delivery.Transaction) {
			return nil, errors.NotFound("missing principal: %v not found", delivery.Transaction.Header.Principal)
		}
	}

	// Set up the state manager
	st := chain.NewStateManager(&x.Describe, &x.globals.Active, batch.Begin(false), principal, delivery.Transaction, x.logger.With("operation", "ValidateEnvelope"))
	defer st.Discard()
	st.Pretend = true

	// Execute the transaction
	executor, ok := x.executors[delivery.Transaction.Body.Type()]
	if !ok {
		return nil, errors.Format(errors.StatusInternalError, "missing executor for %v", delivery.Transaction.Body.Type())
	}

	result, err := executor.Validate(st, delivery)
	if err != nil {
		return nil, errors.Wrap(errors.StatusUnknownError, err)
	}

	return result, nil
}

func (x *Executor) ValidateSignature(batch *database.Batch, delivery *chain.Delivery, status *protocol.TransactionStatus, signature protocol.Signature) error {
	var md sigExecMetadata
	md.IsInitiator = protocol.SignatureDidInitiate(signature, delivery.Transaction.Header.Initiator[:], nil)
	if !signature.Type().IsSystem() {
		md.Location = signature.RoutingLocation()
	}
	_, err := x.validateSignature(batch, delivery, status, signature, md)
	return errors.Wrap(errors.StatusUnknownError, err)
}

func (x *Executor) validateSignature(batch *database.Batch, delivery *chain.Delivery, status *protocol.TransactionStatus, signature protocol.Signature, md sigExecMetadata) (protocol.Signer2, error) {
	err := x.checkRouting(delivery, signature)
	if err != nil {
		return nil, err
	}

	// Verify that the initiator signature matches the transaction
	err = validateInitialSignature(delivery.Transaction, signature, md)
	if err != nil {
		return nil, errors.Wrap(errors.StatusUnknownError, err)
	}

	// Stateful validation (mostly for synthetic transactions)
	var signer protocol.Signer2
	var delegate protocol.Signer
	switch signature := signature.(type) {
	case *protocol.PartitionSignature:
		// Add the partition info to the temp status
		// TODO Replace fields on status with just PartitionSignature
		if status.SourceNetwork == nil {
			status.SourceNetwork = signature.SourceNetwork
			status.DestinationNetwork = signature.DestinationNetwork
			status.SequenceNumber = signature.SequenceNumber
		}

		signer = x.globals.Active.AsSigner(x.Describe.PartitionId)
		err = verifyPartitionSignature(&x.Describe, batch, delivery.Transaction, signature, md)

	case *protocol.ReceiptSignature:
		signer = x.globals.Active.AsSigner(x.Describe.PartitionId)
		err = verifyReceiptSignature(delivery.Transaction, signature, md)

	case *protocol.RemoteSignature:
		return nil, errors.New(errors.StatusBadRequest, "a remote signature is not allowed outside of a forwarded transaction")

	case *protocol.SignatureSet:
		return nil, errors.New(errors.StatusBadRequest, "a signature set is not allowed outside of a forwarded transaction")

	case *protocol.DelegatedSignature:
		if !md.Nested() {
			// Limit delegation depth
			for i, sig := 1, signature.Signature; ; i++ {
				if i > protocol.DelegationDepthLimit {
					return nil, errors.Format(errors.StatusBadRequest, "delegated signature exceeded the depth limit (%d)", protocol.DelegationDepthLimit)
				}
				if del, ok := sig.(*protocol.DelegatedSignature); ok {
					sig = del.Signature
				} else {
					break
				}
			}
		}

		s, err := x.validateSignature(batch, delivery, status, signature.Signature, md.SetDelegated())
		if err != nil {
			return nil, errors.Format(errors.StatusUnknownError, "validate delegated signature: %w", err)
		}
		if !md.Nested() && !signature.Verify(signature.Metadata().Hash(), delivery.Transaction.GetHash()) {
			return nil, errors.Format(errors.StatusBadRequest, "invalid signature")
		}
		if !signature.Delegator.LocalTo(md.Location) {
			return nil, nil
		}

		// Validate the delegator
		signer, err = x.validateSigner(batch, delivery.Transaction, signature.Delegator, md.Location, false, md)
		if err != nil {
			return nil, errors.Wrap(errors.StatusUnknownError, err)
		}

		// Verify delegation
		var ok bool
		delegate, ok = s.(protocol.Signer)
		if !ok {
			// The only non-account signer is the network signer which is only
			// used for system signatures, so this should never happen
			return nil, errors.Format(errors.StatusInternalError, "delegate is not an account")
		}
		_, _, ok = signer.EntryByDelegate(delegate.GetUrl())
		if !ok {
			return nil, errors.Format(errors.StatusUnauthorized, "%v is not authorized to sign for %v", delegate.GetUrl(), signature.Delegator)
		}

	case protocol.KeySignature:
		// Basic validation
		if !md.Nested() && !signature.Verify(nil, delivery.Transaction.GetHash()) {
			return nil, errors.New(errors.StatusBadRequest, "invalid")
		}

		if delivery.Transaction.Body.Type().IsUser() {
			signer, err = x.validateKeySignature(batch, delivery, signature, md, !md.Delegated && delivery.Transaction.Header.Principal.LocalTo(md.Location))
		} else {
			signer, err = x.validatePartitionSignature(signature, delivery.Transaction, status)
		}

	default:
		return nil, errors.Format(errors.StatusBadRequest, "unknown signature type %v", signature.Type())
	}
	if err != nil {
		return nil, errors.Wrap(errors.StatusUnauthenticated, err)
	}

	return signer, nil
}

func validateSyntheticTransactionSignatures(transaction *protocol.Transaction, signatures []protocol.Signature) error {
	var gotSynthSig, gotReceiptSig, gotED25519Sig bool
	for _, sig := range signatures {
		switch sig.(type) {
		case *protocol.PartitionSignature:
			gotSynthSig = true

		case *protocol.ReceiptSignature:
			gotReceiptSig = true

		case *protocol.ED25519Signature, *protocol.LegacyED25519Signature:
			gotED25519Sig = true

		default:
			return errors.Format(errors.StatusBadRequest, "synthetic transaction do not support %T signatures", sig)
		}
	}

	if !gotSynthSig {
		return errors.Format(errors.StatusUnauthenticated, "missing synthetic transaction origin")
	}
	if !gotED25519Sig {
		return errors.Format(errors.StatusUnauthenticated, "missing ED25519 signature")
	}
	if transaction.Body.Type() == protocol.TransactionTypeDirectoryAnchor || transaction.Body.Type() == protocol.TransactionTypeBlockValidatorAnchor {
		return nil
	}

	if !gotReceiptSig {
		return errors.Format(errors.StatusUnauthenticated, "missing synthetic transaction receipt")
	}
	return nil
}

// checkRouting verifies that the signature was routed to the correct partition.
func (x *Executor) checkRouting(delivery *chain.Delivery, signature protocol.Signature) error {
	if signature.Type().IsSystem() {
		return nil
	}

	if delivery.Transaction.Body.Type().IsUser() {
		partition, err := x.Router.RouteAccount(signature.RoutingLocation())
		if err != nil {
			return errors.Wrap(errors.StatusUnknownError, err)
		}
		if !strings.EqualFold(partition, x.Describe.PartitionId) {
			return errors.Format(errors.StatusBadRequest, "signature submitted to %v instead of %v", x.Describe.PartitionId, partition)
		}
	}

	return nil
}
