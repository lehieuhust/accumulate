package protocol

import (
	"bytes"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/binary"
	"fmt"
	"io"
)

func NewFactomDataEntry() *FactomDataEntry {
	return new(FactomDataEntry)
}

//ComputeFactomEntryHashForAccount will compute the entry hash from an entry.
//If accountId is nil, then entry will be used to construct an account id,
//and it assumes the entry will be the first entry in the chain
func ComputeFactomEntryHashForAccount(accountId []byte, entry [][]byte) ([]byte, error) {
	lde := FactomDataEntry{}
	if entry == nil {
		return nil, fmt.Errorf("cannot compute lite entry hash, missing entry")
	}
	if len(entry) > 0 {
		lde.Data = entry[0]
		lde.ExtIds = entry[1:]
	}

	if accountId == nil && entry != nil {
		//if we don't know the chain id, we compute one off of the entry
		copy(lde.AccountId[:], ComputeLiteDataAccountId(&lde))
	} else {
		copy(lde.AccountId[:], accountId)
	}
	return lde.Hash(), nil
}

// ComputeFactomEntryHash returns the Entry hash of data for a Factom data entry
// https://github.com/FactomProject/FactomDocs/blob/master/factomDataStructureDetails.md#entry-hash
func ComputeFactomEntryHash(data []byte) []byte {
	sum := sha512.Sum512(data)
	saltedSum := make([]byte, len(sum)+len(data))
	i := copy(saltedSum, sum[:])
	copy(saltedSum[i:], data)
	h := sha256.Sum256(saltedSum)
	return h[:]
}

func (e *FactomDataEntry) Hash() []byte {
	d, err := e.MarshalBinary()
	if err != nil {
		// TransactionPayload.MarshalBinary should never return an error, but
		// better a panic then a silently ignored error.
		panic(err)
	}
	return ComputeFactomEntryHash(d)
}

func (e *FactomDataEntry) GetData() [][]byte {
	return append([][]byte{e.Data}, e.ExtIds...)
}

// MarshalBinary marshal the FactomDataEntry in accordance to
// https://github.com/FactomProject/FactomDocs/blob/master/factomDataStructureDetails.md#entry
func (e *FactomDataEntry) MarshalBinary() ([]byte, error) {
	var b bytes.Buffer

	d := [32]byte{}
	if e.AccountId == ([32]byte{}) {
		return nil, fmt.Errorf("missing ChainID")
	}

	// Header, version byte 0x00
	b.WriteByte(0)
	b.Write(e.AccountId[:])

	// Payload
	var ex bytes.Buffer

	//ExtId's are data entries 1..N if applicable
	for _, data := range e.ExtIds {
		n := len(data)
		binary.BigEndian.PutUint16(d[:], uint16(n))
		ex.Write(d[:2])
		ex.Write(data)
	}
	binary.BigEndian.PutUint16(d[:], uint16(ex.Len()))
	b.Write(d[:2])
	b.Write(ex.Bytes())

	b.Write(e.Data)

	return b.Bytes(), nil
}

// LiteEntryHeaderSize is the exact length of an Entry header.
const LiteEntryHeaderSize = 1 + // version
	32 + // chain id
	2 // total len

// LiteEntryMaxTotalSize is the maximum encoded length of an Entry.
const LiteEntryMaxTotalSize = TransactionSizeMax + LiteEntryHeaderSize

// UnmarshalBinary unmarshal the FactomDataEntry in accordance to
// https://github.com/FactomProject/FactomDocs/blob/master/factomDataStructureDetails.md#entry
func (e *FactomDataEntry) UnmarshalBinary(data []byte) error {

	if len(data) < LiteEntryHeaderSize || len(data) > LiteEntryMaxTotalSize {
		return fmt.Errorf("malformed entry header")
	}

	copy(e.AccountId[:], data[1:33])

	totalExtIdSize := binary.BigEndian.Uint16(data[33:35])

	if int(totalExtIdSize) > len(data)-LiteEntryHeaderSize || totalExtIdSize == 1 {
		return fmt.Errorf("malformed entry payload")
	}

	j := LiteEntryHeaderSize

	//reset the extId's if present
	e.Data = nil
	e.ExtIds = [][]byte{}
	for n := 0; n < int(totalExtIdSize); {
		extIdSize := binary.BigEndian.Uint16(data[j : j+2])
		if extIdSize > totalExtIdSize {
			return fmt.Errorf("malformed extId")
		}
		j += 2
		e.ExtIds = append(e.ExtIds, data[j:j+int(extIdSize)])
		j += n
	}

	e.Data = data[j:]
	return nil
}

func (e *FactomDataEntry) UnmarshalBinaryFrom(reader io.Reader) error {
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}

	return e.UnmarshalBinary(data)
}