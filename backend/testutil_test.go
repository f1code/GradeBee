package handler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"strings"
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
	result      string
	err         error
	gotBias     []string
}

func (s *stubTranscriber) Transcribe(_ context.Context, _ string, _ io.Reader, contextBias []string) (string, error) {
	s.gotBias = contextBias
	return s.result, s.err
}

// mockDepsAll satisfies deps with configurable returns for all methods.
type mockDepsAll struct {
	roster              Roster
	transcriber         Transcriber
	transErr            error
	extractor           Extractor
	extractErr          error
	noteCreator         NoteCreator
	reportGen           ReportGenerator
	reportGenErr        error
	voiceNoteQueue      JobQueue[VoiceNoteJob]
	voiceNoteQueueErr   error
	driveClient         DriveClient
	driveClientErr      error
	db                  *sql.DB
	classRepo           *ClassRepo
	studentRepo         *StudentRepo
	noteRepo            *NoteRepo
	reportRepo          *ReportRepo
	voiceNoteRepo       *VoiceNoteRepo
	feedbackRepo        *ArtifactFeedbackRepo
	levelRepo           *LevelRepo
	uploadsDir          string
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
func (m *mockDepsAll) GetVoiceNoteRepo() *VoiceNoteRepo       { return m.voiceNoteRepo }
func (m *mockDepsAll) GetFeedbackRepo() *ArtifactFeedbackRepo { return m.feedbackRepo }
func (m *mockDepsAll) GetLevelRepo() *LevelRepo               { return m.levelRepo }
func (m *mockDepsAll) GetUploadsDir() string                  { return m.uploadsDir }

// stubExtractor implements Extractor for tests.
type stubExtractor struct {
	result *ExtractResponse
	err    error
	model  string
}

func (s *stubExtractor) Extract(_ context.Context, _ ExtractRequest) (*ExtractResponse, error) {
	return s.result, s.err
}

func (s *stubExtractor) Model() string {
	if s.model != "" {
		return s.model
	}
	return "stub-model"
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

// requireLiveLLM skips the test if the active LLM provider's API key is unset.
// It returns the configured provider for live tests that need it.
func requireLiveLLM(t *testing.T) LLMProvider {
	t.Helper()
	p, err := LoadProvider()
	if err != nil {
		t.Skipf("LLM provider not configured: %v", err)
	}
	return p
}

// newTestLevel creates a Level for the given Group directly in the DB,
// bypassing the migration's hand-authored seed data. Tests build their own
// Levels through this fixture rather than relying on the migration's seed —
// a data migration should not be load-bearing for the unit suite.
func newTestLevel(t *testing.T, db *sql.DB, groupID, name string) Level {
	t.Helper()
	l, err := (&LevelRepo{db: db}).Create(context.Background(), groupID, name)
	if err != nil {
		t.Fatalf("newTestLevel: %v", err)
	}
	return l
}

// testLevelID returns the ID of a Level named name in groupID, creating it if
// it doesn't already exist. Tests share this across many class-creating call
// sites so re-creating a class with the same level name (e.g. duplicate
// checks) resolves to the same Level rather than erroring on Level creation.
func testLevelID(t *testing.T, db *sql.DB, groupID, name string) int64 {
	t.Helper()
	repo := &LevelRepo{db: db}
	l, err := repo.Create(context.Background(), groupID, name)
	if err == nil {
		return l.ID
	}
	if !errors.Is(err, ErrDuplicate) {
		t.Fatalf("testLevelID: %v", err)
	}
	levels, lerr := repo.List(context.Background(), groupID)
	if lerr != nil {
		t.Fatalf("testLevelID: list: %v", lerr)
	}
	for _, lv := range levels {
		if strings.EqualFold(lv.Name, name) {
			return lv.ID
		}
	}
	t.Fatalf("testLevelID: %q not found in group %q after duplicate error", name, groupID)
	return 0
}

// newTestClass creates a class for userID against a Level named levelName in
// groupID, creating the Level on first use. Test call sites that pre-#57
// passed a free-text level name now pass it here unchanged.
func newTestClass(t *testing.T, cr *ClassRepo, groupID, userID, levelName, scheduleName string) Class {
	t.Helper()
	levelID := testLevelID(t, cr.db, groupID, levelName)
	c, err := cr.Create(context.Background(), groupID, userID, levelID, scheduleName)
	if err != nil {
		t.Fatalf("newTestClass: %v", err)
	}
	return c
}
