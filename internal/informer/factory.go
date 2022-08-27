package informer

import (
	"context"
	"errors"
	"time"

	"github.com/adriansr/sm-controller/internal/schema"
	"github.com/adriansr/sm-controller/internal/watchers"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
)

const (
	defaultResyncPeriod = 30 * time.Second

	zeroDuration = time.Duration(0)
)

type Factory struct {
	inner        informers.SharedInformerFactory
	resyncPeriod time.Duration
	errorHandler watchers.ErrorHandler
}

func NewFactory(client kubernetes.Interface, opts ...FactoryOption) (*Factory, error) {
	f := &Factory{}
	for _, opt := range opts {
		if err := opt(f); err != nil {
			return nil, err
		}
	}
	if f.resyncPeriod == zeroDuration {
		f.resyncPeriod = defaultResyncPeriod
	}
	f.inner = informers.NewSharedInformerFactory(client, f.resyncPeriod)
	return f, nil
}

func (f *Factory) ForResource(r schema.Resource) (Informer, error) {
	inner, err := f.inner.ForResource(r.GroupVersionResource())
	return &informer{
		inner:        inner,
		errorHandler: f.errorHandler,
	}, err
}

func (f *Factory) Start(ctx context.Context) {
	f.inner.Start(ctx.Done())
}

func (f *Factory) Stop() {
	f.inner.Shutdown()
}

type FactoryOption func(*Factory) error

func WithResyncPeriod(p time.Duration) FactoryOption {
	return func(f *Factory) error {
		if f.resyncPeriod != zeroDuration {
			return errors.New("resync period already set")
		}
		f.resyncPeriod = p
		return nil
	}
}

func WithErrorHandler(fn watchers.ErrorHandler) FactoryOption {
	return func(f *Factory) error {
		if f.errorHandler != nil {
			return errors.New("error handler already set")
		}
		f.errorHandler = fn
		return nil
	}
}
