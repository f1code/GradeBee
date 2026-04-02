package handler

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"testing"
)

// stubRoster implements Roster for tests.
type stubRoster struct {
	classNames  []string
	classErr    error
	students    []classGroup
	studentsErr error
}

func (s *stubRoster) ClassNames(_ context.Context) ([]string, error) {
	return s.classNames, s.classErr
}

func (s *stubRoster) Students(_ context.Context) ([]classGroup, error) {
	return s.students, s.studentsErr
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
	roster         Roster
	transcriber    Transcriber
	transErr       error
	extractor      Extractor
	extractErr     error
	noteCreator    NoteCreator
	exampleStore        ExampleStore
	exampleExtractor    ExampleExtractor
	exampleExtractorErr error
	reportGen      ReportGenerator
	reportGenErr   error
	uploadQueue    UploadQueue
	uploadQueueErr error
	driveClient    DriveClient
	driveClientErr error
	db             *sql.DB
	classRepo      *ClassRepo
	studentRepo    *StudentRepo
	noteRepo       *NoteRepo
	reportRepo     *ReportRepo
	exampleRepo    *ReportExampleRepo
	uploadRepo     *UploadRepo
	uploadsDir     string
}

func (m *mockDepsAll) GetTranscriber() (Transcriber, error) {
	if m.transErr != nil {
		return nil, m.transErr
	}
	return m.transcriber, nil
}

func (m *mockDepsAll) GetRoster(_ context.Context, _ string) Roster {
	if m.roster != nil {
		return m.roster
	}
	return &stubRoster{}
}

func (m *mockDepsAll) GetExtractor() (Extractor, error) {
	if m.extractErr != nil {
		return nil, m.extractErr
	}
	return m.extractor, nil
}

func (m *mockDepsAll) GetNoteCreator() NoteCreator {
	return m.noteCreator
}

func (m *mockDepsAll) GetExampleStore() ExampleStore {
	return m.exampleStore
}

func (m *mockDepsAll) GetExampleExtractor() (ExampleExtractor, error) {
	if m.exampleExtractorErr != nil {
		return nil, m.exampleExtractorErr
	}
	return m.exampleExtractor, nil
}

func (m *mockDepsAll) GetReportGenerator() (ReportGenerator, error) {
	if m.reportGenErr != nil {
		return nil, m.reportGenErr
	}
	return m.reportGen, nil
}

func (m *mockDepsAll) GetUploadQueue() (UploadQueue, error) {
	if m.uploadQueueErr != nil {
		return nil, m.uploadQueueErr
	}
	return m.uploadQueue, nil
}

func (m *mockDepsAll) GetDriveClient(_ context.Context, _ string) (DriveClient, error) {
	if m.driveClientErr != nil {
		return nil, m.driveClientErr
	}
	return m.driveClient, nil
}

func (m *mockDepsAll) GetDB() *sql.DB                        { return m.db }
func (m *mockDepsAll) GetClassRepo() *ClassRepo               { return m.classRepo }
func (m *mockDepsAll) GetStudentRepo() *StudentRepo           { return m.studentRepo }
func (m *mockDepsAll) GetNoteRepo() *NoteRepo                 { return m.noteRepo }
func (m *mockDepsAll) GetReportRepo() *ReportRepo             { return m.reportRepo }
func (m *mockDepsAll) GetExampleRepo() *ReportExampleRepo     { return m.exampleRepo }
func (m *mockDepsAll) GetUploadRepo() *UploadRepo             { return m.uploadRepo }
func (m *mockDepsAll) GetUploadsDir() string                  { return m.uploadsDir }

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
	return &CreateNoteResponse{NoteID: 1}, nil
}

// stubUploadQueue implements UploadQueue with in-memory storage for tests.
type stubUploadQueue struct {
	jobs      map[string]UploadJob // keyed by "userId/<uploadId>"
	published []UploadJob          // records Publish calls
}

func newStubUploadQueue() *stubUploadQueue {
	return &stubUploadQueue{jobs: make(map[string]UploadJob)}
}

func (q *stubUploadQueue) Publish(_ context.Context, job UploadJob) error {
	job.Status = JobStatusQueued
	q.jobs[kvKey(job.UserID, job.UploadID)] = job
	q.published = append(q.published, job)
	return nil
}

func (q *stubUploadQueue) GetJob(_ context.Context, userID string, uploadID int64) (*UploadJob, error) {
	job, ok := q.jobs[kvKey(userID, uploadID)]
	if !ok {
		return nil, fmt.Errorf("job not found: %s/%d", userID, uploadID)
	}
	return &job, nil
}

func (q *stubUploadQueue) UpdateJob(_ context.Context, job UploadJob) error {
	q.jobs[kvKey(job.UserID, job.UploadID)] = job
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

func (q *stubUploadQueue) DeleteJob(_ context.Context, userID string, uploadID int64) error {
	key := kvKey(userID, uploadID)
	delete(q.jobs, key)
	return nil
}

// stubDriveClient implements DriveClient for tests.
type stubDriveClient struct {
	meta    *DriveFile
	metaErr error
	data    io.ReadCloser
	dlErr   error
}

// Compile-time check that stubDriveClient satisfies DriveClient.
var _ DriveClient = (*stubDriveClient)(nil)

func (s *stubDriveClient) GetFileMeta(_ context.Context, _ string) (*DriveFile, error) {
	return s.meta, s.metaErr
}

func (s *stubDriveClient) DownloadFile(_ context.Context, _ string) (io.ReadCloser, error) {
	return s.data, s.dlErr
}

// newTestQueue returns a stub queue for integration tests.
func newTestQueue(_ *testing.T) *stubUploadQueue {
	return newStubUploadQueue()
}

// stubExampleExtractor implements ExampleExtractor for tests.
type stubExampleExtractor struct {
	result      string
	err         error
	gotFilename string
	gotData     []byte
}

func (s *stubExampleExtractor) ExtractText(_ context.Context, filename string, data []byte) (string, error) {
	s.gotFilename = filename
	s.gotData = data
	return s.result, s.err
}

// stubExampleStore implements ExampleStore for tests.
type stubExampleStore struct {
	uploadedName    string
	uploadedContent string
	uploadResult    *ReportExample
	uploadErr       error
}

func (s *stubExampleStore) ListExamples(_ context.Context, _ string) ([]ReportExample, error) {
	return nil, nil
}

func (s *stubExampleStore) UploadExample(_ context.Context, _, name, content string) (*ReportExample, error) {
	s.uploadedName = name
	s.uploadedContent = content
	if s.uploadErr != nil {
		return nil, s.uploadErr
	}
	if s.uploadResult != nil {
		return s.uploadResult, nil
	}
	return &ReportExample{ID: 1, Name: name}, nil
}

func (s *stubExampleStore) DeleteExample(_ context.Context, _ string, _ int64) error {
	return nil
}

func (s *stubExampleStore) UpdateExample(_ context.Context, _ string, id int64, name, content string) (*ReportExample, error) {
	return &ReportExample{ID: id, Name: name, Content: content}, nil
}
