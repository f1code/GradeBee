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
	students    []ClassGroup
	studentsErr error
}

func (s *stubRoster) ClassNames(_ context.Context) ([]string, error) {
	return s.classNames, s.classErr
}

func (s *stubRoster) Students(_ context.Context) ([]ClassGroup, error) {
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
	voiceNoteQueue    JobQueue[VoiceNoteJob]
	voiceNoteQueueErr error
	extractionQueue    JobQueue[ExtractionJob]
	extractionQueueErr error
	driveClient    DriveClient
	driveClientErr error
	db             *sql.DB
	classRepo      *ClassRepo
	studentRepo    *StudentRepo
	noteRepo       *NoteRepo
	reportRepo     *ReportRepo
	exampleRepo    *ReportExampleRepo
	voiceNoteRepo  *VoiceNoteRepo
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

func (m *mockDepsAll) GetVoiceNoteQueue() (JobQueue[VoiceNoteJob], error) {
	if m.voiceNoteQueueErr != nil {
		return nil, m.voiceNoteQueueErr
	}
	return m.voiceNoteQueue, nil
}

func (m *mockDepsAll) GetExtractionQueue() (JobQueue[ExtractionJob], error) {
	if m.extractionQueueErr != nil {
		return nil, m.extractionQueueErr
	}
	return m.extractionQueue, nil
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
func (m *mockDepsAll) GetVoiceNoteRepo() *VoiceNoteRepo             { return m.voiceNoteRepo }
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

// stubVoiceNoteQueue implements JobQueue[VoiceNoteJob] for tests.
type stubVoiceNoteQueue struct {
	jobs      map[string]VoiceNoteJob
	published []VoiceNoteJob
}

func newStubVoiceNoteQueue() *stubVoiceNoteQueue {
	return &stubVoiceNoteQueue{jobs: make(map[string]VoiceNoteJob)}
}

func (q *stubVoiceNoteQueue) Publish(_ context.Context, job VoiceNoteJob) error {
	q.jobs[job.JobKey()] = job
	q.published = append(q.published, job)
	return nil
}

func (q *stubVoiceNoteQueue) GetJob(_ context.Context, key string) (*VoiceNoteJob, error) {
	job, ok := q.jobs[key]
	if !ok {
		return nil, fmt.Errorf("job not found: %s", key)
	}
	return &job, nil
}

func (q *stubVoiceNoteQueue) UpdateJob(_ context.Context, job VoiceNoteJob) error {
	q.jobs[job.JobKey()] = job
	return nil
}

func (q *stubVoiceNoteQueue) ListJobs(_ context.Context, ownerID string) ([]VoiceNoteJob, error) {
	prefix := ownerID + "/"
	var jobs []VoiceNoteJob
	for k, j := range q.jobs {
		if len(k) > len(prefix) && k[:len(prefix)] == prefix {
			jobs = append(jobs, j)
		}
	}
	return jobs, nil
}

func (q *stubVoiceNoteQueue) DeleteJob(_ context.Context, key string) error {
	delete(q.jobs, key)
	return nil
}

func (q *stubVoiceNoteQueue) Close() {}

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
func newTestQueue(_ *testing.T) *stubVoiceNoteQueue {
	return newStubVoiceNoteQueue()
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
	pendingResult   *ReportExample
	pendingErr      error
	updateStatusErr error
	updateStatusCalls []struct {
		ID      int64
		Status  string
		Content string
	}
}

func (s *stubExampleStore) ListExamples(_ context.Context, _ string) ([]ReportExample, error) {
	return nil, nil
}

func (s *stubExampleStore) UploadExample(_ context.Context, _, name, content string, classNames []string) (*ReportExample, error) {
	s.uploadedName = name
	s.uploadedContent = content
	if s.uploadErr != nil {
		return nil, s.uploadErr
	}
	if s.uploadResult != nil {
		return s.uploadResult, nil
	}
	return &ReportExample{ID: 1, Name: name, Status: "ready", ClassNames: classNames}, nil
}

func (s *stubExampleStore) CreatePendingExample(_ context.Context, _, name, filePath string, classNames []string) (*ReportExample, error) {
	if s.pendingErr != nil {
		return nil, s.pendingErr
	}
	if s.pendingResult != nil {
		return s.pendingResult, nil
	}
	return &ReportExample{ID: 1, Name: name, Status: "processing", ClassNames: classNames}, nil
}

func (s *stubExampleStore) UpdateExampleStatus(_ context.Context, id int64, status, content string) error {
	s.updateStatusCalls = append(s.updateStatusCalls, struct {
		ID      int64
		Status  string
		Content string
	}{id, status, content})
	return s.updateStatusErr
}

func (s *stubExampleStore) DeleteExample(_ context.Context, _ string, _ int64) error {
	return nil
}

func (s *stubExampleStore) UpdateExample(_ context.Context, _ string, id int64, name, content string, classNames []string) (*ReportExample, error) {
	return &ReportExample{ID: id, Name: name, Content: content, Status: "ready", ClassNames: classNames}, nil
}
