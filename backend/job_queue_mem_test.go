package handler

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testJob is a minimal Keyed implementation for generic queue tests.
type testJob struct {
	Owner  string
	ID     int64
	Status string
	Data   string
}

func (j testJob) JobKey() string  { return fmt.Sprintf("%s/%d", j.Owner, j.ID) }
func (j testJob) OwnerID() string { return j.Owner }

func TestGenericQueue_PublishAndGetJob(t *testing.T) {
	q := NewMemQueue[testJob](nil, 0)
	defer q.Close()

	ctx := context.Background()
	require.NoError(t, q.Publish(ctx, testJob{Owner: "u1", ID: 1, Status: "queued", Data: "hello"}))

	got, err := q.GetJob(ctx, "u1/1")
	require.NoError(t, err)
	assert.Equal(t, "hello", got.Data)
}

func TestGenericQueue_GetJob_NotFound(t *testing.T) {
	q := NewMemQueue[testJob](nil, 0)
	defer q.Close()

	_, err := q.GetJob(context.Background(), "u1/999")
	assert.Error(t, err, "expected error for missing job")
}

func TestGenericQueue_UpdateJob(t *testing.T) {
	q := NewMemQueue[testJob](nil, 0)
	defer q.Close()

	ctx := context.Background()
	require.NoError(t, q.Publish(ctx, testJob{Owner: "u1", ID: 1, Status: "queued"}))
	require.NoError(t, q.UpdateJob(ctx, testJob{Owner: "u1", ID: 1, Status: "done", Data: "result"}))

	got, err := q.GetJob(ctx, "u1/1")
	require.NoError(t, err)
	assert.Equal(t, "done", got.Status)
	assert.Equal(t, "result", got.Data)
}

func TestGenericQueue_ListJobs(t *testing.T) {
	q := NewMemQueue[testJob](nil, 0)
	defer q.Close()

	ctx := context.Background()
	require.NoError(t, q.Publish(ctx, testJob{Owner: "u1", ID: 1}))
	require.NoError(t, q.Publish(ctx, testJob{Owner: "u1", ID: 2}))
	require.NoError(t, q.Publish(ctx, testJob{Owner: "u2", ID: 3}))

	jobs, err := q.ListJobs(ctx, "u1")
	require.NoError(t, err)
	assert.Len(t, jobs, 2)

	jobs2, err := q.ListJobs(ctx, "u2")
	require.NoError(t, err)
	assert.Len(t, jobs2, 1)
}

func TestGenericQueue_ListJobs_Empty(t *testing.T) {
	q := NewMemQueue[testJob](nil, 0)
	defer q.Close()

	jobs, err := q.ListJobs(context.Background(), "nobody")
	require.NoError(t, err)
	assert.Empty(t, jobs)
}

func TestGenericQueue_DeleteJob(t *testing.T) {
	q := NewMemQueue[testJob](nil, 0)
	defer q.Close()

	ctx := context.Background()
	require.NoError(t, q.Publish(ctx, testJob{Owner: "u1", ID: 1}))
	require.NoError(t, q.DeleteJob(ctx, "u1/1"))
	_, err := q.GetJob(ctx, "u1/1")
	assert.Error(t, err, "expected error after delete")
}

func TestGenericQueue_ChannelFull(t *testing.T) {
	q := &MemQueue[testJob]{
		jobs:   make(map[string]testJob),
		work:   make(chan string, 1),
		cancel: func() {},
	}
	defer q.Close()

	ctx := context.Background()
	require.NoError(t, q.Publish(ctx, testJob{Owner: "u1", ID: 1}))
	err := q.Publish(ctx, testJob{Owner: "u1", ID: 2})
	assert.Error(t, err, "expected error when channel is full")
}

func TestGenericQueue_WorkerProcessesJob(t *testing.T) {
	processed := make(chan string, 1)
	q := NewMemQueue[testJob](func(ctx context.Context, q JobQueue[testJob], key string) error {
		processed <- key
		return nil
	}, 1)
	defer q.Close()

	require.NoError(t, q.Publish(context.Background(), testJob{Owner: "u1", ID: 1, Status: "queued"}))

	select {
	case key := <-processed:
		assert.Equal(t, "u1/1", key)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for worker")
	}
}

func TestGenericQueue_Close_StopsWorkers(t *testing.T) {
	q := NewMemQueue[testJob](nil, 2)
	q.Close()
}
