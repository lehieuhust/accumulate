package types

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/tendermint/tendermint/crypto/ed25519"
)

// MarshalBinaryLedgerAdiChainPath adiChainPath == adi/chain/path
//This function will generate a ledger needed for ed25519 signing or sha256 hashed to produce TXID
func MarshalBinaryLedgerAdiChainPath(adiChainPath string, payload []byte, timestamp int64) []byte {
	var msg []byte

	//the timestamp will act
	var tsbytes [8]byte
	binary.BigEndian.PutUint64(tsbytes[:], uint64(timestamp))
	msg = append(msg, tsbytes[:]...)

	//The chain path is either the identity name or the full chain path [identityname]/[chainpath]
	chainid := sha256.Sum256([]byte(adiChainPath))
	msg = append(msg, chainid[:]...)

	msg = append(msg, payload...)

	return msg
}

// MarshalBinaryLedgerChainId create a ledger that can be used for signing or generating a txid
func MarshalBinaryLedgerChainId(chainId []byte, payload []byte, timestamp int64) []byte {
	var msg []byte

	var tsbytes [8]byte
	binary.BigEndian.PutUint64(tsbytes[:], uint64(timestamp))
	msg = append(msg, tsbytes[:]...)

	//The chain path is either the identity name or the full chain path [identityname]/[chainpath]
	msg = append(msg, chainId[:]...)

	msg = append(msg, payload...)

	return msg
}

// CreateKeyPair generic helper function to create an ed25519 key pair
func CreateKeyPair() ed25519.PrivKey {
	return ed25519.GenPrivKey()
}

func CreateKeyPairFromSeed(seed ed25519.PrivKey) (ret ed25519.PrivKey) {
	ret = seed
	return ret
}

// ParseIdentityChainPath helpful parser to extract the identity name and chainpath
//for example RedWagon/MyAccAddress becomes identity=redwagon and chainpath=redwagon/MyAccAddress
func ParseIdentityChainPath(adiChainPath *string) (adi string, chainPath string, err error) {

	s := *adiChainPath

	if !utf8.ValidString(s) {
		return "", "", fmt.Errorf("URL is has invalid UTF8 encoding")
	}

	if !strings.HasPrefix(s, "acc://") {
		s = "acc://" + s
	}

	u, err := url.Parse(s)
	if err != nil {
		return "", "", err
	}
	adi = strings.ToLower(u.Hostname())
	chainPath = adi
	if len(u.Path) != 0 {
		chainPath += u.Path
	}
	return adi, chainPath, nil
}

// GetChainIdFromChainPath this expects an identity chain path to produce the chainid.  RedWagon/Acc/Chain/Path
func GetChainIdFromChainPath(adiChainPath *string) *Bytes32 {
	_, chainPathFormatted, err := ParseIdentityChainPath(adiChainPath)
	if err != nil {
		return nil
	}

	h := Bytes32(sha256.Sum256([]byte(chainPathFormatted)))
	return &h
}

// GetIdentityChainFromIdentity Helper function to generate an identity chain from adi. can return nil, if the adi is malformed
func GetIdentityChainFromIdentity(adi *string) *Bytes32 {
	namelower, _, err := ParseIdentityChainPath(adi)
	if err != nil {
		return nil
	}

	h := Bytes32(sha256.Sum256([]byte(namelower)))
	return &h
}

//GetAddressFromIdentityChain get the 8-bit address from the identity chain.  this is used for bvc routing
func GetAddressFromIdentityChain(identitychain []byte) uint64 {
	addr := binary.BigEndian.Uint64(identitychain)
	return addr
}

//GetAddressFromIdentity given a string, return the address used for bvc routing
func GetAddressFromIdentity(name *string) uint64 {
	b := GetIdentityChainFromIdentity(name)
	return GetAddressFromIdentityChain(b[:])
}

type Bytes []byte

// MarshalJSON serializes ByteArray to hex
func (s *Bytes) MarshalJSON() ([]byte, error) {
	b, err := json.Marshal(fmt.Sprintf("%x", string(*s)))
	return b, err
}

// UnmarshalJSON serializes ByteArray to hex
func (s *Bytes) UnmarshalJSON(data []byte) error {
	str := strings.Trim(string(data), `"`)
	d, err := hex.DecodeString(str)
	if err != nil {
		return nil
	}
	*s = make([]byte, len(d))
	copy(*s, d)
	return err
}

func (s *Bytes) Bytes() []byte {
	return *s
}

func (s *Bytes) MarshalBinary() ([]byte, error) {
	var buf [8]byte
	l := s.Size(&buf)
	i := l - len(*s)
	data := make([]byte, l)
	copy(data, buf[:i])
	copy(data[i:], *s)
	return data, nil
}

func (s *Bytes) UnmarshalBinary(data []byte) (err error) {
	defer func() { //
		if recover() != nil { //
			err = fmt.Errorf("error unmarshaling byte array %v", err) //
		} //
	}()
	slen, l := binary.Varint(data)
	if l == 0 {
		return nil
	}
	if l < 0 {
		return fmt.Errorf("invalid data to unmarshal")
	}
	if len(data) < int(slen)+l {
		return fmt.Errorf("insufficient data to unmarshal")
	}
	*s = data[l : int(slen)+l]
	return err
}

