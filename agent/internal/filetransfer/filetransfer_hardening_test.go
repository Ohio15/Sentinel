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
