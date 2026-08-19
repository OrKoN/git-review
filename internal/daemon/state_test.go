package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStateRoundTripAndOwnedRemoval(t *testing.T) {
	runtime := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", runtime)
	root := t.TempDir()
	path, err := StatePath(root)
	if err != nil {
		t.Fatal(err)
	}
	want := State{PID: os.Getpid(), ID: "instance", BaseURL: "http://127.0.0.1:1", Token: "token", HubURL: "http://hub"}
	if err := Write(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("state = %#v, want %#v", got, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("state mode = %o", info.Mode().Perm())
	}
	if err := Remove(path, "other"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("different instance removed state")
	}
	if err := Remove(path, want.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("state still exists: %v", err)
	}
}

func TestStatePathIsStableAndPrivate(t *testing.T) {
	runtime := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", runtime)
	root := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	first, err := StatePath(root)
	if err != nil {
		t.Fatal(err)
	}
	second, err := StatePath(alias)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("canonical paths differ: %q != %q", first, second)
	}
	info, err := os.Stat(filepath.Dir(first))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("runtime directory mode = %o", info.Mode().Perm())
	}
}

func TestAcquireSerializesCallers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state")
	first, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	acquired := make(chan *Lock, 1)
	go func() { lock, _ := Acquire(path); acquired <- lock }()
	select {
	case <-acquired:
		t.Fatal("second lock acquired too early")
	case <-time.After(30 * time.Millisecond):
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case second := <-acquired:
		if second == nil {
			t.Fatal("second lock failed")
		}
		_ = second.Close()
	case <-time.After(time.Second):
		t.Fatal("second lock did not acquire")
	}
}

func TestProcessAliveRecognizesCurrentProcess(t *testing.T) {
	if !ProcessAlive(os.Getpid()) {
		t.Fatal("current process reported dead")
	}
}
