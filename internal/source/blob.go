package source

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
)

// WriteBlobRaw stores content under dataDir/blobs/<sha256><ext> (idempotent by hash).
// Use this for binary files (FIT, GPX, TCX) where the extension matters.
func WriteBlobRaw(dataDir string, content []byte, ext string) (key, sha256hex string, err error) {
	sum := sha256.Sum256(content)
	sha256hex = hex.EncodeToString(sum[:])
	if dataDir == "" {
		return "", sha256hex, nil
	}
	key = filepath.Join("blobs", sha256hex+ext)
	fullPath := filepath.Join(dataDir, key)
	if mkErr := os.MkdirAll(filepath.Dir(fullPath), 0755); mkErr != nil {
		return "", sha256hex, mkErr
	}
	if _, statErr := os.Stat(fullPath); os.IsNotExist(statErr) {
		if writeErr := os.WriteFile(fullPath, content, 0644); writeErr != nil {
			return "", sha256hex, writeErr
		}
	}
	return key, sha256hex, nil
}

// WriteBlob stores content under dataDir/blobs/<sha256>.json (idempotent by hash).
// Returns the storage key and SHA256 hex. If dataDir is empty the blob is skipped
// and only the hash is returned.
func WriteBlob(dataDir string, content []byte) (key, sha256hex string, err error) {
	sum := sha256.Sum256(content)
	sha256hex = hex.EncodeToString(sum[:])
	if dataDir == "" {
		return "", sha256hex, nil
	}
	key = filepath.Join("blobs", sha256hex+".json")
	fullPath := filepath.Join(dataDir, key)
	if mkErr := os.MkdirAll(filepath.Dir(fullPath), 0755); mkErr != nil {
		return "", sha256hex, mkErr
	}
	if _, statErr := os.Stat(fullPath); os.IsNotExist(statErr) {
		if writeErr := os.WriteFile(fullPath, content, 0644); writeErr != nil {
			return "", sha256hex, writeErr
		}
	}
	return key, sha256hex, nil
}
