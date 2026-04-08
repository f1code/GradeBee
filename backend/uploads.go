// uploads.go provides helpers for saving uploaded files to the shared uploads directory.
package handler

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

// saveToUploadsDir writes data to the uploads directory with a unique filename
// built from a UUID and the given extension (e.g. ".pdf"). Returns the full
// disk path. Callers are responsible for cleanup on downstream failures.
func saveToUploadsDir(data []byte, ext string) (string, error) {
	uploadsDir := serviceDeps.GetUploadsDir()
	diskName := uuid.New().String() + ext
	diskPath := filepath.Join(uploadsDir, diskName)
	if err := os.WriteFile(diskPath, data, 0o644); err != nil {
		return "", fmt.Errorf("save to uploads dir: %w", err)
	}
	return diskPath, nil
}