func (s *Bytes) Size(varintbuf *[8]byte) int {
	buf := varintbuf
	if buf == nil {
		buf = &[8]byte{}
	}
	l := int64(len(*s))
	i := binary.PutVarint(buf[:], l)

	return i + int(l)
}

// Bytes32 is a fixed array of 32 bytes
type Bytes32 [32]byte

// MarshalJSON serializes ByteArray to hex
func (s *Bytes32) MarshalJSON() ([]byte, error) {
	b, err := json.Marshal(s.ToString())
	return b, err
}

// UnmarshalJSON serializes ByteArray to hex
func (s *Bytes32) UnmarshalJSON(data []byte) error {
	str := strings.Trim(string(data), `"`)
	return s.FromString(str)
}

// Bytes returns the bite slice of the 32 byte array
func (s *Bytes32) Bytes() []byte {
	return s[:]
}

// FromString takes a hex encoded string and sets the byte array.
// The input parameter, str, must be 64 hex characters in length
func (s *Bytes32) FromString(str string) error {
	if len(str) != 64 {
		return fmt.Errorf("expected 32 bytes string, received %s", str)
	}

	d, err := hex.DecodeString(str)
	if err != nil {
		return err
	}
	copy(s[:], d)
	return nil
}

// ToString will convert the 32 byte array into a hex string that is 64 hex characters in length
func (s *Bytes32) ToString() String {
	return String(hex.EncodeToString(s[:]))
}

// Bytes64 is a fixed array of 32 bytes
type Bytes64 [64]byte

// MarshalJSON serializes ByteArray to hex
func (s *Bytes64) MarshalJSON() ([]byte, error) {
	b, err := json.Marshal(s.ToString())
	return b, err
}

// UnmarshalJSON serializes ByteArray to hex
func (s *Bytes64) UnmarshalJSON(data []byte) error {
	str := strings.Trim(string(data), `"`)
	return s.FromString(str)
}

// Bytes returns the bite slice of the 32 byte array
func (s Bytes64) Bytes() []byte {
	return s[:]
}

// FromString takes a hex encoded string and sets the byte array.
// The input parameter, str, must be 64 hex characters in length
func (s *Bytes64) FromString(str string) error {
	if len(str) != 128 {
		return fmt.Errorf("expected 64 bytes string, received %s", str)
	}

	d, err := hex.DecodeString(str)
	if err != nil {
		return err
	}
	copy(s[:], d)
	return nil
}

// ToString will convert the 32 byte array into a hex string that is 64 hex characters in length
func (s *Bytes64) ToString() String {
	return String(hex.EncodeToString(s[:]))
}

type String string

func (s *String) MarshalBinary() ([]byte, error) {
	b := Bytes(*s)
	return b.MarshalBinary()
}

func (s *String) UnmarshalBinary(data []byte) error {
	var b Bytes
	err := b.UnmarshalBinary(data)
	if err == nil {
		*s = String(b)
	}
	return err
}

func (s *String) Size(varintbuf *[8]byte) int {
	b := Bytes(*s)
	return b.Size(varintbuf)
}

func (s *String) AsString() *string {
	return (*string)(s)
}

type Byte byte

func (s *Byte) MarshalBinary() ([]byte, error) {
	ret := make([]byte, 1)
	ret[0] = byte(*s)
	return ret, nil
}

func (s *Byte) UnmarshalBinary(data []byte) error {
	if len(data) < 1 {
		return fmt.Errorf("insufficient data length for unmarshal of a byte")
	}
	*s = Byte(data[0])
	return nil
}

type UrlAdi struct {
	String
}

func (s *UrlAdi) IsValid() bool {
	adi, _, err := s.Parse()
	return !(adi == "" || err != nil)
}

func (s *UrlAdi) Parse() (string, string, error) {
	return ParseIdentityChainPath(s.AsString())
}

type UrlChain struct {
	String
}

func (s *UrlChain) IsValid() bool {
	adi, chainPath, err := s.Parse()
	return !(adi == "" || chainPath == "" || err != nil)
}

func (s *UrlChain) Parse() (string, string, error) {
	return ParseIdentityChainPath(s.AsString())
}

type Amount struct {
	big.Int
}

func (a *Amount) AsBigInt() *big.Int {
	return &a.Int
}

func (a *Amount) Size() int {
	return len(a.Int.Bytes()) + 1
}

func (a *Amount) MarshalBinary() ([]byte, error) {
	var buffer bytes.Buffer
	data := a.Int.Bytes()
	buffer.WriteByte(byte(len(data)))
	buffer.Write(data)
	return buffer.Bytes(), nil
}

func (a *Amount) UnmarshalBinary(data []byte) error {
	length := len(data)
	if length < 1 {
		return fmt.Errorf("insufficient data to unmarshal amount header, length == 0")
	}

	amtLen := int(data[0])
	if length < amtLen {
		return fmt.Errorf("insuffcient data to unmarshal amount, len = %d but provided %d", amtLen, length)
	}

	a.Int.SetBytes(data[1:amtLen])
	return nil
}