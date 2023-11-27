package redis

import (
	"context"
	"time"

	"github.com/pborman/uuid"
)

// these methods should be asynchronous functions
type Task interface {
	Set(timeout time.Duration, fc func()) string
	Clear(timeoutId string)
	Close()
}

type timePart struct {
	Key     string
	Timeout int64 // time UnixMilli
	Fc      func()
}

type DefaultTask struct {
	ctx  context.Context
	canl context.CancelFunc
	heap *MinHeap
}

func NewDefaultTask() Task {
	ctx, canl := context.WithCancel(context.Background())
	h, err := NewMinHeap()
	if err != nil {
		panic(err)
	}
	t := &DefaultTask{ctx: ctx, canl: canl, heap: h}
	go t.run(ctx)
	return t
}

func (t *DefaultTask) run(ctx context.Context) {
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			timeTamp := time.Now().UnixMilli()
			tp, err := t.heap.TryGetValue()
			if err != nil {
				continue
			}
			if tp.Timeout > timeTamp {
				continue
			}
			tp, err = t.heap.GetValue()
			if err != nil {
				continue
			}
			go tp.Fc()
		case <-t.ctx.Done():
			return
		}
	}
}

// set a task return task id
// timeout should is Millisecond
func (t *DefaultTask) Set(timeout time.Duration, fc func()) string {
	k := uuid.New()
	t.heap.PutValue(timePart{
		Key:     k,
		Timeout: time.Now().Add(timeout).UnixMilli(),
		Fc:      fc,
	})
	return k
}

// Clear use timeoutId clear the task
func (t *DefaultTask) Clear(timeoutKey string) {
	t.heap.Delete(timeoutKey)
}

func (t *DefaultTask) Close() {
	t.canl()
}
