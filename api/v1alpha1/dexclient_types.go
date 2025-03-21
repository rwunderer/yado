/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DexClientSpec defines the desired state of DexClient
type DexClientSpec struct {
	// SecretName is the name of the secret that will be created to store the
	// OAuth2 client id and client secret.
	SecretName string `json:"secretName"`
	// RedirectURIs is a list of allowed redirect URLs for the client.
	RedirectURIs []string `json:"redirectURIs,omitempty"`
	// TrustedPeers are a list of peers which can issue tokens on this client's
	// behalf using the dynamic "oauth2:server:client_id:(client_id)" scope.
	// If a peer makes such a request, this client's ID will appear as the ID Token's audience.
	TrustedPeers []string `json:"trustedPeers,omitempty"`
	// Public indicates that this client is a public client, such as a mobile app.
	// Public clients must use either use a redirectURL 127.0.0.1:X or "urn:ietf:wg:oauth:2.0:oob".
	Public bool `json:"public,omitempty"`
	// Name is the human-readable name of the client.
	Name string `json:"name,omitempty"`
	// LogoURL is the URL to a logo for the client.
	LogoURL string `json:"logoURL,omitempty"`
}

type DexClientPhase string

const (
	// DexClientPhasePending indicates that the  client is pending.
	DexClientPhasePending DexClientPhase = "Pending"
	// DexClientPhaseReady indicates that the  client is ready.
	DexClientPhaseReady DexClientPhase = "Ready"
	// DexClientPhaseFailed indicates that the  client has failed.
	DexClientPhaseFailed DexClientPhase = "Failed"
)

// DexClientStatus defines the observed state of DexClient
type DexClientStatus struct {
	// Phase is the current phase of the OAuth2 client.
	Phase DexClientPhase `json:"phase,omitempty"`
	// ObservedGeneration is the most recent generation observed for this OAuth2 client by the controller.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Conditions store the status conditions of the OAuth2 client instances
	// +operator-sdk:csv:customresourcedefinitions:type=status
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
	// Reason is a human readable message indicating details about why the OAuth2 client is in this condition.
	Reason string `json:"reason,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// DexClient is the Schema for the dexclients API
type DexClient struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DexClientSpec   `json:"spec,omitempty"`
	Status DexClientStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DexClientList contains a list of DexClient
type DexClientList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DexClient `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DexClient{}, &DexClientList{})
}
