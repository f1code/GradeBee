package handler

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"testing"
)

// stubRoster implements Roster for tests.
type stubRoster struct {
	classNames  []string
	classErr    error
	students    []classGroup
	studentsErr error
	url         string
}

func (s *stubRoster) ClassNames(_ context.Context) ([]string, error) {
	return s.classNames, s.classErr
}

func (s *stubRoster) Students(_ context.Context) ([]classGroup, error) {
	return s.students, s.studentsErr
}

func (s *stubRoster) SpreadsheetURL() string { return s.url }

// stubDriveStore implements DriveStore for tests.
type stubDriveStore struct {
	downloadBody io.ReadCloser
	downloadErr  error
	fileName     string
	fileNameErr  error
	uploadID     string
	uploadErr    error
	copyID       string
	copyErr      error
	mimeType     string
	mimeTypeErr  error
}

func (s *stubDriveStore) Download(_ context.Context, _ string) (io.ReadCloser, error) {
	return s.downloadBody, s.downloadErr
}

func (s *stubDriveStore) FileName(_ context.Context, _ string) (string, error) {
	return s.fileName, s.fileNameErr
}

func (s *stubDriveStore) Upload(_ context.Context, _, _ string, _ io.Reader) (string, error) {
	return s.uploadID, s.uploadErr
}

func (s *stubDriveStore) Copy(_ context.Context, _, _, _ string) (string, error) {
	return s.copyID, s.copyErr
}

func (s *stubDriveStore) GetMimeType(_ context.Context, _ string) (string, error) {
	return s.mimeType, s.mimeTypeErr
}

// stubTranscriber implements Transcriber for tests.
type stubTranscriber struct {
	result    string
	err       error
	gotPrompt string
}

func (s *stubTranscriber) Transcribe(_ context.Context, _ string, _ io.Reader, prompt string) (string, error) {
	s.gotPrompt = prompt
	return s.result, s.err
}

// mockDepsAll satisfies deps with configurable returns for all methods.
type mockDepsAll struct {
	googleSvcErr        error
	googleSvcForUser    *googleServices
	googleSvcForUserErr error
	roster              Roster
	rosterErr           error
	driveStore          DriveStore
	transcriber         Transcriber
	transErr            error
	extractor           Extractor
	extractErr          error
	noteCreator         NoteCreator
	uploadQueue         UploadQueue
	uploadQueueErr      error
	metadata            *gradeBeeMetadata
	metadataErr         error
}

func (m *mockDepsAll) GoogleServices(_ *http.Request) (*googleServices, error) {
	if m.googleSvcErr != nil {
		return nil, m.googleSvcErr
	}
	return &googleServices{User: &clerkUser{UserID: "test-user"}}, nil
}

func (m *mockDepsAll) GoogleServicesForUser(_ context.Context, userID string) (*googleServices, error) {
	if m.googleSvcForUserErr != nil {
		return nil, m.googleSvcForUserErr
	}
	if m.googleSvcForUser != nil {
		return m.googleSvcForUser, nil
	}
	return &googleServices{User: &clerkUser{UserID: userID}}, nil
}

func (m *mockDepsAll) GetTranscriber() (Transcriber, error) {
	if m.transErr != nil {
		return nil, m.transErr
	}
	return m.transcriber, nil
}

func (m *mockDepsAll) GetRoster(_ context.Context, _ *googleServices) (Roster, error) {
	if m.rosterErr != nil {
		return nil, m.rosterErr
	}
	return m.roster, nil
}

func (m *mockDepsAll) GetDriveStore(_ *googleServices) DriveStore {
	return m.driveStore
}

func (m *mockDepsAll) GetExtractor() (Extractor, error) {
	if m.extractErr != nil {
		return nil, m.extractErr
	}
	return m.extractor, nil
}

func (m *mockDepsAll) GetNoteCreator(_ *googleServices) NoteCreator {
	return m.noteCreator
}

func (m *mockDepsAll) GetMetadataIndex(_ *googleServices) MetadataIndex {
	return nil
}

func (m *mockDepsAll) GetExampleStore(_ *googleServices) ExampleStore {
	return nil
}

