package watchers

import (
	"errors"

	"github.com/adriansr/sm-controller/internal/schema"
)

var (
	ErrSkipEvent = errors.New("event filtered")
)

type Filter func(schema.Object) bool

func (f Filter) OnAdd(obj schema.Object) error {
	if !f(obj) {
		return ErrSkipEvent
	}
	return nil
}

func (f Filter) OnUpdate(oldObj, newObj schema.Object) error {
	if !(f(oldObj) || f(newObj)) {
		return ErrSkipEvent
	}
	return nil
}

func (f Filter) OnDelete(obj schema.Object) error {
	if !f(obj) {
		return ErrSkipEvent
	}
	return nil
}

type UpdateFilter func(oldObj schema.Object, newObj schema.Object) bool

func (f UpdateFilter) OnAdd(obj schema.Object) error {
	return nil
}

func (f UpdateFilter) OnUpdate(oldObj, newObj schema.Object) error {
	if !(f(oldObj, newObj)) {
		return ErrSkipEvent
	}
	return nil
}

func (f UpdateFilter) OnDelete(obj schema.Object) error {
	return nil
}
