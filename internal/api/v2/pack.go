package api

import (
	"encoding"
	"encoding/hex"
	"fmt"

	"gitlab.com/accumulatenetwork/accumulate/protocol"
	"gitlab.com/accumulatenetwork/accumulate/types/api/query"
)

func packStateResponse(account protocol.Account, chains []query.ChainState, receipt *query.GeneralReceipt) (*ChainQueryResponse, error) {
	res := new(ChainQueryResponse)
	res.Type = account.Type().String()
	res.Data = account
	res.Chains = chains
	res.ChainId = account.Header().Url.AccountID()
	res.Receipt = receipt

	for _, chain := range chains {
		if chain.Name != protocol.MainChain {
			continue
		}

		res.MainChain = new(MerkleState)
		res.MainChain.Height = chain.Height
		res.MainChain.Roots = chain.Roots
	}
	return res, nil
}

func packTxResponse(qrResp *query.ResponseByTxId, ms *MerkleState, envelope *protocol.Envelope, status *protocol.TransactionStatus) (*TransactionQueryResponse, error) {
	res := new(TransactionQueryResponse)
	res.Type = envelope.Transaction.Body.Type().String()
	res.Data = envelope.Transaction.Body
	res.TransactionHash = qrResp.TxId[:]
	res.MainChain = ms
	res.Transaction = envelope.Transaction

	if len(qrResp.TxSynthTxIds)%32 != 0 {
		return nil, fmt.Errorf("invalid synthetic transaction information, not divisible by 32")
	}

	if qrResp.TxSynthTxIds != nil {
		res.SyntheticTxids = make([][32]byte, len(qrResp.TxSynthTxIds)/32)
		for i := range res.SyntheticTxids {
			copy(res.SyntheticTxids[i][:], qrResp.TxSynthTxIds[i*32:(i+1)*32])
		}
	}

	switch payload := envelope.Transaction.Body.(type) {
	case *protocol.SendTokens:
		if qrResp.TxSynthTxIds != nil && len(res.SyntheticTxids) != len(payload.To) {
			return nil, fmt.Errorf("not enough synthetic TXs: want %d, got %d", len(payload.To), len(res.SyntheticTxids))
		}

		res.Origin = envelope.Transaction.Header.Principal
		data := new(TokenSend)
		data.From = envelope.Transaction.Header.Principal
		data.To = make([]TokenDeposit, len(payload.To))
		for i, to := range payload.To {
			data.To[i].Url = to.Url
			data.To[i].Amount = to.Amount
			if qrResp.TxSynthTxIds != nil {
				data.To[i].Txid = qrResp.TxSynthTxIds[i*32 : (i+1)*32]
			}
		}

		res.Origin = envelope.Transaction.Header.Principal
		res.Data = data

	case *protocol.SyntheticDepositTokens:
		res.Origin = envelope.Transaction.Header.Principal
		res.Data = payload

	default:
		res.Origin = envelope.Transaction.Header.Principal
		res.Data = payload
	}

	res.Status = status
	res.Receipts = qrResp.Receipts

	books := map[string]*SignatureBook{}
	for _, signer := range qrResp.Signers {
		res.Signatures = append(res.Signatures, signer.Signatures...)

		var book *SignatureBook
		signerUrl := signer.Account.Header().Url
		bookUrl, _, ok := protocol.ParseKeyPageUrl(signerUrl)
		if !ok {
			book = new(SignatureBook)
			book.Authority = signerUrl
			res.SignatureBooks = append(res.SignatureBooks, book)
		} else if book, ok = books[bookUrl.String()]; !ok {
			book = new(SignatureBook)
			book.Authority = bookUrl
			res.SignatureBooks = append(res.SignatureBooks, book)
		}

		page := new(SignaturePage)
		book.Pages = append(book.Pages, page)
		page.Signer.Type = signer.Account.Type()
		page.Signer.Url = signerUrl
		page.Signatures = signer.Signatures

		keyPage, ok := signer.Account.(*protocol.KeyPage)
		if ok {
			page.Signer.AcceptThreshold = keyPage.Threshold
		}
	}

	return res, nil
}

func packChainValue(qr *query.ResponseChainEntry) *ChainQueryResponse {
	resp := new(ChainQueryResponse)
	resp.Type = "chainEntry"
	resp.MainChain = new(MerkleState)
	resp.MainChain.Height = uint64(qr.Height)
	resp.MainChain.Roots = qr.State
	resp.Receipt = qr.Receipt

	entry := new(ChainEntry)
	entry.Height = qr.Height
	entry.Entry = qr.Entry
	entry.State = qr.State
	resp.Data = entry

	var value encoding.BinaryUnmarshaler
	switch qr.Type {
	default:
		return resp
	case protocol.ChainTypeData:
		value = new(protocol.DataEntry)
	case protocol.ChainTypeIndex:
		value = new(protocol.IndexEntry)
	}

	if value.UnmarshalBinary(entry.Entry) == nil {
		entry.Value = value
	}
	return resp
}

func packChainValues(qr *query.ResponseChainRange) *MultiResponse {
	resp := new(MultiResponse)
	resp.Type = "chainEntrySet"
	resp.Start = uint64(qr.Start)
	resp.Count = uint64(qr.End - qr.Start)
	resp.Total = uint64(qr.Total)
	resp.Items = make([]interface{}, len(qr.Entries))
	for i, entry := range qr.Entries {
		qr := new(ChainQueryResponse)
		resp.Items[i] = qr
		qr.Type = "hex"
		qr.Data = hex.EncodeToString(entry)
	}

	var newValue func() encoding.BinaryUnmarshaler
	switch qr.Type {
	default:
		return resp
	case protocol.ChainTypeData:
		newValue = func() encoding.BinaryUnmarshaler { return new(protocol.DataEntry) }
	case protocol.ChainTypeIndex:
		newValue = func() encoding.BinaryUnmarshaler { return new(protocol.IndexEntry) }
	}

	resp.OtherItems = make([]interface{}, len(qr.Entries))
	for i, entry := range qr.Entries {
		value := newValue()
		if value.UnmarshalBinary(entry) == nil {
			resp.Items[i] = value
		}
	}
	return resp
}
