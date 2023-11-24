package redis

// these methods should be asynchronous functions
type Task interface {
	Set(timeout int, fc func()) string
	Clear(timeoutId string)
	Close()
}

type DefaultTask struct{}

func NewDefaultTask() Task {
	return &DefaultTask{}
}

// set a task return task id
// timeout is Millisecond
func (t *DefaultTask) Set(timeout int, fc func()) string {
	return ""
}

func (t *DefaultTask) Clear(timeoutId string) {
}

func (t *DefaultTask) Close() {
}
