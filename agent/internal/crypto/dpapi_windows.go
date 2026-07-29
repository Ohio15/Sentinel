//go:build windows

package crypto

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/sentinel/agent/internal/ipc"
)

// DPAPI machine-scope sealing for secret material at rest (mTLS private key,
// config machine secret). CRYPTPROTECT_LOCAL_MACHINE ties the blob to the
// machine rather than a user profile, which is required because the agent runs
// as SYSTEM and secrets must survive across the accounts that operate it.
//
// SECURITY MODEL (honest): machine-scope DPAPI with NO secondary entropy is
// recoverable by ANY local process — it is not confidentiality against a local
// user. Same-host confidentiality here rests on the file DACL + SYSTEM ownership
// + read-time verification (VerifyFileSecurity). To make DPAPI more than a
// stolen-disk defense-in-depth, we pass a per-install 32-byte random secondary
// entropy stored in its own SYSTEM+Administrators-only DACL file: a
// non-privileged local process cannot read that entropy, so it cannot unseal a
// copied blob even by calling CryptUnprotectData itself.
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

const (
	dpapiEntropyFileName = "dpapi-entropy.dat"
	dpapiEntropySize     = 32
)

var (
	dpapiEntropy     []byte
	dpapiEntropyOnce sync.Once
	dpapiEntropyErr  error
)

// dpapiEntropyPath returns the path to the per-install DPAPI secondary entropy.
func dpapiEntropyPath() string {
	return filepath.Join(KeyStoreDir(), dpapiEntropyFileName)
}

// loadOrCreateDPAPIEntropy loads the per-install secondary entropy, verifying
// its DACL/ownership before trusting it, or generates and persists a fresh one
// behind a SYSTEM+Administrators-only DACL. If verification fails the file is
// treated as planted and regenerated (blobs sealed under the old entropy become
// unrecoverable, which cascades to secret regeneration in the callers).
func loadOrCreateDPAPIEntropy() ([]byte, error) {
	path := dpapiEntropyPath()

	if raw, err := os.ReadFile(path); err == nil && len(raw) == dpapiEntropySize {
		if verr := ipc.VerifyFileSecurity(path); verr != nil {
			log.Printf("[crypto] DPAPI entropy file failed security verification, regenerating: %v", verr)
		} else {
			return raw, nil
		}
	}

	ent := make([]byte, dpapiEntropySize)
	if _, err := rand.Read(ent); err != nil {
		return nil, fmt.Errorf("failed to generate DPAPI entropy: %w", err)
	}
	if err := ipc.EnsureSecureDir(KeyStoreDir(), 0700); err != nil {
		return nil, fmt.Errorf("failed to prepare key store directory for DPAPI entropy: %w", err)
	}
	if err := ipc.SecureWriteFileStrict(path, ent, 0600); err != nil {
		return nil, fmt.Errorf("failed to persist DPAPI entropy: %w", err)
	}
	return ent, nil
}

// getDPAPIEntropy returns the cached per-install secondary entropy.
func getDPAPIEntropy() ([]byte, error) {
	dpapiEntropyOnce.Do(func() {
		dpapiEntropy, dpapiEntropyErr = loadOrCreateDPAPIEntropy()
	})
	return dpapiEntropy, dpapiEntropyErr
}

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
	entropy, err := getDPAPIEntropy()
	if err != nil {
		return nil, err
	}
	in := newBlob(plaintext)
	entBlob := newBlob(entropy)
	var out dataBlob

	r, _, callErr := procCryptProtect.Call(
		uintptr(unsafe.Pointer(&in)),
		0,                                   // szDataDescr
		uintptr(unsafe.Pointer(&entBlob)),   // pOptionalEntropy
		0,                                   // pvReserved
		0,                                   // pPromptStruct
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

	entropy, err := getDPAPIEntropy()
	if err != nil {
		return nil, err
	}
	in := newBlob(blob)
	entBlob := newBlob(entropy)
	var out dataBlob

	r, _, callErr := procCryptUnprot.Call(
		uintptr(unsafe.Pointer(&in)),
		0,                                 // ppszDataDescr
		uintptr(unsafe.Pointer(&entBlob)), // pOptionalEntropy
		0,                                 // pvReserved
		0,                                 // pPromptStruct
		0,                                 // dwFlags
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