func (m *mockDepsAll) GetExampleExtractor() (ExampleExtractor, error) {
	return nil, fmt.Errorf("not configured")
}

func (m *mockDepsAll) GetReportGenerator(_ *googleServices) (ReportGenerator, error) {
	return nil, fmt.Errorf("not configured")
}

func (m *mockDepsAll) GetUploadQueue() (UploadQueue, error) {
	if m.uploadQueueErr != nil {
		return nil, m.uploadQueueErr
	}
	return m.uploadQueue, nil
}

func (m *mockDepsAll) GetGradeBeeMetadata(_ context.Context, _ string) (*gradeBeeMetadata, error) {
	if m.metadataErr != nil {
		return nil, m.metadataErr
	}
	return m.metadata, nil
}

func (m *mockDepsAll) GetDB() *sql.DB                        { return nil }
func (m *mockDepsAll) GetClassRepo() *ClassRepo               { return nil }
func (m *mockDepsAll) GetStudentRepo() *StudentRepo           { return nil }
func (m *mockDepsAll) GetNoteRepo() *NoteRepo                 { return nil }
func (m *mockDepsAll) GetReportRepo() *ReportRepo             { return nil }
func (m *mockDepsAll) GetExampleRepo() *ReportExampleRepo     { return nil }
func (m *mockDepsAll) GetUploadRepo() *UploadRepo             { return nil }

// stubExtractor implements Extractor for tests.
type stubExtractor struct {
	result *ExtractResponse
	err    error
}

func (s *stubExtractor) Extract(_ context.Context, _ ExtractRequest) (*ExtractResponse, error) {
	return s.result, s.err
}

// stubNoteCreator implements NoteCreator for tests.
type stubNoteCreator struct {
	results []*CreateNoteResponse // returned in order
	err     error
	calls   []CreateNoteRequest // recorded calls
	idx     int
}

func (s *stubNoteCreator) CreateNote(_ context.Context, req CreateNoteRequest) (*CreateNoteResponse, error) {
	s.calls = append(s.calls, req)
	if s.err != nil {
		return nil, s.err
	}
	if s.idx < len(s.results) {
		r := s.results[s.idx]
		s.idx++
		return r, nil
	}
	return &CreateNoteResponse{DocID: "doc-id", DocURL: "https://docs.google.com/document/d/doc-id/edit"}, nil
}

// stubUploadQueue implements UploadQueue with in-memory storage for tests.
type stubUploadQueue struct {
	jobs      map[string]UploadJob // keyed by "userId/fileId"
	published []UploadJob          // records Publish calls
}

func newStubUploadQueue() *stubUploadQueue {
	return &stubUploadQueue{jobs: make(map[string]UploadJob)}
}

func (q *stubUploadQueue) Publish(_ context.Context, job UploadJob) error {
	job.Status = JobStatusQueued
	q.jobs[kvKey(job.UserID, job.FileID)] = job
	q.published = append(q.published, job)
	return nil
}

func (q *stubUploadQueue) GetJob(_ context.Context, userID, fileID string) (*UploadJob, error) {
	job, ok := q.jobs[kvKey(userID, fileID)]
	if !ok {
		return nil, fmt.Errorf("job not found: %s/%s", userID, fileID)
	}
	return &job, nil
}

func (q *stubUploadQueue) UpdateJob(_ context.Context, job UploadJob) error {
	q.jobs[kvKey(job.UserID, job.FileID)] = job
	return nil
}

func (q *stubUploadQueue) ListJobs(_ context.Context, userID string) ([]UploadJob, error) {
	prefix := userID + "/"
	var jobs []UploadJob
	for k, j := range q.jobs {
		if len(k) > len(prefix) && k[:len(prefix)] == prefix {
			jobs = append(jobs, j)
		}
	}
	return jobs, nil
}

func (q *stubUploadQueue) Close() {}

func (q *stubUploadQueue) DeleteJob(_ context.Context, userID, fileID string) error {
	key := kvKey(userID, fileID)
	delete(q.jobs, key)
	return nil
}

// newTestQueue returns a stub queue for integration tests. The *testing.T
// parameter is kept for API compatibility with the old NATS-based version.
func newTestQueue(_ *testing.T) *stubUploadQueue {
	return newStubUploadQueue()
}
