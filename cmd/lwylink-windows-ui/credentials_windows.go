//go:build windows

package main

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

type dataBlob struct {
	cbData uint32
	pbData *byte
}

var (
	crypt32                = windows.NewLazySystemDLL("crypt32.dll")
	kernel32               = windows.NewLazySystemDLL("kernel32.dll")
	procCryptProtectData   = crypt32.NewProc("CryptProtectData")
	procCryptUnprotectData = crypt32.NewProc("CryptUnprotectData")
	procLocalFree          = kernel32.NewProc("LocalFree")
)

func protectCredential(plain []byte) ([]byte, error) {
	return cryptProtectData(plain, procCryptProtectData)
}

func unprotectCredential(protected []byte) ([]byte, error) {
	return cryptProtectData(protected, procCryptUnprotectData)
}

func cryptProtectData(input []byte, proc *windows.LazyProc) ([]byte, error) {
	in := dataBlob{cbData: uint32(len(input))}
	if len(input) > 0 {
		in.pbData = &input[0]
	}
	var out dataBlob
	r, _, err := proc.Call(
		uintptr(unsafe.Pointer(&in)),
		0,
		0,
		0,
		0,
		0,
		uintptr(unsafe.Pointer(&out)),
	)
	if r == 0 {
		if err != windows.ERROR_SUCCESS {
			return nil, err
		}
		return nil, fmt.Errorf("windows credential protection failed")
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	if out.cbData == 0 || out.pbData == nil {
		return nil, nil
	}
	return append([]byte(nil), unsafe.Slice(out.pbData, int(out.cbData))...), nil
}
