package chain

import (
	"encoding"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"gitlab.com/accumulatenetwork/accumulate/internal/database"
	"gitlab.com/accumulatenetwork/accumulate/internal/indexing"
	"gitlab.com/accumulatenetwork/accumulate/internal/url"
	"gitlab.com/accumulatenetwork/accumulate/protocol"
	"gitlab.com/accumulatenetwork/accumulate/smt/managed"
	"gitlab.com/accumulatenetwork/accumulate/smt/storage"
	"gitlab.com/accumulatenetwork/accumulate/types"
	"gitlab.com/accumulatenetwork/accumulate/types/api/query"
)

func (m *Executor) queryByUrl(batch *database.Batch, u *url.URL, prove bool) ([]byte, encoding.BinaryMarshaler, error) {
	qv := u.QueryValues()
	switch {
	case qv.Get("txid") != "":
		// Query by transaction ID
		txid, err := hex.DecodeString(qv.Get("txid"))
		if err != nil {
			return nil, nil, fmt.Errorf("invalid txid %q: %v", qv.Get("txid"), err)
		}

		v, err := m.queryByTxId(batch, txid, prove)
		return []byte("tx"), v, err

	case u.Fragment == "":
		// Query by chain URL
		v, err := m.queryByChainId(batch, u.AccountID())
		return []byte("chain"), v, err
	}

	fragment := strings.Split(u.Fragment, "/")
	switch fragment[0] {
	case "chain":
		if len(fragment) < 2 {
			return nil, nil, fmt.Errorf("invalid fragment")
		}

		chain, err := batch.Account(u).ReadChain(fragment[1])
		if err != nil {
			return nil, nil, fmt.Errorf("failed to load chain %q of %q: %v", strings.Join(fragment[1:], "."), u, err)
		}

		switch len(fragment) {
		case 2:
			start, end, err := parseRange(qv)
			if err != nil {
				return nil, nil, err
			}

			res := new(query.ResponseChainRange)
			res.Start = start
			res.End = end
			res.Total = chain.Height()
			res.Entries, err = chain.Entries(start, end)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to load entries: %v", err)
			}

			return []byte("chain-range"), res, nil

		case 3:
			height, entry, err := getChainEntry(chain, fragment[2])
			if err != nil {
				return nil, nil, err
			}

			state, err := chain.State(height)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to load chain state: %v", err)
			}

			res := new(query.ResponseChainEntry)
			res.Height = height
			res.Entry = entry
			res.State = make([][]byte, len(state.Pending))
			for i, h := range state.Pending {
				res.State[i] = h.Copy()
			}
			return []byte("chain-entry"), res, nil
		}

	case "tx", "txn", "transaction":
		switch len(fragment) {
		case 1:
			start, end, err := parseRange(qv)
			if err != nil {
				return nil, nil, err
			}

			txns, perr := m.queryTxHistoryByChainId(batch, u.AccountID(), start, end, protocol.MainChain)
			if perr != nil {
				return nil, nil, perr
			}

			return []byte("tx-history"), txns, nil

		case 2:
			chain, err := batch.Account(u).ReadChain(protocol.MainChain)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to load main chain of %q: %v", u, err)
			}

			height, txid, err := getTransaction(chain, fragment[1])
			if err != nil {
				return nil, nil, err
			}

			state, err := chain.State(height)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to load chain state: %v", err)
			}

			res, err := m.queryByTxId(batch, txid, prove)
			if err != nil {
				return nil, nil, err
			}

			res.Height = height
			res.ChainState = make([][]byte, len(state.Pending))
			for i, h := range state.Pending {
				res.ChainState[i] = h.Copy()
			}

			return []byte("tx"), res, nil
		}
	case "pending":
		switch len(fragment) {
		case 1:
			txIds, err := indexing.PendingTransactions(batch, u).Get()
			if err != nil {
				return nil, nil, err
			}
			resp := new(query.ResponsePending)
			resp.Transactions = txIds
			return []byte("pending"), resp, nil
		case 2:
			if strings.Contains(fragment[1], ":") {
				indexes := strings.Split(fragment[1], ":")
				start, err := strconv.Atoi(indexes[0])
				if err != nil {
					return nil, nil, err
				}
				end, err := strconv.Atoi(indexes[1])
				if err != nil {
					return nil, nil, err
				}
				txns, perr := m.queryTxHistoryByChainId(batch, u.AccountID(), int64(start), int64(end), protocol.PendingChain)
				if perr != nil {
					return nil, nil, perr
				}
				return []byte("tx-history"), txns, nil
			} else {
				chain, err := batch.Account(u).ReadChain(protocol.PendingChain)
				if err != nil {
					return nil, nil, fmt.Errorf("failed to load main chain of %q: %v", u, err)
				}

				height, txid, err := getTransaction(chain, fragment[1])
				if err != nil {
					return nil, nil, err
				}

				state, err := chain.State(height)
				if err != nil {
					return nil, nil, fmt.Errorf("failed to load chain state: %v", err)
				}

				res, err := m.queryByTxId(batch, txid, prove)
				if err != nil {
					return nil, nil, err
				}

				res.Height = height
				res.ChainState = make([][]byte, len(state.Pending))
				for i, h := range state.Pending {
					res.ChainState[i] = h.Copy()
				}

				return []byte("tx"), res, nil
			}
		}
	case "data":
		data, err := batch.Account(u).Data()
		if err != nil {
			return nil, nil, err
		}
		switch len(fragment) {
		case 1:
			entryHash, entry, err := data.GetLatest()
			if err != nil {
				return nil, nil, err
			}
			res := &protocol.ResponseDataEntry{
				Entry: *entry,
			}
			copy(res.EntryHash[:], entryHash)
			return []byte("data-entry"), res, nil
		case 2:
			queryParam := fragment[1]
			if strings.Contains(queryParam, ":") {
				indexes := strings.Split(queryParam, ":")
				start, err := strconv.Atoi(indexes[0])
				if err != nil {
					return nil, nil, err
				}
				end, err := strconv.Atoi(indexes[1])
				if err != nil {
					return nil, nil, err
				}
				entryHashes, err := data.GetHashes(int64(start), int64(end))
				if err != nil {
					return nil, nil, err
				}
				res := &protocol.ResponseDataEntrySet{}
				res.Total = uint64(data.Height())
				for _, entryHash := range entryHashes {
					er := protocol.ResponseDataEntry{}
					copy(er.EntryHash[:], entryHash)

					entry, err := data.Get(entryHash)
					if err != nil {
						return nil, nil, err
					}
					er.Entry = *entry
					res.DataEntries = append(res.DataEntries, er)
				}
				return []byte("data-entry-set"), res, nil
			} else {
				index, err := strconv.Atoi(queryParam)
				if err != nil {
					entryHash, err := hex.DecodeString(queryParam)
					if err != nil {
						return nil, nil, err
					}
					entry, err := data.Get(entryHash)
					if err != nil {
						return nil, nil, err
					}

					res := &protocol.ResponseDataEntry{}
					copy(res.EntryHash[:], entry.Hash())
					res.Entry = *entry
					return []byte("data-entry"), res, nil
				} else {
					entry, err := data.Entry(int64(index))
					if err != nil {
						return nil, nil, err
					}
					res := &protocol.ResponseDataEntry{}

					copy(res.EntryHash[:], entry.Hash())
					res.Entry = *entry
					return []byte("data-entry"), res, nil
				}
			}
		}
	}
	return nil, nil, fmt.Errorf("invalid fragment")
}

