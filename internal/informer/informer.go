package informer

import (
	"github.com/adriansr/sm-controller/internal/watchers"
	"k8s.io/client-go/informers"
)

type Informer interface {
	AddWatcher(watcher watchers.Watcher) error
}

type informer struct {
	inner        informers.GenericInformer
	errorHandler watchers.ErrorHandler
}

func (inf *informer) AddWatcher(w watchers.Watcher) error {
	_, err := inf.inner.Informer().AddEventHandler(watchers.ToK8S(w, inf.errorHandler))
	return err
}
