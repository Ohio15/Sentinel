//go:build windows

package crypto

import (
	"bytes"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// DPAPI machine-scope sealing for secret material at rest (mTLS private key,
// config machine secret). CRYPTPROTECT_LOCAL_MACHINE ties the blob to the
// machine rather than a user profile, which is required because the agent runs
// as SYSTEM and secrets must survive across the accounts that operate it.
//
// golang.org/x/sys/windows does not wrap CryptProtectData/CryptUnprotectData,
// so we bind them directly from crypt32.dll.
var (
	modCrypt32       = windows.NewLazySystemDLL("crypt32.dll")
	modKernel32      = windows.NewLazySystemDLL("kernel32.dll")
	procCryptProtect = modCrypt32.NewProc("CryptProtectData")
	procCryptUnprot  = modCrypt32.NewProc("CryptUnprotectData")
	procLocalFree    = modKernel32.NewProc("LocalFree")
)

const cryptProtectLocalMachine = 0x4

// dataBlob mirrors the Win32 DATA_BLOB structure.
type dataBlob struct {
	cbData uint32
	pbData *byte
}

func newBlob(d []byte) dataBlob {
	if len(d) == 0 {
		return dataBlob{}
	}
	return dataBlob{cbData: uint32(len(d)), pbData: &d[0]}
}

// toBytes copies the blob's contents into a Go-managed slice.
func (b dataBlob) toBytes() []byte {
	if b.pbData == nil || b.cbData == 0 {
		return nil
	}
	out := make([]byte, b.cbData)
	copy(out, unsafe.Slice(b.pbData, b.cbData))
	return out
}

// SealMachineData encrypts plaintext with DPAPI machine scope and returns the
// blob prefixed with sealMagic so it is self-describing on unseal.
func SealMachineData(plaintext []byte) ([]byte, error) {
	in := newBlob(plaintext)
	var out dataBlob

	r, _, callErr := procCryptProtect.Call(
		uintptr(unsafe.Pointer(&in)),
		0, // szDataDescr
		0, // pOptionalEntropy
		0, // pvReserved
		0, // pPromptStruct
		uintptr(cryptProtectLocalMachine),
		uintptr(unsafe.Pointer(&out)),
	)
	if r == 0 {
		return nil, fmt.Errorf("CryptProtectData failed: %v", callErr)
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))

	sealed := out.toBytes()
	result := make([]byte, 0, len(sealMagic)+len(sealed))
	result = append(result, []byte(sealMagic)...)
	result = append(result, sealed...)
	return result, nil
}

// UnsealMachineData reverses SealMachineData. Data without the sealMagic prefix
// is treated as legacy plaintext and returned unchanged so pre-sealing files
// (e.g. an existing client.key or machine secret) still load and can be
// re-sealed on the next write.
func UnsealMachineData(data []byte) ([]byte, error) {
	if !bytes.HasPrefix(data, []byte(sealMagic)) {
		return data, nil
	}
	blob := data[len(sealMagic):]

	in := newBlob(blob)
	var out dataBlob

	r, _, callErr := procCryptUnprot.Call(
		uintptr(unsafe.Pointer(&in)),
		0, // ppszDataDescr
		0, // pOptionalEntropy
		0, // pvReserved
		0, // pPromptStruct
		0, // dwFlags
		uintptr(unsafe.Pointer(&out)),
	)
	if r == 0 {
		return nil, fmt.Errorf("CryptUnprotectData failed: %v", callErr)
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))

	return out.toBytes(), nil
}

// IsSealed reports whether data carries the DPAPI seal prefix.
func IsSealed(data []byte) bool {
	return bytes.HasPrefix(data, []byte(sealMagic))
}
