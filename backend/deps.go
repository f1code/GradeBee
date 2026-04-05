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
	// GetVoiceNoteQueue returns the JobQueue for async voice note processing.
	GetVoiceNoteQueue() (JobQueue[VoiceNoteJob], error)
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
	GetVoiceNoteRepo() *VoiceNoteRepo
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
	voiceNoteRepo *VoiceNoteRepo
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

func (p *prodDeps) GetVoiceNoteQueue() (JobQueue[VoiceNoteJob], error) {
	if voiceNoteQueueInstance == nil {
		return nil, fmt.Errorf("voice note queue not initialized — call InitVoiceNoteQueue first")
	}
	return voiceNoteQueueInstance, nil
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
func (p *prodDeps) GetVoiceNoteRepo() *VoiceNoteRepo             { return p.voiceNoteRepo }
func (p *prodDeps) GetUploadsDir() string                  { return p.uploadsDir }

// Voice note queue singleton, initialised at startup via InitVoiceNoteQueue.
var voiceNoteQueueInstance JobQueue[VoiceNoteJob]

// InitVoiceNoteQueue creates the in-memory voice note queue, starts worker
// goroutines, and stores it as the package-level singleton.
func InitVoiceNoteQueue(d deps, workers int) *MemQueue[VoiceNoteJob] {
	q := NewMemQueue[VoiceNoteJob](func(ctx context.Context, queue JobQueue[VoiceNoteJob], key string) error {
		return processVoiceNote(ctx, d, queue, key)
	}, workers)
	voiceNoteQueueInstance = q
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
		voiceNoteRepo: &VoiceNoteRepo{db: db},
		uploadsDir:  uploadsDir,
	}
	serviceDeps = d
	return d
}

// serviceDeps is the active dependency implementation. Tests override this.
var serviceDeps deps
