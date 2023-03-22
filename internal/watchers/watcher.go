package watchers

import (
	"github.com/adriansr/sm-controller/internal/schema"
	"k8s.io/client-go/tools/cache"
)

type Watcher interface {
	OnAdd(obj schema.Object) error
	OnUpdate(oldObj, newObj schema.Object) error
	OnDelete(obj schema.Object) error
}

type ResourceMetaSetter schema.Resource

func (s ResourceMetaSetter) OnAdd(obj schema.Object) error {
	obj.SetGroupVersionKind(schema.Resource(s).GroupVersionKind())
	return nil
}

func (s ResourceMetaSetter) OnUpdate(oldObj, newObj schema.Object) error {
	oldObj.SetGroupVersionKind(schema.Resource(s).GroupVersionKind())
	newObj.SetGroupVersionKind(schema.Resource(s).GroupVersionKind())
	return nil
}

func (s ResourceMetaSetter) OnDelete(obj schema.Object) error {
	obj.SetGroupVersionKind(schema.Resource(s).GroupVersionKind())
	return nil
}

type Chain []Watcher

func (s Chain) OnAdd(obj schema.Object) error {
	for _, h := range s {
		if err := h.OnAdd(obj); err != nil {
			return err
		}
	}
	return nil
}

func (s Chain) OnUpdate(oldObj, newObj schema.Object) error {
	for _, h := range s {
		if err := h.OnUpdate(oldObj, newObj); err != nil {
			break
		}
	}
	return nil
}

func (s Chain) OnDelete(obj schema.Object) error {
	for _, h := range s {
		if err := h.OnDelete(obj); err != nil {
			break
		}
	}
	return nil
}

func ToK8S(w Watcher, fn ErrorHandler) cache.ResourceEventHandler {
	return wrapper{
		Watcher: w,
		errorFn: fn,
	}
}

type wrapper struct {
	Watcher
	errorFn func(error)
}

func (w wrapper) onError(err error) {
	if w.errorFn != nil {
		w.errorFn(err)
	}
}

func (w wrapper) OnAdd(obj interface{}) {
	o, err := schema.ObjectFrom(obj)
	if err != nil {
		w.onError(PipelineError{
			Err:     err,
			Object:  obj,
			Watcher: w.Watcher,
			Action:  Cast,
		})
		return
	}
	if err = w.Watcher.OnAdd(o); err != nil {
		w.onError(PipelineError{
			Err:     err,
			Object:  obj,
			Watcher: w.Watcher,
			Action:  Add,
		})
	}
}

func (w wrapper) OnUpdate(oldObj, newObj interface{}) {
	o, err := schema.ObjectFrom(oldObj)
	if err != nil {
		w.onError(PipelineError{
			Err:     err,
			Object:  oldObj,
			Watcher: w.Watcher,
			Action:  Cast,
		})
		return
	}
	n, err := schema.ObjectFrom(newObj)
	if err != nil {
		w.onError(PipelineError{
			Err:     err,
			Object:  newObj,
			Watcher: w.Watcher,
			Action:  Cast,
		})
		return
	}
	if err = w.Watcher.OnUpdate(o, n); err != nil {
		w.onError(PipelineError{
			Err:     err,
			Object:  newObj,
			Watcher: w.Watcher,
			Action:  Update,
		})
	}
}

func (w wrapper) OnDelete(obj interface{}) {
	o, err := schema.ObjectFrom(obj)
	if err != nil {
		w.onError(PipelineError{
			Err:     err,
			Object:  obj,
			Watcher: w.Watcher,
			Action:  Cast,
		})
		return
	}
	if err = w.Watcher.OnDelete(o); err != nil {
		w.onError(PipelineError{
			Err:     err,
			Object:  obj,
			Watcher: w.Watcher,
			Action:  Delete,
		})
	}
}
