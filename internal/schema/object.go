package schema

import (
	"errors"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

type k8sObject interface {
	schema.ObjectKind

	GetNamespace() string
	GetName() string
	GetAnnotations() map[string]string
}

type Object interface {
	k8sObject
	fmt.Stringer

	ID() string
	Inner() interface{}
}

type wrapper struct {
	k8sObject
}

var (
	ErrUnexpectedObject = errors.New("unexpected object type")
)

func ObjectFrom(obj interface{}) (Object, error) {
	cast, ok := obj.(k8sObject)
	if !ok {
		return nil, ErrUnexpectedObject
	}
	return wrapper{k8sObject: cast}, nil
}

func (o wrapper) ID() string {
	sb := idBuilder(o.GroupVersionKind())
	sb.WriteByte(':')
	sb.WriteString(o.GetNamespace())
	sb.WriteByte('/')
	sb.WriteString(o.GetName())
	return sb.String()
}

func (o wrapper) String() string {
	return o.ID()
}

func (o wrapper) Inner() interface{} {
	return o.k8sObject
}

func idBuilder(gvk schema.GroupVersionKind) *strings.Builder {
	sb := new(strings.Builder)
	if gvk.Group != "" {
		sb.WriteString(gvk.Group)
		sb.WriteByte('/')
	}
	if gvk.Version != "" {
		sb.WriteString(gvk.Version)
		sb.WriteByte('/')
	}
	sb.WriteString(gvk.Kind)
	return sb
}
