package query

// GENERATED BY go run ./tools/cmd/gen-enum. DO NOT EDIT.

import (
	"encoding/json"
	"fmt"
	"strings"
)

// TxFetchModeExpand expand the full transactions in the result set.
const TxFetchModeExpand TxFetchMode = 0

// TxFetchModeIds include the transaction IDs & count in the result set.
const TxFetchModeIds TxFetchMode = 1

// TxFetchModeCountOnly only include the transaction count in the result set.
const TxFetchModeCountOnly TxFetchMode = 2

// TxFetchModeOmit omit all transaction info from the result set.
const TxFetchModeOmit TxFetchMode = 3

// GetEnumValue returns the value of the Tx Fetch Mode
func (v TxFetchMode) GetEnumValue() uint64 { return uint64(v) }

// SetEnumValue sets the value. SetEnumValue returns false if the value is invalid.
func (v *TxFetchMode) SetEnumValue(id uint64) bool {
	u := TxFetchMode(id)
	switch u {
	case TxFetchModeExpand, TxFetchModeIds, TxFetchModeCountOnly, TxFetchModeOmit:
		*v = u
		return true
	default:
		return false
	}
}

// String returns the name of the Tx Fetch Mode
func (v TxFetchMode) String() string {
	switch v {
	case TxFetchModeExpand:
		return "expand"
	case TxFetchModeIds:
		return "ids"
	case TxFetchModeCountOnly:
		return "countOnly"
	case TxFetchModeOmit:
		return "omit"
	default:
		return fmt.Sprintf("TxFetchMode:%d", v)
	}
}

// TxFetchModeByName returns the named Tx Fetch Mode.
func TxFetchModeByName(name string) (TxFetchMode, bool) {
	switch strings.ToLower(name) {
	case "expand":
		return TxFetchModeExpand, true
	case "ids":
		return TxFetchModeIds, true
	case "countonly":
		return TxFetchModeCountOnly, true
	case "omit":
		return TxFetchModeOmit, true
	default:
		return 0, false
	}
}

// MarshalJSON marshals the Tx Fetch Mode to JSON as a string.
func (v TxFetchMode) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.String())
}

// UnmarshalJSON unmarshals the Tx Fetch Mode from JSON as a string.
func (v *TxFetchMode) UnmarshalJSON(data []byte) error {
	var s string
	err := json.Unmarshal(data, &s)
	if err != nil {
		return err
	}

	var ok bool
	*v, ok = TxFetchModeByName(s)
	if !ok || strings.ContainsRune(v.String(), ':') {
		return fmt.Errorf("invalid Tx Fetch Mode %q", s)
	}
	return nil
}
