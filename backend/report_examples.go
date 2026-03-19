// report_examples.go defines the ExampleStore interface and its Drive implementation
// for managing example report cards stored as plain text files.
package handler

import (
	"context"
	"fmt"
	"io"
	"strings"

	"google.golang.org/api/drive/v3"
)

// ReportExample represents a stored example report card.
type ReportExample struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Content string `json:"content,omitempty"`
}

// ExampleStore abstracts CRUD operations for example report cards.
type ExampleStore interface {
	ListExamples(ctx context.Context, examplesFolderID string) ([]ReportExample, error)
	ReadExample(ctx context.Context, fileID string) (*ReportExample, error)
	UploadExample(ctx context.Context, examplesFolderID, name, content string) (*ReportExample, error)
	DeleteExample(ctx context.Context, fileID string) error
}

// driveExampleStore implements ExampleStore using Google Drive.
type driveExampleStore struct {
	drive *drive.Service
}

func newDriveExampleStore(driveSvc *drive.Service) *driveExampleStore {
	return &driveExampleStore{drive: driveSvc}
}

func (s *driveExampleStore) ListExamples(ctx context.Context, examplesFolderID string) ([]ReportExample, error) {
	q := fmt.Sprintf("'%s' in parents and trashed = false", examplesFolderID)
	list, err := s.drive.Files.List().Q(q).Fields("files(id, name)").Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("report_examples: list: %w", err)
	}
	examples := make([]ReportExample, 0, len(list.Files))
	for _, f := range list.Files {
		examples = append(examples, ReportExample{ID: f.Id, Name: f.Name})
	}
	return examples, nil
}

func (s *driveExampleStore) ReadExample(ctx context.Context, fileID string) (*ReportExample, error) {
	file, err := s.drive.Files.Get(fileID).Fields("id, name").Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("report_examples: get file: %w", err)
	}
	resp, err := s.drive.Files.Get(fileID).Context(ctx).Download()
	if err != nil {
		return nil, fmt.Errorf("report_examples: download: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("report_examples: read content: %w", err)
	}
	return &ReportExample{
		ID:      file.Id,
		Name:    file.Name,
		Content: string(data),
	}, nil
}

func (s *driveExampleStore) UploadExample(ctx context.Context, examplesFolderID, name, content string) (*ReportExample, error) {
	f, err := s.drive.Files.Create(&drive.File{
		Name:    name,
		Parents: []string{examplesFolderID},
	}).Media(strings.NewReader(content)).Fields("id, name").Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("report_examples: upload: %w", err)
	}
	return &ReportExample{ID: f.Id, Name: f.Name}, nil
}

func (s *driveExampleStore) DeleteExample(ctx context.Context, fileID string) error {
	_, err := s.drive.Files.Update(fileID, &drive.File{Trashed: true}).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("report_examples: delete: %w", err)
	}
	return nil
}
