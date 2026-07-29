package filetransfer

import (
	"os"
	"path/filepath"
	"testing"
)

// TestValidatePath_DeniedBaseMutating proves AG-H write: mutating operations
// into a protected directory are denied while reads are still permitted.
func TestValidatePath_DeniedBaseMutating(t *testing.T) {
	tmp := t.TempDir()
	protected := filepath.Join(tmp, "protected")
	if err := os.MkdirAll(protected, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	ft := &FileTransfer{
		allowedBases: []string{tmp},
		deniedBases:  []string{filepath.Clean(protected)},
	}

	if _, err := ft.validatePath(filepath.Join(protected, "f.txt"), "read_file"); err != nil {
		t.Errorf("read within protected dir should be allowed, got: %v", err)
	}
	if _, err := ft.validatePath(filepath.Join(protected, "f.txt"), "write_file"); err == nil {
		t.Errorf("SECURITY: write within protected dir must be denied")
	}
	if _, err := ft.validatePath(filepath.Join(protected, "sub"), "create_directory"); err == nil {
		t.Errorf("SECURITY: create_directory within protected dir must be denied")
	}
	if _, err := ft.validatePath(filepath.Join(tmp, "ok.txt"), "write_file"); err != nil {
		t.Errorf("write within allowed non-protected dir should pass, got: %v", err)
	}
}

// TestVerifyRealPathContainment proves the resolved-real-path containment logic
// (AG-H write): a real path inside a denied base or outside every allowed base
// is rejected.
func TestVerifyRealPathContainment(t *testing.T) {
	tmp := t.TempDir()
	protected := filepath.Join(tmp, "protected")

	// Inside allowed, outside denied -> OK.
	if err := verifyRealPathContainment(filepath.Join(tmp, "ok.txt"), []string{tmp}, []string{protected}); err != nil {
		t.Errorf("allowed non-protected path should pass, got: %v", err)
	}
	// Inside a denied base -> rejected.
	if err := verifyRealPathContainment(filepath.Join(protected, "x"), []string{tmp}, []string{protected}); err == nil {
		t.Errorf("SECURITY: path within denied base must be rejected")
	}
	// Outside every allowed base -> rejected.
	if err := verifyRealPathContainment(filepath.Join(tmp, "..", "elsewhere", "x"), []string{tmp}, nil); err == nil {
		t.Errorf("SECURITY: path outside allowed bases must be rejected")
	}
	// No allowed restriction, not denied -> OK.
	if err := verifyRealPathContainment(filepath.Join(tmp, "x"), nil, nil); err != nil {
		t.Errorf("unrestricted path should pass, got: %v", err)
	}
}

// TestOpenFileHardened_DeniedBase proves OpenFileHardened refuses to open a
// target whose real path resolves inside a denied base (AG-H write).
func TestOpenFileHardened_DeniedBase(t *testing.T) {
	tmp := t.TempDir()
	protected := filepath.Join(tmp, "protected")
	if err := os.MkdirAll(protected, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Allowed, non-protected path opens.
	f, err := OpenFileHardened(filepath.Join(tmp, "ok.txt"), false, []string{tmp}, []string{protected})
	if err != nil {
		t.Fatalf("expected open to succeed, got: %v", err)
	}
	f.Close()

	// Protected path is rejected.
	if f, err := OpenFileHardened(filepath.Join(protected, "evil.txt"), false, []string{tmp}, []string{protected}); err == nil {
		f.Close()
		t.Errorf("SECURITY: OpenFileHardened must reject a target inside a denied base")
	}
}

// TestIsSubPath_Boundary proves AG-M path: prefix matching is directory-boundary
// aware, so "allowed-evil" does not match base "allowed".
func TestIsSubPath_Boundary(t *testing.T) {
	base := filepath.Clean(filepath.Join("srv", "allowed"))
	if isSubPath(filepath.Clean(filepath.Join("srv", "allowed-evil", "x")), base) {
		t.Errorf("SECURITY: sibling 'allowed-evil' must not match base 'allowed'")
	}
	if !isSubPath(filepath.Clean(filepath.Join("srv", "allowed", "x")), base) {
		t.Errorf("legitimate child must match base")
	}
	if !isSubPath(base, base) {
		t.Errorf("exact base must match itself")
	}
}
