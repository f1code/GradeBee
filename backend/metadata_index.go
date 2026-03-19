// metadata_index.go implements a JSON-based metadata index for student notes
// stored in Google Drive. Each student gets an index.json file that tracks
// note document IDs, dates, and summaries for efficient lookup.
package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"google.golang.org/api/drive/v3"
)

// IndexEntry represents a single note in the metadata index.
type IndexEntry struct {
	DocID   string `json:"docId"`
	Date    string `json:"date"`
	Summary string `json:"summary"`
}

// StudentIndex is the top-level structure of an index.json file.
type StudentIndex struct {
	Entries []IndexEntry `json:"entries"`
}

// MetadataIndex abstracts read/write access to per-student note indexes.
type MetadataIndex interface {
	ReadIndex(ctx context.Context, notesRootID, class, student string) (*StudentIndex, error)
	AppendEntry(ctx context.Context, notesRootID, class, student string, entry IndexEntry) error
}

// driveMetadataIndex implements MetadataIndex using Google Drive.
type driveMetadataIndex struct {
	drive *drive.Service
}

func newDriveMetadataIndex(driveSvc *drive.Service) *driveMetadataIndex {
	return &driveMetadataIndex{drive: driveSvc}
}

func (m *driveMetadataIndex) ReadIndex(ctx context.Context, notesRootID, class, student string) (*StudentIndex, error) {
	folderID, err := m.resolveStudentFolder(ctx, notesRootID, class, student)
	if err != nil {
		return nil, err
	}
	if folderID == "" {
		return &StudentIndex{}, nil
	}

	fileID, err := m.findIndexFile(ctx, folderID)
	if err != nil {
		return nil, err
	}
	if fileID == "" {
		return &StudentIndex{}, nil
	}

	resp, err := m.drive.Files.Get(fileID).Context(ctx).Download()
	if err != nil {
		return nil, fmt.Errorf("metadata_index: download index.json: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("metadata_index: read index.json: %w", err)
	}

	var idx StudentIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("metadata_index: parse index.json: %w", err)
	}
	return &idx, nil
}

func (m *driveMetadataIndex) AppendEntry(ctx context.Context, notesRootID, class, student string, entry IndexEntry) error {
	// Ensure class/student folder hierarchy exists.
	classFolderID, err := findOrCreateDriveFolder(ctx, m.drive, notesRootID, class)
	if err != nil {
		return fmt.Errorf("metadata_index: create class folder: %w", err)
	}
	studentFolderID, err := findOrCreateDriveFolder(ctx, m.drive, classFolderID, student)
	if err != nil {
		return fmt.Errorf("metadata_index: create student folder: %w", err)
	}

	// Read existing index or create new.
	idx := &StudentIndex{}
	fileID, err := m.findIndexFile(ctx, studentFolderID)
	if err != nil {
		return err
	}
	if fileID != "" {
		existing, err := m.ReadIndex(ctx, notesRootID, class, student)
		if err != nil {
			return err
		}
		idx = existing
	}

	idx.Entries = append(idx.Entries, entry)

	data, err := json.Marshal(idx)
	if err != nil {
		return fmt.Errorf("metadata_index: marshal index: %w", err)
	}

	if fileID != "" {
		// Update existing file.
		_, err = m.drive.Files.Update(fileID, &drive.File{}).
			Media(bytes.NewReader(data)).
			Context(ctx).Do()
	} else {
		// Create new index.json.
		_, err = m.drive.Files.Create(&drive.File{
			Name:    "index.json",
			Parents: []string{studentFolderID},
		}).Media(bytes.NewReader(data)).Context(ctx).Do()
	}
	if err != nil {
		return fmt.Errorf("metadata_index: write index.json: %w", err)
	}
	return nil
}

// resolveStudentFolder finds the class/student folder without creating it. Returns "" if not found.
func (m *driveMetadataIndex) resolveStudentFolder(ctx context.Context, notesRootID, class, student string) (string, error) {
	classFolderID, err := findDriveFolder(ctx, m.drive, notesRootID, class)
	if err != nil || classFolderID == "" {
		return "", err
	}
	return findDriveFolder(ctx, m.drive, classFolderID, student)
}

func (m *driveMetadataIndex) findIndexFile(ctx context.Context, folderID string) (string, error) {
	q := fmt.Sprintf("'%s' in parents and name = 'index.json' and trashed = false", folderID)
	list, err := m.drive.Files.List().Q(q).Fields("files(id)").Context(ctx).Do()
	if err != nil {
		return "", fmt.Errorf("metadata_index: list index.json: %w", err)
	}
	if len(list.Files) > 0 {
		return list.Files[0].Id, nil
	}
	return "", nil
}

// findDriveFolder looks for a subfolder by name under parentID. Returns "" if not found.
func findDriveFolder(ctx context.Context, driveSvc *drive.Service, parentID, name string) (string, error) {
	q := fmt.Sprintf("'%s' in parents and name = '%s' and mimeType = 'application/vnd.google-apps.folder' and trashed = false", parentID, name)
	list, err := driveSvc.Files.List().Q(q).Fields("files(id)").Context(ctx).Do()
	if err != nil {
		return "", err
	}
	if len(list.Files) > 0 {
		return list.Files[0].Id, nil
	}
	return "", nil
}

// findOrCreateDriveFolder looks for a subfolder by name under parentID, creating it if not found.
// This is the shared utility extracted from notes.go.
func findOrCreateDriveFolder(ctx context.Context, driveSvc *drive.Service, parentID, name string) (string, error) {
	q := fmt.Sprintf("'%s' in parents and name = '%s' and mimeType = 'application/vnd.google-apps.folder' and trashed = false", parentID, name)
	list, err := driveSvc.Files.List().Q(q).Fields("files(id)").Context(ctx).Do()
	if err != nil {
		return "", err
	}
	if len(list.Files) > 0 {
		return list.Files[0].Id, nil
	}
	folder, err := driveSvc.Files.Create(&drive.File{
		Name:     name,
		MimeType: "application/vnd.google-apps.folder",
		Parents:  []string{parentID},
	}).Fields("id").Context(ctx).Do()
	if err != nil {
		return "", err
	}
	return folder.Id, nil
}