func parseRange(qv url.Values) (start, end int64, err error) {
	if s := qv.Get("from"); s != "" {
		start, err = strconv.ParseInt(s, 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid start: %v", err)
		}
	} else {
		start = 0
	}

	if s := qv.Get("from"); s != "" {
		end, err = strconv.ParseInt(s, 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid end: %v", err)
		}
	} else {
		end = 10
	}

	return start, end, nil
}

func getChainEntry(chain *database.Chain, s string) (int64, []byte, error) {
	var valid bool

	height, err := strconv.ParseInt(s, 10, 64)
	if err == nil {
		valid = true
		entry, err := chain.Entry(height)
		if err == nil {
			return height, entry, nil
		}
		if !errors.Is(err, storage.ErrNotFound) {
			return 0, nil, err
		}
	}

	entry, err := hex.DecodeString(s)
	if err == nil {
		valid = true
		height, err := chain.HeightOf(entry)
		if err == nil {
			return height, entry, nil
		}
		if !errors.Is(err, storage.ErrNotFound) {
			return 0, nil, err
		}
	}

	if valid {
		return 0, nil, fmt.Errorf("entry %q %w", s, storage.ErrNotFound)
	}
	return 0, nil, fmt.Errorf("invalid entry: %q is not a number or a hash", s)
}

