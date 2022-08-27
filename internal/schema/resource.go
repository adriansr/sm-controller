package schema

import (
	k8s_schema "k8s.io/apimachinery/pkg/runtime/schema"
)

type Resource struct {
	Group   string
	Version string
	Kind    string
	// Plural is what the k8s lib calls "Resource"
	Plural string
}

func (r Resource) GroupVersionKind() k8s_schema.GroupVersionKind {
	return k8s_schema.GroupVersionKind{
		Group:   r.Group,
		Version: r.Version,
		Kind:    r.Kind,
	}
}

func (r Resource) GroupVersionResource() k8s_schema.GroupVersionResource {
	return k8s_schema.GroupVersionResource{
		Group:    r.Group,
		Version:  r.Version,
		Resource: r.Plural,
	}
}

func (r Resource) String() string {
	sb := idBuilder(r.GroupVersionKind())
	return sb.String()
}
