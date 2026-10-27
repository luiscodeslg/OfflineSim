package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

type jobState string

const (
	jobRunning   jobState = "running"
	jobDone      jobState = "done"
	jobError     jobState = "error"
	jobCancelled jobState = "cancelled"
)

// job is a long running task (a sim or a SimC install) the UI polls.
type job struct {
	mu       sync.Mutex
	ID       string
	Kind     string
	Title    string
	State    jobState
	Progress float64 // 0..1, negative when unknown
	Phase    string
	Lines    []string
	ResultID string
	Err      string
	Started  time.Time
	ctx      context.Context
	cancel   context.CancelFunc
}

type jobSnapshot struct {
	ID       string   `json:"id"`
	Kind     string   `json:"kind"`
	Title    string   `json:"title"`
	State    jobState `json:"state"`
	Progress float64  `json:"progress"`
	Phase    string   `json:"phase"`
	Lines    []string `json:"lines"`
	Next     int      `json:"next"`
	ResultID string   `json:"resultId,omitempty"`
	Err      string   `json:"error,omitempty"`
	Elapsed  float64  `json:"elapsed"`
}

const maxJobLines = 4000

func (j *job) log(line string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if len(j.Lines) >= maxJobLines {
		// Keep the head (setup output) and drop the middle of very chatty runs.
		j.Lines = append(j.Lines[:maxJobLines/2], j.Lines[len(j.Lines)-maxJobLines/4:]...)
	}
	j.Lines = append(j.Lines, line)
}

func (j *job) setProgress(p float64, phase string) {
	j.mu.Lock()
	j.Progress = p
	if phase != "" {
		j.Phase = phase
	}
	j.mu.Unlock()
}

func (j *job) finish(state jobState, resultID string, err error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.State != jobRunning {
		return
	}
	j.State = state
	j.ResultID = resultID
	if err != nil {
		j.Err = err.Error()
	}
	if state == jobDone {
		j.Progress = 1
	}
	j.cancel()
}

func (j *job) snapshot(since int) jobSnapshot {
	j.mu.Lock()
	defer j.mu.Unlock()
	if since < 0 || since > len(j.Lines) {
		since = 0
	}
	lines := append([]string{}, j.Lines[since:]...)
	return jobSnapshot{
		ID: j.ID, Kind: j.Kind, Title: j.Title, State: j.State, Progress: j.Progress,
		Phase: j.Phase, Lines: lines, Next: len(j.Lines), ResultID: j.ResultID,
		Err: j.Err, Elapsed: time.Since(j.Started).Seconds(),
	}
}

type jobManager struct {
	mu   sync.Mutex
	jobs map[string]*job
}

func newJobManager() *jobManager { return &jobManager{jobs: map[string]*job{}} }

func newID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return time.Now().Format("20060102-150405") + "-" + hex.EncodeToString(b)
}

func (m *jobManager) start(kind, title string, run func(j *job) (string, error)) *job {
	ctx, cancel := context.WithCancel(context.Background())
	j := &job{ID: newID(), Kind: kind, Title: title, State: jobRunning, Progress: -1,
		Started: time.Now(), ctx: ctx, cancel: cancel}
	m.mu.Lock()
	m.jobs[j.ID] = j
	m.mu.Unlock()
	go func() {
		id, err := run(j)
		switch {
		case ctx.Err() != nil && err != nil:
			j.finish(jobCancelled, "", nil)
		case err != nil:
			j.log("ERROR: " + err.Error())
			j.finish(jobError, "", err)
		default:
			j.finish(jobDone, id, nil)
		}
	}()
	return j
}

func (m *jobManager) get(id string) *job {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.jobs[id]
}

func (m *jobManager) running(kind string) *job {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, j := range m.jobs {
		j.mu.Lock()
		r := j.State == jobRunning && j.Kind == kind
		j.mu.Unlock()
		if r {
			return j
		}
	}
	return nil
}

func (m *jobManager) cancelAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, j := range m.jobs {
		j.cancel()
	}
}

func contextWithTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}
