package service

type ValidationError struct {
	Message string
}

func (e ValidationError) Error() string {
	return e.Message
}

type QueueError struct {
	Cause error
}

func (e QueueError) Error() string {
	return "failed to enqueue event"
}

func (e QueueError) Unwrap() error {
	return e.Cause
}
