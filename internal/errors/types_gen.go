package errors

// GENERATED BY go run ./tools/cmd/gen-types. DO NOT EDIT.

import (
	"bytes"
	"errors"
	"io"
	"strings"

	"gitlab.com/accumulatenetwork/accumulate/internal/encoding"
)

type CallSite struct {
	fieldsSet []bool
	FuncName  string `json:"funcName,omitempty" form:"funcName" query:"funcName" validate:"required"`
	File      string `json:"file,omitempty" form:"file" query:"file" validate:"required"`
	Line      int64  `json:"line,omitempty" form:"line" query:"line" validate:"required"`
}

type Error struct {
	fieldsSet []bool
	Message   string    `json:"message,omitempty" form:"message" query:"message" validate:"required"`
	Code      Status    `json:"code,omitempty" form:"code" query:"code" validate:"required"`
	Cause     *Error    `json:"cause,omitempty" form:"cause" query:"cause" validate:"required"`
	CallSite  *CallSite `json:"callSite,omitempty" form:"callSite" query:"callSite" validate:"required"`
}

func (v *CallSite) Copy() *CallSite {
	u := new(CallSite)

	u.FuncName = v.FuncName
	u.File = v.File
	u.Line = v.Line

	return u
}

func (v *CallSite) CopyAsInterface() interface{} { return v.Copy() }

func (v *Error) Copy() *Error {
	u := new(Error)

	u.Message = v.Message
	u.Code = v.Code
	if v.Cause != nil {
		u.Cause = (v.Cause).Copy()
	}
	if v.CallSite != nil {
		u.CallSite = (v.CallSite).Copy()
	}

	return u
}

func (v *Error) CopyAsInterface() interface{} { return v.Copy() }

func (v *CallSite) Equal(u *CallSite) bool {
	if !(v.FuncName == u.FuncName) {
		return false
	}
	if !(v.File == u.File) {
		return false
	}
	if !(v.Line == u.Line) {
		return false
	}

	return true
}

func (v *Error) Equal(u *Error) bool {
	if !(v.Message == u.Message) {
		return false
	}
	if !(v.Code == u.Code) {
		return false
	}
	switch {
	case v.Cause == u.Cause:
		// equal
	case v.Cause == nil || u.Cause == nil:
		return false
	case !((v.Cause).Equal(u.Cause)):
		return false
	}
	switch {
	case v.CallSite == u.CallSite:
		// equal
	case v.CallSite == nil || u.CallSite == nil:
		return false
	case !((v.CallSite).Equal(u.CallSite)):
		return false
	}

	return true
}

var fieldNames_CallSite = []string{
	1: "FuncName",
	2: "File",
	3: "Line",
}

func (v *CallSite) MarshalBinary() ([]byte, error) {
	buffer := new(bytes.Buffer)
	writer := encoding.NewWriter(buffer)

	if !(len(v.FuncName) == 0) {
		writer.WriteString(1, v.FuncName)
	}
	if !(len(v.File) == 0) {
		writer.WriteString(2, v.File)
	}
	if !(v.Line == 0) {
		writer.WriteInt(3, v.Line)
	}

	_, _, err := writer.Reset(fieldNames_CallSite)
	return buffer.Bytes(), err
}

func (v *CallSite) IsValid() error {
	var errs []string

	if len(v.fieldsSet) > 1 && !v.fieldsSet[1] {
		errs = append(errs, "field FuncName is missing")
	} else if len(v.FuncName) == 0 {
		errs = append(errs, "field FuncName is not set")
	}
	if len(v.fieldsSet) > 2 && !v.fieldsSet[2] {
		errs = append(errs, "field File is missing")
	} else if len(v.File) == 0 {
		errs = append(errs, "field File is not set")
	}
	if len(v.fieldsSet) > 3 && !v.fieldsSet[3] {
		errs = append(errs, "field Line is missing")
	} else if v.Line == 0 {
		errs = append(errs, "field Line is not set")
	}

	switch len(errs) {
	case 0:
		return nil
	case 1:
		return errors.New(errs[0])
	default:
		return errors.New(strings.Join(errs, "; "))
	}
}

var fieldNames_Error = []string{
	1: "Message",
	2: "Code",
	3: "Cause",
	4: "CallSite",
}

func (v *Error) MarshalBinary() ([]byte, error) {
	buffer := new(bytes.Buffer)
	writer := encoding.NewWriter(buffer)

	if !(len(v.Message) == 0) {
		writer.WriteString(1, v.Message)
	}
	if !(v.Code == 0) {
		writer.WriteEnum(2, v.Code)
	}
	if !(v.Cause == nil) {
		writer.WriteValue(3, v.Cause)
	}
	if !(v.CallSite == nil) {
		writer.WriteValue(4, v.CallSite)
	}

	_, _, err := writer.Reset(fieldNames_Error)
	return buffer.Bytes(), err
}

func (v *Error) IsValid() error {
	var errs []string

	if len(v.fieldsSet) > 1 && !v.fieldsSet[1] {
		errs = append(errs, "field Message is missing")
	} else if len(v.Message) == 0 {
		errs = append(errs, "field Message is not set")
	}
	if len(v.fieldsSet) > 2 && !v.fieldsSet[2] {
		errs = append(errs, "field Code is missing")
	} else if v.Code == 0 {
		errs = append(errs, "field Code is not set")
	}
	if len(v.fieldsSet) > 3 && !v.fieldsSet[3] {
		errs = append(errs, "field Cause is missing")
	} else if v.Cause == nil {
		errs = append(errs, "field Cause is not set")
	}
	if len(v.fieldsSet) > 4 && !v.fieldsSet[4] {
		errs = append(errs, "field CallSite is missing")
	} else if v.CallSite == nil {
		errs = append(errs, "field CallSite is not set")
	}

	switch len(errs) {
	case 0:
		return nil
	case 1:
		return errors.New(errs[0])
	default:
		return errors.New(strings.Join(errs, "; "))
	}
}

func (v *CallSite) UnmarshalBinary(data []byte) error {
	return v.UnmarshalBinaryFrom(bytes.NewReader(data))
}

func (v *CallSite) UnmarshalBinaryFrom(rd io.Reader) error {
	reader := encoding.NewReader(rd)

	if x, ok := reader.ReadString(1); ok {
		v.FuncName = x
	}
	if x, ok := reader.ReadString(2); ok {
		v.File = x
	}
	if x, ok := reader.ReadInt(3); ok {
		v.Line = x
	}

	seen, err := reader.Reset(fieldNames_CallSite)
	v.fieldsSet = seen
	return err
}

func (v *Error) UnmarshalBinary(data []byte) error {
	return v.UnmarshalBinaryFrom(bytes.NewReader(data))
}

func (v *Error) UnmarshalBinaryFrom(rd io.Reader) error {
	reader := encoding.NewReader(rd)

	if x, ok := reader.ReadString(1); ok {
		v.Message = x
	}
	if x := new(Status); reader.ReadEnum(2, x) {
		v.Code = *x
	}
	if x := new(Error); reader.ReadValue(3, x.UnmarshalBinary) {
		v.Cause = x
	}
	if x := new(CallSite); reader.ReadValue(4, x.UnmarshalBinary) {
		v.CallSite = x
	}

	seen, err := reader.Reset(fieldNames_Error)
	v.fieldsSet = seen
	return err
}
