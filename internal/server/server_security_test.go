package server

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestWholeFileMutationRejectsStaleDiff(t *testing.T) {
	app, base, client, _ := liveServer(t)
	authenticateClient(t, app, base, client)
	diffResponse, err := client.Get(base + "/api/diff?path=file.txt&area=unstaged")
	if err != nil {
		t.Fatal(err)
	}
	var diff struct {
		Fingerprint string `json:"fingerprint"`
	}
	if err := json.NewDecoder(diffResponse.Body).Decode(&diff); err != nil {
		t.Fatal(err)
	}
	diffResponse.Body.Close()
	if err := os.WriteFile(filepath.Join(app.repo.Root(), "file.txt"), []byte("changed externally\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	response := requestJSON(t, client, "POST", base+"/api/change", map[string]any{"action": "stage", "path": "file.txt", "fingerprint": diff.Fingerprint})
	response.Body.Close()
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("stale mutation status = %d", response.StatusCode)
	}
	cmd := exec.Command("git", "-C", app.repo.Root(), "diff", "--cached", "--quiet")
	if err := cmd.Run(); err != nil {
		t.Fatal("stale mutation changed the index")
	}
}

func TestServerListensOnlyOnLoopback(t *testing.T) {
	app, _, _, _ := liveServer(t)
	if !app.listener.Addr().(*net.TCPAddr).IP.IsLoopback() {
		t.Fatalf("listener = %s", app.listener.Addr())
	}
}

func TestChooseLocalIPPrefersPrivateAddress(t *testing.T) {
	public := net.ParseIP("203.0.113.8")
	nat := net.ParseIP("10.0.2.15")
	lan := net.ParseIP("192.168.1.20")
	if got := chooseLocalIP([]net.IP{public, nat, lan}); !got.Equal(lan) {
		t.Fatalf("chooseLocalIP = %v", got)
	}
	if got := chooseLocalIP([]net.IP{public}); !got.Equal(public) {
		t.Fatalf("public fallback = %v", got)
	}
}

func TestLocateCommentRequiresUniqueMatchingLine(t *testing.T) {
	diff := []byte("@@ -10,2 +10,2 @@\n-old\n+new\n same\n")
	line, ok := locateComment(diff, "new", "+new")
	if !ok || line != 10 {
		t.Fatalf("locateComment = %d, %v", line, ok)
	}
	duplicate := []byte("@@ -1 +1,2 @@\n-a\n+same\n+same\n")
	if _, ok := locateComment(duplicate, "new", "+same"); ok {
		t.Fatal("ambiguous comment was treated as current")
	}
	if _, ok := locateComment(diff, "old", "+new"); ok {
		t.Fatal("comment matched the wrong diff side")
	}
	if line, ok := locateComment(diff, "new", " same"); !ok || line != 11 {
		t.Fatalf("context locateComment = %d, %v", line, ok)
	}
}

func TestLocateSourceLineRequiresUniqueContext(t *testing.T) {
	if line, ok := locateSourceLine("first\nsecond\nthird", "second"); !ok || line != 2 {
		t.Fatalf("locateSourceLine = %d, %v", line, ok)
	}
	if _, ok := locateSourceLine("same\nother\nsame", "same"); ok {
		t.Fatal("duplicate source context was accepted")
	}
}
