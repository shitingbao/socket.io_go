package redis

import (
	"context"
	"time"
)

// these methods should be asynchronous functions
type Task interface {
	Set(timeout time.Duration, fc func()) string
	Clear(timeoutId string)
	Close()
}

type timePart struct {
	Timeout int64 // time UnixMilli
	Fc      func()
}

type DefaultTask struct {
	Ctx context.Context
}

func NewDefaultTask() Task {
	ctx := context.Background()
	t := &DefaultTask{ctx}
	go t.run(ctx)
	return t
}

func (t *DefaultTask) run(ctx context.Context) {}

// set a task return task id
// timeout should is Millisecond
func (t *DefaultTask) Set(timeout time.Duration, fc func()) string {
	return ""
}

// Clear use timeoutId clear the task
func (t *DefaultTask) Clear(timeoutId string) {
}

func (t *DefaultTask) Close() {
}
