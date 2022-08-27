package watchers

import (
	"github.com/adriansr/sm-controller/internal/schema"
	"github.com/rs/zerolog"
)

type Logger struct {
	*zerolog.Logger
	Level zerolog.Level
}

func (l Logger) OnAdd(obj schema.Object) error {
	l.WithLevel(l.Level).Str("obj", obj.ID()).Msg("add")
	return nil
}

func (l Logger) OnUpdate(oldObj, newObj schema.Object) error {
	l.WithLevel(l.Level).Str("old", oldObj.ID()).Str("new", newObj.ID()).Msg("update")
	return nil
}

func (l Logger) OnDelete(obj schema.Object) error {
	l.WithLevel(l.Level).Str("obj", obj.ID()).Msg("delete")
	return nil
}