func getTransaction(chain *database.Chain, s string) (int64, []byte, error) {
	var valid bool

	height, err := strconv.ParseInt(s, 10, 64)
	if err == nil {
		valid = true
		id, err := chain.Entry(height)
		if err == nil {
			return height, id, nil
		}
		if !errors.Is(err, storage.ErrNotFound) {
			return 0, nil, err
		}
	}

	entry, err := hex.DecodeString(s)
	if err == nil {
		valid = true
		height, err := chain.HeightOf(entry)
		if err == nil {
			return height, entry, nil
		}
		if !errors.Is(err, storage.ErrNotFound) {
			return 0, nil, err
		}
	}

	if valid {
		return 0, nil, fmt.Errorf("transaction %q %w", s, storage.ErrNotFound)
	}
	return 0, nil, fmt.Errorf("invalid transaction: %q is not a number or a hash", s)
}

func (m *Executor) queryByChainId(batch *database.Batch, chainId []byte) (*query.ResponseByChainId, error) {
	qr := query.ResponseByChainId{}

	// Look up record or transaction
	var obj encoding.BinaryMarshaler
	obj, err := batch.AccountByID(chainId).GetState()
	if errors.Is(err, storage.ErrNotFound) {
		obj, err = batch.Transaction(chainId).GetState()
	}
	// Not a record or a transaction
	if errors.Is(err, storage.ErrNotFound) {
		return nil, fmt.Errorf("%w: no chain or transaction found for %X", storage.ErrNotFound, chainId)
	}
	// Some other error
	if err != nil {
		return nil, fmt.Errorf("failed to locate chain entry for chain id %x: %v", chainId, err)
	}

	qr.Object.Entry, err = obj.MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal %T (id %x): %v", obj, chainId, err)
	}

	// Add Merkle chain info (for records)
	chain, err := batch.AccountByID(chainId).ReadChain(protocol.MainChain)
	if err == nil {
		qr.Height = uint64(chain.Height())

		pending := chain.Pending()
		qr.Roots = make([][]byte, len(pending))
		for i, h := range pending {
			qr.Roots[i] = h
		}
	}

	return &qr, nil
}

func (m *Executor) queryDirectoryByChainId(batch *database.Batch, chainId []byte, start uint64, limit uint64) (*protocol.DirectoryQueryResult, error) {
	md, err := loadDirectoryMetadata(batch, chainId)
	if err != nil {
		return nil, err
	}

	count := limit
	if start+count > md.Count {
		count = md.Count - start
	}
	if count > md.Count { // when uint64 0-x is really big number
		count = 0
	}

	resp := new(protocol.DirectoryQueryResult)
	resp.Entries = make([]string, count)

	for i := uint64(0); i < count; i++ {
		resp.Entries[i], err = loadDirectoryEntry(batch, chainId, start+i)
		if err != nil {
			return nil, fmt.Errorf("failed to get entry %d", i)
		}
	}
	resp.Total = md.Count

	return resp, nil
}

