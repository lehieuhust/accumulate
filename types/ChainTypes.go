package types

type ChainType uint64

//ChainType enumeration order matters, do not change order or insert new enums.
const (
	ChainTypeUnknown          = ChainType(iota)
	ChainTypeDC               // Directory Chain
	ChainTypeBVC              // Block Validator Chain
	ChainTypeAdi              // Accumulate Digital/Distributed Identity/Identifier/Domain
	ChainTypeToken            // Token Issue
	ChainTypeTokenAccount     // Token Account
	ChainTypeAnonTokenAccount // Anonymous Token Account
	ChainTypeTransaction      // Pending Chain
	ChainTypeSignatureGroup   // Signature Group chain
)

// Enum value maps for ChainType.
var (
	ChainTypeName = map[ChainType]string{
		ChainTypeUnknown:          "ChainTypeUnknown",
		ChainTypeDC:               "ChainTypeDC",
		ChainTypeBVC:              "ChainTypeBVC",
		ChainTypeAdi:              "ChainTypeAdi",
		ChainTypeToken:            "ChainTypeToken",
		ChainTypeTokenAccount:     "ChainTypeTokenAccount",
		ChainTypeAnonTokenAccount: "ChainTypeAnonTokenAccount",
		ChainTypeTransaction:      "ChainTypeTransaction",
		ChainTypeSignatureGroup:   "ChainTypeSignatureGroup",
	}
	ChainTypeValue = map[string]ChainType{
		"ChainTypeUnknown":          ChainTypeUnknown,
		"ChainTypeDC":               ChainTypeDC,
		"ChainTypeBVC":              ChainTypeBVC,
		"ChainTypeAdi":              ChainTypeAdi,
		"ChainTypeToken":            ChainTypeToken,
		"ChainTypeTokenAccount":     ChainTypeTokenAccount,
		"ChainTypeAnonTokenAccount": ChainTypeAnonTokenAccount,
		"ChainTypeTransaction":      ChainTypeTransaction,
		"ChainTypeSignatureGroup":   ChainTypeSignatureGroup,
	}
)

//Name will return the name of the type
func (t ChainType) Name() string {
	if name := ChainTypeName[t]; name != "" {
		return name
	}
	return ChainTypeUnknown.Name()
}

//SetType will set the type based on the string name submitted
func (t *ChainType) SetType(s string) {
	*t = ChainTypeValue[s]
}

//AsUint64 casts as a uint64
func (t ChainType) AsUint64() uint64 {
	return uint64(t)
}