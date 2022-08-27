package schema

import (
	"testing"

	"github.com/stretchr/testify/require"
	k8s_schema "k8s.io/apimachinery/pkg/runtime/schema"
)

func TestResource(t *testing.T) {
	for name, test := range map[string]struct {
		Resource
		k8s_schema.GroupVersionKind
		k8s_schema.GroupVersionResource
	}{
		"all fields": {
			Resource: Resource{
				Group:   "networking.k8s.io",
				Version: "v1",
				Kind:    "Ingress",
				Plural:  "ingresses",
			},
			GroupVersionKind: k8s_schema.GroupVersionKind{
				Group:   "networking.k8s.io",
				Version: "v1",
				Kind:    "Ingress",
			},
			GroupVersionResource: k8s_schema.GroupVersionResource{
				Group:    "networking.k8s.io",
				Version:  "v1",
				Resource: "ingresses",
			},
		},
		"no group": {
			Resource: Resource{
				Version: "v1",
				Kind:    "Foo",
				Plural:  "foos",
			},
			GroupVersionKind: k8s_schema.GroupVersionKind{
				Version: "v1",
				Kind:    "Foo",
			},
			GroupVersionResource: k8s_schema.GroupVersionResource{
				Version:  "v1",
				Resource: "foos",
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, test.GroupVersionKind, test.Resource.GroupVersionKind())
			require.Equal(t, test.GroupVersionResource, test.Resource.GroupVersionResource())
		})
	}
}