func (m *Executor) queryByTxId(batch *database.Batch, txid []byte, prove bool) (*query.ResponseByTxId, error) {
	var err error

	tx := batch.Transaction(txid)
	txState, err := tx.GetState()
	if errors.Is(err, storage.ErrNotFound) {
		return nil, fmt.Errorf("tx %X %w", txid, storage.ErrNotFound)
	} else if err != nil {
		return nil, fmt.Errorf("invalid query from GetTx in state database, %v", err)
	}

	status, err := tx.GetStatus()
	if err != nil {
		return nil, fmt.Errorf("invalid query from GetTx in state database, %v", err)
	} else if !status.Delivered && status.Remote {
		// If the transaction is a synthetic transaction produced by this BVN
		// and has not been delivered, pretend like it doesn't exist
		return nil, fmt.Errorf("tx %X %w", txid, storage.ErrNotFound)
	}

	signatures, err := tx.GetSignatures()
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return nil, fmt.Errorf("invalid query from GetTx in state database, %v", err)
	}

	qr := query.ResponseByTxId{}
	qr.Envelope = txState
	qr.Status = status
	qr.Envelope.Signatures = signatures.Signatures
	copy(qr.TxId[:], txid)
	qr.Height = -1

	synth, err := tx.GetSyntheticTxns()
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return nil, fmt.Errorf("invalid query from GetTx in state database, %v", err)
	}

	qr.TxSynthTxIds = make(types.Bytes, 0, len(synth)*32)
	for _, synth := range synth {
		qr.TxSynthTxIds = append(qr.TxSynthTxIds, synth[:]...)
	}

	err = getPendingStatus(batch, &txState.Transaction.TransactionHeader, status, &qr)
	if err != nil {
		return nil, err
	}

	if !prove {
		return &qr, nil
	}

	rootChain, err := batch.Account(m.Network.NodeUrl(protocol.Ledger)).ReadChain(protocol.MinorRootChain)
	if err != nil {
		return nil, fmt.Errorf("failed to read the minor root chain: %v", err)
	}

	chainIndex, err := indexing.TransactionChain(batch, txid).Get()
	if err != nil {
		return nil, fmt.Errorf("failed to load transaction chain index: %v", err)
	}

	qr.Receipts = make([]*query.TxReceipt, len(chainIndex.Entries))
	for i, entry := range chainIndex.Entries {
		receipt, err := m.resolveTxReceipt(batch, rootChain, txid, entry)
		if err != nil {
			// If one receipt fails to build, do not cause the entire request to
			// fail
			receipt.Error = err.Error()
		}
		qr.Receipts[i] = receipt
	}

	return &qr, nil
}

func (m *Executor) queryTxHistoryByChainId(batch *database.Batch, id []byte, start, end int64, chainName string) (*query.ResponseTxHistory, *protocol.Error) {
	chain, err := batch.AccountByID(id).ReadChain(chainName)
	if err != nil {
		return nil, &protocol.Error{Code: protocol.ErrorCodeTxnHistory, Message: fmt.Errorf("error obtaining txid range %v", err)}
	}

	thr := query.ResponseTxHistory{}
	thr.Start = start
	thr.End = end
	thr.Total = chain.Height()

	txids, err := chain.Entries(start, end)
	if err != nil {
		return nil, &protocol.Error{Code: protocol.ErrorCodeTxnHistory, Message: fmt.Errorf("error obtaining txid range %v", err)}
	}

	for _, txid := range txids {
		qr, err := m.queryByTxId(batch, txid, false)
		if err != nil {
			return nil, &protocol.Error{Code: protocol.ErrorCodeTxnQueryError, Message: err}
		}
		thr.Transactions = append(thr.Transactions, *qr)
	}

	return &thr, nil
}

func (m *Executor) queryDataByUrl(batch *database.Batch, u *url.URL) (*protocol.ResponseDataEntry, error) {
	qr := protocol.ResponseDataEntry{}

	data, err := batch.Account(u).Data()
	if err != nil {
		return nil, err
	}

	entryHash, entry, err := data.GetLatest()
	if err != nil {
		return nil, err
	}

	copy(qr.EntryHash[:], entryHash)
	qr.Entry = *entry
	return &qr, nil
}

