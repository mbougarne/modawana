package bundle

import (
	"archive/zip"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRejectTraversalDuplicateAndSchema(t *testing.T) {
	for _, name := range []string{"../escape", "/absolute", "a/../../escape", "a\\escape", "a//b", "manifest.json"} {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "bad.zip")
			f, _ := os.Create(p)
			z := zip.NewWriter(f)
			w, _ := z.Create(name)
			w.Write([]byte("x"))
			w, _ = z.Create("manifest.json")
			json.NewEncoder(w).Encode(Manifest{Schema: 99, Kind: "content", Files: []Entry{}})
			z.Close()
			f.Close()
			if root, _, e := Read(p, "content"); e == nil {
				os.RemoveAll(root)
				t.Fatal("unsafe archive accepted")
			}
		})
	}
}
func TestRejectChecksum(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "data.json"), []byte("data"), 0600)
	p := filepath.Join(t.TempDir(), "bundle.zip")
	f, _ := os.Create(p)
	z := zip.NewWriter(f)
	w, _ := z.Create("data.json")
	w.Write([]byte("data"))
	w, _ = z.Create("manifest.json")
	json.NewEncoder(w).Encode(Manifest{Schema: 1, Kind: "content", Files: []Entry{{Path: "data.json", Size: 4, SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}})
	z.Close()
	f.Close()
	if _, _, e := Read(p, "content"); e == nil {
		t.Fatal("corrupt bytes accepted")
	}
}
