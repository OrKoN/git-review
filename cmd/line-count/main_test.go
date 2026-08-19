package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCountTreeFindsGoAndTypeScriptSources(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "main.go", "package main\n\n")
	writeTestFile(t, root, "web/app.ts", "one\ntwo\nthree\n")
	writeTestFile(t, root, "web/style.css", "ignored\n")
	writeTestFile(t, root, "node_modules/package/index.ts", "ignored\n")

	results, err := countTree(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].path != "web/app.ts" || results[0].lines != 3 || results[1].lines != 2 {
		t.Fatalf("unexpected results: %#v", results)
	}
}

func TestSourceFileExtensions(t *testing.T) {
	for _, test := range []struct {
		path string
		want bool
	}{{"main.go", true}, {"app.ts", true}, {"app.tsx", false}, {"style.css", false}} {
		if got := sourceFile(test.path); got != test.want {
			t.Errorf("sourceFile(%q) = %v, want %v", test.path, got, test.want)
		}
	}
}

func writeTestFile(t *testing.T, root, name, contents string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
