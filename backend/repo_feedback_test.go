package handler

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestArtifactFeedbackRepo_InsertAndList(t *testing.T) {
	ctx, r := testDBAndRepos(t)
	repo := &ArtifactFeedbackRepo{db: r.notes.db}

	// Set up a student + note for reference
	c, err := r.classes.Create(ctx, "user1", "Math", "")
	require.NoError(t, err)
	s, err := r.students.Create(ctx, c.ID, "Alice")
	require.NoError(t, err)
	n := &Note{StudentID: s.ID, Date: "2026-01-15", Summary: "Good work", Source: "auto"}
	require.NoError(t, r.notes.Create(ctx, n))

	comment := "wrong student"
	// Insert explicit thumbs-down
	id, err := repo.Insert(ctx, ArtifactFeedback{
		ArtifactType: "note",
		ArtifactID:   n.ID,
		Rating:       "down",
		Signal:       "explicit",
		Comment:      &comment,
		UserID:       "user1",
	})
	require.NoError(t, err)
	assert.Positive(t, id)

	// Insert implicit 'edited' signal
	prev := "original summary"
	_, err = repo.Insert(ctx, ArtifactFeedback{
		ArtifactType:  "note",
		ArtifactID:    n.ID,
		Rating:        "down",
		Signal:        "edited",
		PreviousValue: &prev,
		UserID:        "user1",
	})
	require.NoError(t, err)

	// ListByArtifact
	list, err := repo.ListByArtifact(ctx, "note", n.ID)
	require.NoError(t, err)
	assert.Len(t, list, 2)
	// Collect signals to avoid timestamp-tie ordering flakiness
	signals := map[string]bool{}
	for _, f := range list {
		signals[f.Signal] = true
	}
	assert.True(t, signals["explicit"], "expected 'explicit' signal in list")
	assert.True(t, signals["edited"], "expected 'edited' signal in list")

	// Verify previous_value on the 'edited' row
	var editedRow *ArtifactFeedback
	for i := range list {
		if list[i].Signal == "edited" {
			editedRow = &list[i]
			break
		}
	}
	require.NotNil(t, editedRow)
	require.NotNil(t, editedRow.PreviousValue)
	assert.Equal(t, prev, *editedRow.PreviousValue)
	assert.Nil(t, editedRow.Comment)

	// ListByUser
	since := time.Now().Add(-1 * time.Hour)
	userList, err := repo.ListByUser(ctx, "user1", since, 10)
	require.NoError(t, err)
	assert.Len(t, userList, 2)

	// CountSignals
	counts, err := repo.CountSignals(ctx, since)
	require.NoError(t, err)
	// 1 explicit:down + 1 edited:down
	assert.Equal(t, 1, counts["explicit:down"], "explicit:down count")
	assert.Equal(t, 1, counts["edited:down"], "edited:down count")
}

func TestArtifactFeedbackRepo_ReportFeedback(t *testing.T) {
	ctx, r := testDBAndRepos(t)
	repo := &ArtifactFeedbackRepo{db: r.notes.db}

	c, err := r.classes.Create(ctx, "user1", "Math", "")
	require.NoError(t, err)
	s, err := r.students.Create(ctx, c.ID, "Alice")
	require.NoError(t, err)
	rpt := &Report{StudentID: s.ID, StartDate: "2026-01-01", EndDate: "2026-01-31", HTML: "<p>Report</p>"}
	require.NoError(t, r.reports.Create(ctx, rpt))

	// Thumbs-up on report
	id, err := repo.Insert(ctx, ArtifactFeedback{
		ArtifactType: "report",
		ArtifactID:   rpt.ID,
		Rating:       "up",
		Signal:       "explicit",
		UserID:       "user1",
	})
	require.NoError(t, err)
	assert.Positive(t, id)

	list, err := repo.ListByArtifact(ctx, "report", rpt.ID)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "up", list[0].Rating)
	assert.Equal(t, "explicit", list[0].Signal)
	assert.Nil(t, list[0].Comment)
}

func TestArtifactFeedbackRepo_DeletedNote(t *testing.T) {
	ctx, r := testDBAndRepos(t)
	repo := &ArtifactFeedbackRepo{db: r.notes.db}

	c, err := r.classes.Create(ctx, "user1", "Math", "")
	require.NoError(t, err)
	s, err := r.students.Create(ctx, c.ID, "Alice")
	require.NoError(t, err)
	n := &Note{StudentID: s.ID, Date: "2026-02-01", Summary: "Auto note", Source: "auto"}
	require.NoError(t, r.notes.Create(ctx, n))

	// Delete the note, then record signal (artifact_id will dangle — expected by design)
	require.NoError(t, r.notes.Delete(ctx, n.ID))
	prev := n.Summary
	_, err = repo.Insert(ctx, ArtifactFeedback{
		ArtifactType:  "note",
		ArtifactID:    n.ID,
		Rating:        "down",
		Signal:        "deleted",
		PreviousValue: &prev,
		UserID:        "user1",
	})
	require.NoError(t, err, "inserting feedback for deleted note should succeed (dangling artifact_id is expected)")
}
