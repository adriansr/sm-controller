package watchers

import (
	"context"

	"github.com/adriansr/sm-controller/internal/schema"
)

type Event struct {
	Obj    schema.Object
	Action Action
}

type Publisher struct {
	C   chan<- Event
	Ctx context.Context
}

func (p Publisher) OnAdd(obj schema.Object) error {
	return p.publish(Event{Obj: obj, Action: Add})
}

func (p Publisher) OnUpdate(oldObj, newObj schema.Object) error {
	return p.publish(Event{Obj: newObj, Action: Update})
}

func (p Publisher) OnDelete(obj schema.Object) error {
	return p.publish(Event{Obj: obj, Action: Delete})
}

func (p Publisher) publish(ev Event) error {
	select {
	case p.C <- ev:
		return nil
	case <-p.Ctx.Done():
		return p.Ctx.Err()
	}
}