func (m *Executor) queryDataByEntryHash(batch *database.Batch, u *url.URL, entryHash []byte) (*protocol.ResponseDataEntry, error) {
	qr := protocol.ResponseDataEntry{}
	copy(qr.EntryHash[:], entryHash)

	data, err := batch.Account(u).Data()
	if err != nil {
		return nil, err
	}

	entry, err := data.Get(entryHash)
	if err != nil {
		return nil, err
	}

	qr.Entry = *entry
	return &qr, nil
}

func (m *Executor) queryDataSet(batch *database.Batch, u *url.URL, start int64, limit int64, expand bool) (*protocol.ResponseDataEntrySet, error) {
	qr := protocol.ResponseDataEntrySet{}

	data, err := batch.Account(u).Data()
	if err != nil {
		return nil, err
	}

	entryHashes, err := data.GetHashes(start, start+limit)
	if err != nil {
		return nil, err
	}

	qr.Total = uint64(data.Height())
	for _, entryHash := range entryHashes {
		er := protocol.ResponseDataEntry{}
		copy(er.EntryHash[:], entryHash)

		if expand {
			entry, err := data.Get(entryHash)
			if err != nil {
				return nil, err
			}
			er.Entry = *entry
		}

		qr.DataEntries = append(qr.DataEntries, er)
	}
	return &qr, nil
}

