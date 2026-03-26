// deps.go defines the dependency-injection interface used by HTTP handlers to
// obtain Google API service clients. The production implementation delegates to
// Clerk and Google; tests swap in a stub via the serviceDeps variable.
package handler

import (
	"context"
	"fmt"
	"net/http"
)

// deps abstracts external service calls for testability.
type deps interface {
	// GoogleServices returns authenticated Google API clients for the user.
	GoogleServices(r *http.Request) (*googleServices, error)
	// GoogleServicesForUser returns Google API clients for a user by ID
	// (no HTTP request needed — used for background processing).
	GoogleServicesForUser(ctx context.Context, userID string) (*googleServices, error)
	// GetTranscriber returns a Transcriber implementation.
	GetTranscriber() (Transcriber, error)
	// GetRoster returns a Roster for the authenticated user's spreadsheet.
	GetRoster(ctx context.Context, svc *googleServices) (Roster, error)
	// GetDriveStore returns a DriveStore for the authenticated user's Drive.
	GetDriveStore(svc *googleServices) DriveStore
	// GetExtractor returns an Extractor for transcript analysis.
	GetExtractor() (Extractor, error)
	// GetNoteCreator returns a NoteCreator for the authenticated user's Drive.
	GetNoteCreator(svc *googleServices) NoteCreator
	// GetMetadataIndex returns a MetadataIndex for the authenticated user's Drive.
	GetMetadataIndex(svc *googleServices) MetadataIndex
	// GetExampleStore returns an ExampleStore for the authenticated user's Drive.
	GetExampleStore(svc *googleServices) ExampleStore
	// GetExampleExtractor returns an ExampleExtractor for PDF/image text extraction.
	GetExampleExtractor() (ExampleExtractor, error)
	// GetReportGenerator returns a ReportGenerator.
	GetReportGenerator(svc *googleServices) (ReportGenerator, error)
	// GetUploadQueue returns the UploadQueue for async job management.
	GetUploadQueue() (UploadQueue, error)
	// GetGradeBeeMetadata retrieves GradeBee IDs from Clerk user metadata.
	GetGradeBeeMetadata(ctx context.Context, userID string) (*gradeBeeMetadata, error)
}

// prodDeps is the real implementation that calls Clerk + Google APIs.
type prodDeps struct{}

func (prodDeps) GoogleServices(r *http.Request) (*googleServices, error) {
	return newGoogleServices(r)
}

func (prodDeps) GoogleServicesForUser(ctx context.Context, userID string) (*googleServices, error) {
	return newGoogleServicesForUser(ctx, userID)
}

func (prodDeps) GetTranscriber() (Transcriber, error) {
	return newWhisperTranscriber()
}

func (prodDeps) GetRoster(ctx context.Context, svc *googleServices) (Roster, error) {
	return newSheetsRoster(ctx, svc)
}

func (prodDeps) GetDriveStore(svc *googleServices) DriveStore {
	return newDriveStore(svc)
}

func (prodDeps) GetExtractor() (Extractor, error) {
	return newGPTExtractor()
}

func (prodDeps) GetNoteCreator(svc *googleServices) NoteCreator {
	metaIdx := newDriveMetadataIndex(svc.Drive)
	return newDriveNoteCreator(svc.Drive, svc.Docs, metaIdx)
}

func (prodDeps) GetMetadataIndex(svc *googleServices) MetadataIndex {
	return newDriveMetadataIndex(svc.Drive)
}

func (prodDeps) GetExampleStore(svc *googleServices) ExampleStore {
	return newDriveExampleStore(svc.Drive)
}

func (prodDeps) GetExampleExtractor() (ExampleExtractor, error) {
	return newGPTExampleExtractor()
}

func (prodDeps) GetReportGenerator(svc *googleServices) (ReportGenerator, error) {
	return newGPTReportGenerator(svc.Drive, svc.Docs)
}

func (prodDeps) GetUploadQueue() (UploadQueue, error) {
	if uploadQueueInstance == nil {
		return nil, fmt.Errorf("upload queue not initialized — call InitUploadQueue first")
	}
	return uploadQueueInstance, nil
}

func (prodDeps) GetGradeBeeMetadata(ctx context.Context, userID string) (*gradeBeeMetadata, error) {
	return getGradeBeeMetadata(ctx, userID)
}

// Upload queue singleton, initialised at startup via InitUploadQueue.
var uploadQueueInstance UploadQueue

// InitUploadQueue creates the in-memory queue, starts worker goroutines, and
// stores it as the package-level singleton. Must be called once at startup.
func InitUploadQueue(d deps, workers int) *memQueue {
	q := NewMemQueue(d, workers)
	uploadQueueInstance = q
	return q
}

// ServiceDeps returns the package-level production deps for use in main().
func ServiceDeps() deps {
	return serviceDeps
}

// serviceDeps is the active dependency implementation. Tests override this.
var serviceDeps deps = prodDeps{}
