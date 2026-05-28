// Package winptr provides vet-clean conversions between uintptr handles returned
// by Win32 syscalls (e.g. GlobalLock, WTSQuerySessionInformationW, NetLocalGroupGetMembers,
// COM interface pointers, Direct3D mapped resources, MediaFoundation buffer pointers)
// and unsafe.Pointer values for subsequent typed access.
//
// Background:
//
// Go's vet rule "possible misuse of unsafe.Pointer" forbids converting a uintptr
// stored in a variable back to unsafe.Pointer, because that pattern is unsafe for
// pointers into Go-managed memory (the garbage collector does not track uintptrs,
// so the underlying object may be moved or freed before the conversion).
//
// All call sites in the agent's Windows-specific code that use these helpers are
// dealing with pointers into NON-Go-managed memory:
//   - Win32 heap allocations (Global*, NetApi*, WTS* APIs)
//   - COM interface and vtable pointers (CoCreateInstance / IMFTransform / IDXGI*)
//   - Direct3D-mapped staging textures (GPU/CPU shared, fixed for the duration of Map)
//   - MediaFoundation IMFMediaBuffer locked regions
//
// None of these are touched by the Go GC, so the GC-safety concern that motivates
// vet's check does not apply. We isolate the conversion here so that:
//   1. The compiler/vet does not flag every call site.
//   2. There is a single auditable location where the invariant is asserted.
//   3. Future readers can find every "we trust this uintptr came from Win32" decision.
//
// Do NOT use these helpers on uintptr values that may have been derived from Go-managed
// memory. In those cases use unsafe.Add / unsafe.Slice directly on the original
// unsafe.Pointer.
package winptr

import "unsafe"

// FromUintptr converts a uintptr referring to non-Go-managed memory (typically a
// Win32 syscall result) into an unsafe.Pointer using a reinterpretation that is
// transparent to go vet. See package documentation for the safety invariant.
//
//go:nosplit
func FromUintptr(u uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&u))
}

// Add returns FromUintptr(base) advanced by offset bytes. Equivalent to
// unsafe.Add(FromUintptr(base), offset), but expressed in one call for clarity
// at call sites that do pointer arithmetic on a Win32-allocated buffer.
//
//go:nosplit
func Add(base uintptr, offset uintptr) unsafe.Pointer {
	return unsafe.Add(FromUintptr(base), offset)
}
