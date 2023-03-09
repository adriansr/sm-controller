package watchers

import (
	"fmt"
)

type Action uint8

const (
	Add Action = iota + 1
	Update
	Delete
	Cast
)

func (a Action) String() string {
	switch a {
	case Add:
		return "add"
	case Update:
		return "update"
	case Delete:
		return "delete"
	case Cast:
		return "cast"
	default:
		return fmt.Sprintf("<unknown:%d>", a)
	}
}

type PipelineError struct {
	Err     error
	Object  interface{}
	Watcher Watcher
	Action  Action
}

func (e PipelineError) Error() string {
	return fmt.Sprintf("watcher %s failed for object of type %T: %s", e.Action, e.Object, e.Err.Error())
}

func (e PipelineError) Unwrap() error {
	return e.Err
}

type ErrorHandler func(error)
