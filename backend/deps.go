// deps.go defines the dependency-injection interface used by HTTP handlers.
// The production implementation uses SQLite repos; tests swap in stubs via the
// serviceDeps variable.
package handler

import (
	"context"
	"database/sql"
	"fmt"
)

// deps abstracts external service calls for testability.
type deps interface {
	// GetTranscriber returns a Transcriber implementation.
	GetTranscriber() (Transcriber, error)
	// GetRoster returns a Roster for the given user.
	GetRoster(ctx context.Context, userID string) Roster
	// GetExtractor returns an Extractor for transcript analysis.
	GetExtractor() (Extractor, error)
	// GetNoteCreator returns a NoteCreator backed by the DB.
	GetNoteCreator() NoteCreator
	// GetExampleStore returns an ExampleStore backed by the DB.
	GetExampleStore() ExampleStore
	// GetExampleExtractor returns an ExampleExtractor for PDF/image text extraction.
	GetExampleExtractor() (ExampleExtractor, error)
	// GetReportGenerator returns a ReportGenerator.
	GetReportGenerator() (ReportGenerator, error)
	// GetUploadQueue returns the UploadQueue for async job management.
	GetUploadQueue() (UploadQueue, error)
	// GetDriveClient returns a Drive-read-only client for the given user.
	GetDriveClient(ctx context.Context, userID string) (DriveClient, error)
	// GetDB returns the SQLite database handle.
	GetDB() *sql.DB
	// Repository accessors.
	GetClassRepo() *ClassRepo
	GetStudentRepo() *StudentRepo
	GetNoteRepo() *NoteRepo
	GetReportRepo() *ReportRepo
	GetExampleRepo() *ReportExampleRepo
	GetUploadRepo() *UploadRepo
	// GetUploadsDir returns the local directory for audio file storage.
	GetUploadsDir() string
}

// prodDeps is the real implementation backed by SQLite repos.
type prodDeps struct {
	db          *sql.DB
	classRepo   *ClassRepo
	studentRepo *StudentRepo
	noteRepo    *NoteRepo
	reportRepo  *ReportRepo
	exampleRepo *ReportExampleRepo
	uploadRepo  *UploadRepo
	uploadsDir  string
}

func (p *prodDeps) GetTranscriber() (Transcriber, error) {
	return newWhisperTranscriber()
}

func (p *prodDeps) GetRoster(_ context.Context, userID string) Roster {
	return newDBRoster(p.classRepo, p.studentRepo, userID)
}

func (p *prodDeps) GetExtractor() (Extractor, error) {
	return newGPTExtractor()
}

func (p *prodDeps) GetNoteCreator() NoteCreator {
	return newDBNoteCreator(p.noteRepo)
}

func (p *prodDeps) GetExampleStore() ExampleStore {
	return newDBExampleStore(p.exampleRepo)
}

func (p *prodDeps) GetExampleExtractor() (ExampleExtractor, error) {
	return newGPTExampleExtractor()
}

func (p *prodDeps) GetReportGenerator() (ReportGenerator, error) {
	return newDBReportGenerator(p.noteRepo, p.reportRepo, p.exampleRepo)
}

func (p *prodDeps) GetUploadQueue() (UploadQueue, error) {
	if uploadQueueInstance == nil {
		return nil, fmt.Errorf("upload queue not initialized — call InitUploadQueue first")
	}
	return uploadQueueInstance, nil
}

func (p *prodDeps) GetDriveClient(ctx context.Context, userID string) (DriveClient, error) {
	svc, err := newDriveReadClient(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &googleDriveClient{svc: svc}, nil
}

func (p *prodDeps) GetDB() *sql.DB                        { return p.db }
func (p *prodDeps) GetClassRepo() *ClassRepo               { return p.classRepo }
func (p *prodDeps) GetStudentRepo() *StudentRepo           { return p.studentRepo }
func (p *prodDeps) GetNoteRepo() *NoteRepo                 { return p.noteRepo }
func (p *prodDeps) GetReportRepo() *ReportRepo             { return p.reportRepo }
func (p *prodDeps) GetExampleRepo() *ReportExampleRepo     { return p.exampleRepo }
func (p *prodDeps) GetUploadRepo() *UploadRepo             { return p.uploadRepo }
func (p *prodDeps) GetUploadsDir() string                  { return p.uploadsDir }

// Upload queue singleton, initialised at startup via InitUploadQueue.
var uploadQueueInstance UploadQueue

// InitUploadQueue creates the in-memory queue, starts worker goroutines, and
// stores it as the package-level singleton. Must be called once at startup.
func InitUploadQueue(d deps, workers int) *memQueue {
	q := NewMemQueue(d, workers)
	uploadQueueInstance = q
	return q
}

// NewProdDeps creates the production deps with the given database handle
// and uploads directory.
func NewProdDeps(db *sql.DB, uploadsDir string) deps {
	d := &prodDeps{
		db:          db,
		classRepo:   &ClassRepo{db: db},
		studentRepo: &StudentRepo{db: db},
		noteRepo:    &NoteRepo{db: db},
		reportRepo:  &ReportRepo{db: db},
		exampleRepo: &ReportExampleRepo{db: db},
		uploadRepo:  &UploadRepo{db: db},
		uploadsDir:  uploadsDir,
	}
	serviceDeps = d
	return d
}

// serviceDeps is the active dependency implementation. Tests override this.
var serviceDeps deps
