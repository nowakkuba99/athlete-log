package upload

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"athlete-log/internal/domain"
)

// Parse parses uploaded file content into a SourceActivity.
// The Ref fields (Source, SourceConnectionID, SourceActivityID, blobs) are NOT set — the caller sets those.
func Parse(ctx context.Context, data []byte, filename string) (domain.SourceActivity, error) {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".fit":
		return ParseFIT(ctx, bytes.NewReader(data))
	case ".gpx":
		return ParseGPX(ctx, bytes.NewReader(data))
	case ".tcx":
		return ParseTCX(ctx, bytes.NewReader(data))
	default:
		return domain.SourceActivity{}, fmt.Errorf("unsupported file type %q; accepted: .fit, .gpx, .tcx", filepath.Ext(filename))
	}
}

// SourceTypeForFilename returns the SourceType for an uploaded file based on extension.
func SourceTypeForFilename(filename string) (domain.SourceType, error) {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".fit":
		return domain.SourceFITUpload, nil
	case ".gpx":
		return domain.SourceGPXUpload, nil
	case ".tcx":
		return domain.SourceTCXUpload, nil
	default:
		return "", fmt.Errorf("unsupported file type %q; accepted: .fit, .gpx, .tcx", filepath.Ext(filename))
	}
}