func (m *Executor) Query(q *query.Query, _ int64, prove bool) (k, v []byte, err *protocol.Error) {
	batch := m.DB.Begin(true)
	defer batch.Discard()

	switch q.Type {
	case types.QueryTypeTxId:
		txr := query.RequestByTxId{}
		err := txr.UnmarshalBinary(q.Content)
		if err != nil {
			return nil, nil, &protocol.Error{Code: protocol.ErrorCodeUnMarshallingError, Message: err}
		}
		qr, err := m.queryByTxId(batch, txr.TxId[:], prove)
		if err != nil {
			return nil, nil, &protocol.Error{Code: protocol.ErrorCodeTxnQueryError, Message: err}
		}
		k = []byte("tx")
		v, err = qr.MarshalBinary()
		if err != nil {
			return nil, nil, &protocol.Error{Code: protocol.ErrorCodeMarshallingError, Message: fmt.Errorf("%v, on Chain %x", err, txr.TxId[:])}
		}
	case types.QueryTypeTxHistory:
		txh := query.RequestTxHistory{}
		err := txh.UnmarshalBinary(q.Content)
		if err != nil {
			return nil, nil, &protocol.Error{Code: protocol.ErrorCodeUnMarshallingError, Message: err}
		}

		thr, perr := m.queryTxHistoryByChainId(batch, txh.ChainId[:], txh.Start, txh.Start+txh.Limit, protocol.MainChain)
		if perr != nil {
			return nil, nil, perr
		}

		k = []byte("tx-history")
		v, err = thr.MarshalBinary()
		if err != nil {
			return nil, nil, &protocol.Error{Code: protocol.ErrorCodeMarshallingError, Message: fmt.Errorf("error marshalling payload for transaction history")}
		}
	case types.QueryTypeUrl:
		chr := query.RequestByUrl{}
		err := chr.UnmarshalBinary(q.Content)
		if err != nil {
			return nil, nil, &protocol.Error{Code: protocol.ErrorCodeUnMarshallingError, Message: err}
		}
		u, err := url.Parse(*chr.Url.AsString())
		if err != nil {
			return nil, nil, &protocol.Error{Code: protocol.ErrorCodeInvalidURL, Message: fmt.Errorf("invalid URL in query %s", chr.Url)}
		}

		var obj encoding.BinaryMarshaler
		k, obj, err = m.queryByUrl(batch, u, prove)
		if err != nil {
			return nil, nil, &protocol.Error{Code: protocol.ErrorCodeTxnQueryError, Message: err}
		}
		v, err = obj.MarshalBinary()
		if err != nil {
			return nil, nil, &protocol.Error{Code: protocol.ErrorCodeMarshallingError, Message: fmt.Errorf("%v, on Url %s", err, chr.Url)}
		}
	case types.QueryTypeDirectoryUrl:
		chr := query.RequestDirectory{}
		err := chr.UnmarshalBinary(q.Content)
		if err != nil {
			return nil, nil, &protocol.Error{Code: protocol.ErrorCodeUnMarshallingError, Message: err}
		}
		u, err := url.Parse(*chr.Url.AsString())
		if err != nil {
			return nil, nil, &protocol.Error{Code: protocol.ErrorCodeInvalidURL, Message: fmt.Errorf("invalid URL in query %s", chr.Url)}
		}
		dir, err := m.queryDirectoryByChainId(batch, u.AccountID(), chr.Start, chr.Limit)
		if err != nil {
			return nil, nil, &protocol.Error{Code: protocol.ErrorCodeDirectoryURL, Message: err}
		}

		if chr.ExpandChains {
			entries, err := m.expandChainEntries(batch, dir.Entries)
			if err != nil {
				return nil, nil, &protocol.Error{Code: protocol.ErrorCodeDirectoryURL, Message: err}
			}
			dir.ExpandedEntries = entries
		}

		k = []byte("directory")
		v, err = dir.MarshalBinary()
		if err != nil {
			return nil, nil, &protocol.Error{Code: protocol.ErrorCodeMarshallingError, Message: fmt.Errorf("%v, on Url %s", err, chr.Url)}
		}
	case types.QueryTypeChainId:
		chr := query.RequestByChainId{}
		err := chr.UnmarshalBinary(q.Content)
		if err != nil {
			return nil, nil, &protocol.Error{Code: protocol.ErrorCodeUnMarshallingError, Message: err}
		}
		obj, err := m.queryByChainId(batch, chr.ChainId[:])
		if err != nil {
			return nil, nil, &protocol.Error{Code: protocol.ErrorCodeChainIdError, Message: err}
		}
		k = []byte("chain")
		v, err = obj.MarshalBinary()
		if err != nil {
			return nil, nil, &protocol.Error{Code: protocol.ErrorCodeMarshallingError, Message: fmt.Errorf("%v, on Chain %x", err, chr.ChainId)}
		}
	case types.QueryTypeData:
		chr := query.RequestDataEntry{}
		err := chr.UnmarshalBinary(q.Content)
		if err != nil {
			return nil, nil, &protocol.Error{Code: protocol.ErrorCodeUnMarshallingError, Message: err}
		}

		u := chr.Url
		var ret *protocol.ResponseDataEntry
		if chr.EntryHash != [32]byte{} {
			ret, err = m.queryDataByEntryHash(batch, u, chr.EntryHash[:])
			if err != nil {
				return nil, nil, &protocol.Error{Code: protocol.ErrorCodeDataEntryHashError, Message: err}
			}
		} else {
			ret, err = m.queryDataByUrl(batch, u)
			if err != nil {
				return nil, nil, &protocol.Error{Code: protocol.ErrorCodeDataUrlError, Message: err}
			}
		}

		k = []byte("data")
		v, err = ret.MarshalBinary()
		if err != nil {
			return nil, nil, &protocol.Error{Code: protocol.ErrorCodeMarshallingError, Message: err}
		}
	case types.QueryTypeDataSet:
		chr := query.RequestDataEntrySet{}
		err := chr.UnmarshalBinary(q.Content)
		if err != nil {
			return nil, nil, &protocol.Error{Code: protocol.ErrorCodeUnMarshallingError, Message: err}
		}
		u := chr.Url
		ret, err := m.queryDataSet(batch, u, int64(chr.Start), int64(chr.Count), chr.ExpandChains)
		if err != nil {
			return nil, nil, &protocol.Error{Code: protocol.ErrorCodeDataEntryHashError, Message: err}
		}

		k = []byte("dataSet")
		v, err = ret.MarshalBinary()
		if err != nil {
			return nil, nil, &protocol.Error{Code: protocol.ErrorCodeMarshallingError, Message: err}
		}
	case types.QueryTypeKeyPageIndex:
		chr := query.RequestKeyPageIndex{}
		err := chr.UnmarshalBinary(q.Content)
		if err != nil {
			return nil, nil, &protocol.Error{Code: protocol.ErrorCodeUnMarshallingError, Message: err}
		}
		u := chr.Url
		obj, err := m.queryByChainId(batch, u.AccountID())
		if err != nil {
			return nil, nil, &protocol.Error{Code: protocol.ErrorCodeChainIdError, Message: err}
		}
		account, err := protocol.UnmarshalAccount(obj.Entry)
		if err != nil {
			return nil, nil, &protocol.Error{Code: protocol.ErrorCodeMarshallingError, Message: fmt.Errorf("inavid object error")}
		}
		if account.GetType() != protocol.AccountTypeKeyBook {
			obj, err = m.queryByChainId(batch, account.Header().KeyBook.AccountID())
			if err != nil {
				return nil, nil, &protocol.Error{Code: protocol.ErrorCodeChainIdError, Message: err}
			}
			_, err = protocol.UnmarshalAccount(obj.Entry)
			if err != nil {
				return nil, nil, &protocol.Error{Code: protocol.ErrorCodeMarshallingError, Message: fmt.Errorf("inavid object error")}
			}
		}
		k = []byte("key-page-index")
		var found bool
		keyBook := new(protocol.KeyBook)
		if err = obj.As(keyBook); err != nil {
			return nil, nil, &protocol.Error{Code: protocol.ErrorCodeMarshallingError, Message: fmt.Errorf("invalid object error")}
		}
		response := query.ResponseKeyPageIndex{
			KeyBook: keyBook.Url,
		}
		for index := uint64(0); index < keyBook.PageCount; index++ {
			pageUrl := protocol.FormatKeyPageUrl(keyBook.Url, index)
			pageObject, err := m.queryByChainId(batch, pageUrl.AccountID())
			if err != nil {
				return nil, nil, &protocol.Error{Code: protocol.ErrorCodeChainIdError, Message: err}
			}
			keyPage := new(protocol.KeyPage)
			if err = pageObject.As(keyPage); err != nil {
				return nil, nil, &protocol.Error{Code: protocol.ErrorCodeMarshallingError, Message: fmt.Errorf("invalid object error")}
			}
			if keyPage.FindKey(chr.Key) != nil {
				response.KeyPage = keyPage.Url
				response.Index = index
				found = true
				break
			}
		}
		if !found {
			return nil, nil, &protocol.Error{Code: protocol.ErrorCodeNotFound, Message: fmt.Errorf("key %X not found in keypage url %s", chr.Key, chr.Url)}
		}
		v, err = response.MarshalBinary()
		if err != nil {
			return nil, nil, &protocol.Error{Code: protocol.ErrorCodeMarshallingError, Message: err}
		}
	default:
		return nil, nil, &protocol.Error{Code: protocol.ErrorCodeInvalidQueryType, Message: fmt.Errorf("unable to query for type, %s (%d)", q.Type.Name(), q.Type.AsUint64())}
	}
	return k, v, err
}

