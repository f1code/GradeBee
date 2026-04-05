package handler

import (
	"context"
	"fmt"
	"testing"
	"time"
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
	if err := q.Publish(ctx, testJob{Owner: "u1", ID: 1, Status: "queued", Data: "hello"}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	got, err := q.GetJob(ctx, "u1/1")
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if got.Data != "hello" {
		t.Errorf("data = %q, want %q", got.Data, "hello")
	}
}

func TestGenericQueue_GetJob_NotFound(t *testing.T) {
	q := NewMemQueue[testJob](nil, 0)
	defer q.Close()

	_, err := q.GetJob(context.Background(), "u1/999")
	if err == nil {
		t.Fatal("expected error for missing job")
	}
}

func TestGenericQueue_UpdateJob(t *testing.T) {
	q := NewMemQueue[testJob](nil, 0)
	defer q.Close()

	ctx := context.Background()
	if err := q.Publish(ctx, testJob{Owner: "u1", ID: 1, Status: "queued"}); err != nil {
		t.Fatal(err)
	}

	if err := q.UpdateJob(ctx, testJob{Owner: "u1", ID: 1, Status: "done", Data: "result"}); err != nil {
		t.Fatal(err)
	}

	got, err := q.GetJob(ctx, "u1/1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "done" {
		t.Errorf("status = %q, want done", got.Status)
	}
	if got.Data != "result" {
		t.Errorf("data = %q, want result", got.Data)
	}
}

func TestGenericQueue_ListJobs(t *testing.T) {
	q := NewMemQueue[testJob](nil, 0)
	defer q.Close()

	ctx := context.Background()
	if err := q.Publish(ctx, testJob{Owner: "u1", ID: 1}); err != nil {
		t.Fatal(err)
	}
	if err := q.Publish(ctx, testJob{Owner: "u1", ID: 2}); err != nil {
		t.Fatal(err)
	}
	if err := q.Publish(ctx, testJob{Owner: "u2", ID: 3}); err != nil {
		t.Fatal(err)
	}

	jobs, err := q.ListJobs(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 {
		t.Errorf("got %d jobs for u1, want 2", len(jobs))
	}

	jobs2, err := q.ListJobs(ctx, "u2")
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs2) != 1 {
		t.Errorf("got %d jobs for u2, want 1", len(jobs2))
	}
}

func TestGenericQueue_ListJobs_Empty(t *testing.T) {
	q := NewMemQueue[testJob](nil, 0)
	defer q.Close()

	jobs, err := q.ListJobs(context.Background(), "nobody")
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 0 {
		t.Errorf("got %d jobs, want 0", len(jobs))
	}
}

func TestGenericQueue_DeleteJob(t *testing.T) {
	q := NewMemQueue[testJob](nil, 0)
	defer q.Close()

	ctx := context.Background()
	if err := q.Publish(ctx, testJob{Owner: "u1", ID: 1}); err != nil {
		t.Fatal(err)
	}
	if err := q.DeleteJob(ctx, "u1/1"); err != nil {
		t.Fatal(err)
	}
	_, err := q.GetJob(ctx, "u1/1")
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestGenericQueue_ChannelFull(t *testing.T) {
	q := &MemQueue[testJob]{
		jobs:   make(map[string]testJob),
		work:   make(chan string, 1),
		cancel: func() {},
	}
	defer q.Close()

	ctx := context.Background()
	if err := q.Publish(ctx, testJob{Owner: "u1", ID: 1}); err != nil {
		t.Fatal(err)
	}
	err := q.Publish(ctx, testJob{Owner: "u1", ID: 2})
	if err == nil {
		t.Fatal("expected error when channel is full")
	}
}

func TestGenericQueue_WorkerProcessesJob(t *testing.T) {
	processed := make(chan string, 1)
	q := NewMemQueue[testJob](func(ctx context.Context, q JobQueue[testJob], key string) error {
		processed <- key
		return nil
	}, 1)
	defer q.Close()

	if err := q.Publish(context.Background(), testJob{Owner: "u1", ID: 1, Status: "queued"}); err != nil {
		t.Fatal(err)
	}

	select {
	case key := <-processed:
		if key != "u1/1" {
			t.Errorf("processed key = %q, want u1/1", key)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for worker")
	}
}

func TestGenericQueue_Close_StopsWorkers(t *testing.T) {
	q := NewMemQueue[testJob](nil, 2)
	q.Close()
}
