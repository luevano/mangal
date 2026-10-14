package afs

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWriteFileAtomic(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "sub", "a.toml")
	if err := WriteFileAtomic(file, []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(file); string(b) != "one" {
		t.Errorf("content %q", b)
	}
	if runtime.GOOS != "windows" {
		if info, _ := os.Stat(file); info.Mode().Perm() != 0o600 {
			t.Errorf("new file mode %v", info.Mode().Perm())
		}
		if err := os.Chmod(file, 0o640); err != nil {
			t.Fatal(err)
		}
	}
	if err := WriteFileAtomic(file, []byte("two"), 0o600); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(file); string(b) != "two" {
		t.Errorf("content %q", b)
	}
	if runtime.GOOS != "windows" {
		if info, _ := os.Stat(file); info.Mode().Perm() != 0o640 {
			t.Errorf("kept mode %v", info.Mode().Perm())
		}
		link := filepath.Join(dir, "link.toml")
		if err := os.Symlink(file, link); err != nil {
			t.Fatal(err)
		}
		if err := WriteFileAtomic(link, []byte("three"), 0o600); err != nil {
			t.Fatal(err)
		}
		if info, _ := os.Lstat(link); info.Mode()&os.ModeSymlink == 0 {
			t.Error("the symlink was replaced")
		}
		if b, _ := os.ReadFile(file); string(b) != "three" {
			t.Errorf("target content %q", b)
		}
	}
	if entries, _ := os.ReadDir(filepath.Dir(file)); len(entries) != 1 {
		t.Errorf("temporary files left: %v", entries)
	}
}