func (m *Executor) expandChainEntries(batch *database.Batch, entries []string) ([]*protocol.Object, error) {
	expEntries := make([]*protocol.Object, len(entries))
	for i, entry := range entries {
		index := i
		r, err := m.expandChainEntry(batch, entry)
		if err != nil {
			return nil, err
		}
		expEntries[index] = r
	}
	return expEntries, nil
}

func (m *Executor) expandChainEntry(batch *database.Batch, entryUrl string) (*protocol.Object, error) {
	u, err := url.Parse(entryUrl)
	if err != nil {
		return nil, fmt.Errorf("invalid URL in query %s", entryUrl)
	}

	v, err := m.queryByChainId(batch, u.AccountID())
	if err != nil {
		return nil, &protocol.Error{Code: protocol.ErrorCodeTxnQueryError, Message: err}
	}
	return &protocol.Object{Entry: v.Entry, Height: v.Height, Roots: v.Roots}, nil
}

func (m *Executor) resolveTxReceipt(batch *database.Batch, rootChain *database.Chain, txid []byte, entry *indexing.TransactionChainEntry) (*query.TxReceipt, error) {
	receipt := new(query.TxReceipt)
	receipt.Account = entry.Account
	receipt.Chain = entry.Chain
	receipt.Receipt.Start = txid

	// Load the index entry
	account := batch.Account(entry.Account)
	accountIndex, err := m.loadIndexEntry(account, entry.Chain, protocol.IndexChain(entry.Chain, false), entry.ChainIndex)
	if err != nil {
		return receipt, err
	}

	// Load the root index entry
	ledger := batch.Account(m.Network.NodeUrl(protocol.Ledger))
	rootIndex, err := m.loadIndexEntry(ledger, protocol.MinorRootChain, protocol.MinorRootIndexChain, entry.AnchorIndex)
	if err != nil {
		return receipt, err
	}

	// Get a receipt from the account's chain
	accountReceipt, err := m.getIndexedChainReceipt(account, entry.Chain, txid, accountIndex)
	if err != nil {
		return receipt, err
	}

	// Get a receipt from the root chain
	rootReceipt, err := m.getReceipt(rootChain, protocol.MinorRootChain, accountIndex.Anchor, rootIndex.Source)
	if err != nil {
		return receipt, err
	}

	// Finalize the receipt
	r, err := combineReceipts(nil, accountReceipt, rootReceipt)
	if err != nil {
		return receipt, err
	}

	// TODO Include the part of the receipt from the DN

	receipt.Receipt = *protocol.ReceiptFromManaged(r)
	return receipt, nil
}

