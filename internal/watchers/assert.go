package watchers

import (
	"fmt"

	"github.com/adriansr/sm-controller/internal/schema"
)

type TypeAssert[T any] struct{}

func typeAssert[T any](obj schema.Object) error {
	inner := obj.Inner()
	if _, ok := inner.(T); !ok {
		var dummy T
		return fmt.Errorf("type assertion failed, expected %T got %T", dummy, inner)
	}
	return nil
}

func (t TypeAssert[T]) OnAdd(obj schema.Object) error {
	return typeAssert[T](obj)
}

func (t TypeAssert[T]) OnUpdate(oldObj, newObj schema.Object) error {
	if err := typeAssert[T](oldObj); err != nil {
		return err
	}
	return typeAssert[T](newObj)
}

func (t TypeAssert[T]) OnDelete(obj schema.Object) error {
	return typeAssert[T](obj)
}
