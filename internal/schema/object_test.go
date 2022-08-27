package schema

import (
	"testing"

	"github.com/stretchr/testify/require"
	coreV1 "k8s.io/api/core/v1"
	networkingV1 "k8s.io/api/networking/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestObjectFrom(t *testing.T) {
	for name, test := range map[string]struct {
		obj        interface{}
		expectedID string
		err        error
	}{
		"service": {
			obj: &coreV1.Service{
				TypeMeta: metaV1.TypeMeta{
					Kind:       "Service",
					APIVersion: "v1",
				},
				ObjectMeta: metaV1.ObjectMeta{
					Name:      "my_service",
					Namespace: "default",
					Annotations: map[string]string{
						"foo": "bar",
					},
				},
			},
			expectedID: "v1/Service:default/my_service",
		},
		"ingress": {
			obj: &networkingV1.Ingress{
				TypeMeta: metaV1.TypeMeta{
					Kind:       "Ingress",
					APIVersion: "networking.k8s.io/v1",
				},
				ObjectMeta: metaV1.ObjectMeta{
					Name:            "ingress1",
					Namespace:       "ingress",
					Annotations:     nil,
					OwnerReferences: nil,
					Finalizers:      nil,
					ManagedFields:   nil,
				},
				Spec:   networkingV1.IngressSpec{},
				Status: networkingV1.IngressStatus{},
			},
			expectedID: "networking.k8s.io/v1/Ingress:ingress/ingress1",
		},
		"non pointer": {
			obj: coreV1.Service{
				TypeMeta: metaV1.TypeMeta{
					Kind:       "Service",
					APIVersion: "v1",
				},
				ObjectMeta: metaV1.ObjectMeta{
					Name: "non-pointer",
				},
			},
			err: ErrUnexpectedObject,
		},
	} {
		t.Run(name, func(t *testing.T) {
			obj, err := ObjectFrom(test.obj)
			require.Equal(t, test.err, err)
			if err != nil {
				return
			}
			require.Equal(t, test.expectedID, obj.ID())
			require.Equal(t, test.expectedID, obj.String())
			require.Equal(t, test.obj, obj.Inner())
			require.IsType(t, test.obj, obj.Inner())
			require.Equal(t, test.obj.(k8sObject).GetAnnotations(), obj.GetAnnotations())
		})
	}
}
