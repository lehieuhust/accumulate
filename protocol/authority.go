package protocol

import (
	"bytes"
	"crypto/sha256"
	"fmt"

	"gitlab.com/accumulatenetwork/accumulate/internal/url"
)

type Authority interface {
	Account
	GetSigners() []*url.URL
}

type Signer interface {
	AccountWithCredits
	GetVersion() uint64
	GetSignatureThreshold() uint64
	EntryByKey(key []byte) (int, KeyEntry, bool)
	EntryByKeyHash(keyHash []byte) (int, KeyEntry, bool)
}

func UnmarshalSigner(data []byte) (Signer, error) {
	account, err := UnmarshalAccount(data)
	if err != nil {
		return nil, err
	}

	signer, ok := account.(Signer)
	if !ok {
		return nil, fmt.Errorf("account type %v is not a signer", account.Type())
	}

	return signer, nil
}

func UnmarshalSignerJSON(data []byte) (Signer, error) {
	account, err := UnmarshalAccountJSON(data)
	if err != nil {
		return nil, err
	}

	signer, ok := account.(Signer)
	if !ok {
		return nil, fmt.Errorf("account type %v is not a signer", account.Type())
	}

	return signer, nil
}

// MakeLiteSigner returns a copy of the signer with some fields removed.
// This is used for forwarding signers and storing signers in the transaction
// status.
func MakeLiteSigner(signer Signer) Signer {
	switch signer := signer.(type) {
	case *KeyPage:
		// Make a copy of the key page with no keys
		signer = signer.Copy()
		signer.Keys = nil
		return signer

	default:
		return signer
	}
}

/* ***** Unknown signer ***** */

func (s *UnknownSigner) GetUrl() *url.URL                                  { return s.Url }
func (s *UnknownSigner) GetVersion() uint64                                { return s.Version }
func (*UnknownSigner) GetSignatureThreshold() uint64                       { return 1 }
func (*UnknownSigner) EntryByKeyHash(keyHash []byte) (int, KeyEntry, bool) { return -1, nil, false }
func (*UnknownSigner) EntryByKey(key []byte) (int, KeyEntry, bool)         { return -1, nil, false }
func (*UnknownSigner) GetCreditBalance() uint64                            { return 0 }
func (*UnknownSigner) CreditCredits(amount uint64)                         {}
func (*UnknownSigner) DebitCredits(amount uint64) bool                     { return false }
func (*UnknownSigner) CanDebitCredits(amount uint64) bool                  { return false }

/* ***** Lite account auth ***** */

// GetSigners returns the lite address.
func (l *LiteTokenAccount) GetSigners() []*url.URL { return []*url.URL{l.Url} }

// GetVersion returns 1.
func (*LiteTokenAccount) GetVersion() uint64 { return 1 }

// GetSignatureThreshold returns 1.
func (*LiteTokenAccount) GetSignatureThreshold() uint64 { return 1 }

// EntryByKeyHash checks if the key hash matches the lite token account URL.
// EntryByKeyHash will panic if the lite token account URL is invalid.
func (l *LiteTokenAccount) EntryByKeyHash(keyHash []byte) (int, KeyEntry, bool) {
	myKey, _, _ := ParseLiteTokenAddress(l.Url)
	if myKey == nil {
		panic("lite token account URL is not a valid lite address")
	}

	if !bytes.Equal(myKey, keyHash[:20]) {
		return -1, nil, false
	}
	return 0, l, true
}

// EntryByKey checks if the key's hash matches the lite token account URL.
// EntryByKey will panic if the lite token account URL is invalid.
func (l *LiteTokenAccount) EntryByKey(key []byte) (int, KeyEntry, bool) {
	keyHash := sha256.Sum256(key)
	return l.EntryByKeyHash(keyHash[:])
}

/* ***** ADI account auth ***** */

// GetSigners returns URLs of the book's pages.
func (b *KeyBook) GetSigners() []*url.URL {
	pages := make([]*url.URL, b.PageCount)
	for i := uint64(0); i < b.PageCount; i++ {
		pages[i] = FormatKeyPageUrl(b.Url, i)
	}
	return pages
}

// GetVersion returns Version.
func (p *KeyPage) GetVersion() uint64 { return p.Version }

// GetSignatureThreshold returns Threshold.
func (p *KeyPage) GetSignatureThreshold() uint64 {
	if p.AcceptThreshold == 0 {
		return 1
	}
	return p.AcceptThreshold
}

// EntryByKeyHash finds the entry with a matching key hash.
func (p *KeyPage) EntryByKey(key []byte) (int, KeyEntry, bool) {
	keyHash := sha256.Sum256(key)
	return p.EntryByKeyHash(keyHash[:])
}

// EntryByKeyHash finds the entry with a matching key hash.
func (p *KeyPage) EntryByKeyHash(keyHash []byte) (int, KeyEntry, bool) {
	for i, entry := range p.Keys {
		if bytes.Equal(entry.PublicKeyHash, keyHash) {
			return i, entry, true
		}
	}

	return -1, nil, false
}