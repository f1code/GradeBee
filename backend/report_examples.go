// report_examples.go defines the ExampleStore interface and its DB-backed
// implementation for managing example report cards.
package handler

import (
	"context"
	"fmt"
)

// ReportExample represents a stored example report card.
type ReportExample struct {
	ID         int64    `json:"id"`
	Name       string   `json:"name"`
	Content    string   `json:"content"`
	Status     string   `json:"status"` // "ready", "processing", "failed"
	LevelNames []string `json:"levelNames"`
}

// ExampleStore abstracts CRUD operations for example report cards.
type ExampleStore interface {
	ListExamples(ctx context.Context, userID string) ([]ReportExample, error)
	UploadExample(ctx context.Context, userID, name, content string, levelNames []string) (*ReportExample, error)
	CreatePendingExample(ctx context.Context, userID, name, filePath string, levelNames []string) (*ReportExample, error)
	UpdateExampleStatus(ctx context.Context, id int64, status, content string) error
	UpdateExample(ctx context.Context, userID string, id int64, name, content string, levelNames []string) (*ReportExample, error)
	DeleteExample(ctx context.Context, userID string, id int64) error
}

// dbExampleStore implements ExampleStore using the DB repo.
type dbExampleStore struct {
	repo *ReportExampleRepo
}

func newDBExampleStore(r *ReportExampleRepo) *dbExampleStore {
	return &dbExampleStore{repo: r}
}

func (s *dbExampleStore) ListExamples(ctx context.Context, userID string) ([]ReportExample, error) {
	dbExamples, err := s.repo.List(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("report_examples: list: %w", err)
	}
	examples := make([]ReportExample, len(dbExamples))
	for i, e := range dbExamples {
		lns, err2 := s.repo.GetLevelNames(ctx, e.ID)
		if err2 != nil {
			return nil, fmt.Errorf("report_examples: list get level names: %w", err2)
		}
		examples[i] = ReportExample{ID: e.ID, Name: e.Name, Content: e.Content, Status: e.Status, LevelNames: lns}
	}
	return examples, nil
}

func (s *dbExampleStore) UploadExample(ctx context.Context, userID, name, content string, levelNames []string) (*ReportExample, error) {
	e, err := s.repo.Create(ctx, userID, name, content)
	if err != nil {
		return nil, fmt.Errorf("report_examples: upload: %w", err)
	}
	if err := s.repo.SetLevelNames(ctx, e.ID, levelNames); err != nil {
		return nil, fmt.Errorf("report_examples: upload set level names: %w", err)
	}
	return &ReportExample{ID: e.ID, Name: e.Name, Content: e.Content, Status: e.Status, LevelNames: levelNames}, nil
}

func (s *dbExampleStore) CreatePendingExample(ctx context.Context, userID, name, filePath string, levelNames []string) (*ReportExample, error) {
	e, err := s.repo.CreatePending(ctx, userID, name, filePath)
	if err != nil {
		return nil, fmt.Errorf("report_examples: create pending: %w", err)
	}
	if err := s.repo.SetLevelNames(ctx, e.ID, levelNames); err != nil {
		return nil, fmt.Errorf("report_examples: create pending set level names: %w", err)
	}
	return &ReportExample{ID: e.ID, Name: e.Name, Content: e.Content, Status: e.Status, LevelNames: levelNames}, nil
}

func (s *dbExampleStore) UpdateExampleStatus(ctx context.Context, id int64, status, content string) error {
	return s.repo.UpdateStatus(ctx, id, status, content)
}

func (s *dbExampleStore) UpdateExample(ctx context.Context, userID string, id int64, name, content string, levelNames []string) (*ReportExample, error) {
	e, err := s.repo.Update(ctx, userID, id, name, content)
	if err != nil {
		return nil, fmt.Errorf("report_examples: update: %w", err)
	}
	if err := s.repo.SetLevelNames(ctx, e.ID, levelNames); err != nil {
		return nil, fmt.Errorf("report_examples: update set level names: %w", err)
	}
	return &ReportExample{ID: e.ID, Name: e.Name, Content: e.Content, Status: e.Status, LevelNames: levelNames}, nil
}

func (s *dbExampleStore) DeleteExample(ctx context.Context, userID string, id int64) error {
	if err := s.repo.Delete(ctx, userID, id); err != nil {
		return fmt.Errorf("report_examples: delete: %w", err)
	}
	return nil
}
