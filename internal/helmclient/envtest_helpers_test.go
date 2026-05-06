package helmclient

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// metav1Create returns the metav1.CreateOptions used by envtest seeding.
// Encapsulated so the metav1 import stays scoped to this helper.
func metav1Create() metav1.CreateOptions { return metav1.CreateOptions{} }
