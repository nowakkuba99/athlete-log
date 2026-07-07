package source

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteBlob_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	content := []byte(`{"activity":"test"}`)

	key, hash, err := WriteBlob(dir, content)
	if err != nil {
		t.Fatalf("WriteBlob() error = %v", err)
	}
	if key == "" || hash == "" {
		t.Fatal("WriteBlob() returned empty key or hash")
	}

	fullPath := filepath.Join(dir, key)
	got, err := os.ReadFile(fullPath)
	if err != nil {
		t.Fatalf("file not created at %s: %v", fullPath, err)
	}
	if string(got) != string(content) {
		t.Fatalf("file content = %q, want %q", got, content)
	}
}

func TestWriteBlob_Idempotent(t *testing.T) {
	dir := t.TempDir()
	content := []byte(`{"activity":"test"}`)

	key1, hash1, err := WriteBlob(dir, content)
	if err != nil {
		t.Fatal(err)
	}
	key2, hash2, err := WriteBlob(dir, content)
	if err != nil {
		t.Fatal(err)
	}
	if key1 != key2 || hash1 != hash2 {
		t.Fatalf("WriteBlob() not idempotent: got (%s,%s) then (%s,%s)", key1, hash1, key2, hash2)
	}
}

func TestWriteBlob_EmptyDataDir_SkipsWrite(t *testing.T) {
	key, hash, err := WriteBlob("", []byte("data"))
	if err != nil {
		t.Fatalf("WriteBlob() error = %v", err)
	}
	if key != "" {
		t.Fatalf("WriteBlob() key = %q, want empty when dataDir is empty", key)
	}
	if hash == "" {
		t.Fatal("WriteBlob() hash should not be empty")
	}
}

func TestWriteBlobRaw_CreatesFileWithExtension(t *testing.T) {
	dir := t.TempDir()
	content := []byte("FIT binary data")

	key, hash, err := WriteBlobRaw(dir, content, ".fit")
	if err != nil {
		t.Fatalf("WriteBlobRaw() error = %v", err)
	}
	if filepath.Ext(key) != ".fit" {
		t.Fatalf("WriteBlobRaw() key extension = %q, want .fit", filepath.Ext(key))
	}
	if hash == "" {
		t.Fatal("WriteBlobRaw() returned empty hash")
	}

	fullPath := filepath.Join(dir, key)
	if _, err := os.Stat(fullPath); err != nil {
		t.Fatalf("file not created at %s: %v", fullPath, err)
	}
}

func TestWriteBlobRaw_DifferentExtensions_DifferentKeys(t *testing.T) {
	dir := t.TempDir()
	content := []byte("same content")

	key1, _, _ := WriteBlobRaw(dir, content, ".fit")
	key2, _, _ := WriteBlobRaw(dir, content, ".gpx")

	if key1 == key2 {
		t.Fatal("WriteBlobRaw() should produce different keys for different extensions")
	}
}
