package handler

import (
	"context"
	"io"
	"net/http"
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
	googleSvcErr error
	roster       Roster
	rosterErr    error
	driveStore   DriveStore
	transcriber  Transcriber
	transErr     error
}

func (m *mockDepsAll) GoogleServices(_ *http.Request) (*googleServices, error) {
	if m.googleSvcErr != nil {
		return nil, m.googleSvcErr
	}
	return &googleServices{User: &clerkUser{UserID: "test-user"}}, nil
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
