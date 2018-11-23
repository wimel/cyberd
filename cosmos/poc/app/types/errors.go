package types

import (
	"fmt"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	// Base error codes
	CodeOK               sdk.CodeType = 0
	CodeLinkAlreadyExist sdk.CodeType = 1
	CodeInvalidCid       sdk.CodeType = 2
	CodeCidNotFound      sdk.CodeType = 3

	// Code space
	CodespaceCBD sdk.CodespaceType = 42
)

func codeToDefaultMsg(code sdk.CodeType) string {
	switch code {
	case CodeInvalidCid:
		return "invalid cid"
	case CodeCidNotFound:
		return "cid not found"
	case CodeLinkAlreadyExist:
		return "link already exists"
	default:
		return fmt.Sprintf("unknown error: code %d", code)
	}
}

//----------------------------------------
// Code constructors

func LinkAlreadyExistsCode() sdk.ABCICodeType {
	return sdk.ToABCICode(CodespaceCBD, CodeLinkAlreadyExist)
}

//----------------------------------------
// Error constructors

func ErrInvalidCid() sdk.Error {
	return newError(CodespaceCBD, CodeInvalidCid)
}

func ErrCidNotFound() sdk.Error {
	return newError(CodespaceCBD, CodeCidNotFound)
}

func newError(codespace sdk.CodespaceType, code sdk.CodeType) sdk.Error {
	msg := codeToDefaultMsg(code)
	return sdk.NewError(codespace, code, msg)
}