// getReceipt gets a receipt for a chain from an entry to an anchor.
func (m *Executor) getReceipt(chain *database.Chain, name string, from, to uint64) (*managed.Receipt, error) {
	receipt, err := chain.Receipt(int64(from), int64(to))
	if err != nil {
		return nil, fmt.Errorf("unable to construct a receipt from %d to %d for the %s chain: %w", from, to, name, err)
	}

	return receipt, nil
}

// loadIndexEntry loads an entry from an index chain.
func (m *Executor) loadIndexEntry(account *database.Account, chainName, indexChain string, index uint64) (*protocol.IndexEntry, error) {
	// Load the chain
	chain, err := account.ReadChain(indexChain)
	if err != nil {
		return nil, fmt.Errorf("unable to load the index chain of the %s chain: %w", chainName, err)
	}

	// Load the entry
	entry := new(protocol.IndexEntry)
	err = chain.EntryAs(int64(index), entry)
	if err != nil {
		return nil, fmt.Errorf("unable to load index entry %d for the index chain of the %s chain: %w", index, chainName, err)
	}

	return entry, nil
}

// getIndexedChainReceipt locates a chain entry and gets a receipt from that
// entry to an indexed anchor.
func (m *Executor) getIndexedChainReceipt(account *database.Account, name string, chainEntry []byte, indexEntry *protocol.IndexEntry) (*managed.Receipt, error) {
	// Load the chain
	chain, err := account.ReadChain(name)
	if err != nil {
		return nil, fmt.Errorf("unable to load the %s chain: %w", name, err)
	}

	// Find the entry
	entryIndex, err := chain.HeightOf(chainEntry)
	if err != nil {
		return nil, fmt.Errorf("unable to find entry %X of the %s chain: %w", chainEntry, name, err)
	}

	// Get the receipt
	return m.getReceipt(chain, name, uint64(entryIndex), indexEntry.Source)
}
