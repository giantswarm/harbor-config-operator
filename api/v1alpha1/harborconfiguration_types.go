/*
Copyright 2022.

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

func init() {
	SchemeBuilder.Register(&HarborConfiguration{}, &HarborConfigurationList{})
}

type HarborConfigurationSpec struct {
	HarborTarget HarborTarget `json:"harborTarget"`
	Registry     Registry     `json:"registry"`
}

type HarborConfigurationStatus struct {
	RegistryId int64 `json:"id"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

type HarborConfiguration struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   HarborConfigurationSpec   `json:"spec,omitempty"`
	Status HarborConfigurationStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

type HarborConfigurationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []HarborConfiguration `json:"items"`
}

type HarborTarget struct {
	ApiUrl   string `json:"apiUrl"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type Registry struct {
	Name              string              `json:"name"`
	Type              string              `json:"type"`
	TargetRegistryUrl string              `json:"targetRegistryUrl"`
	Description       string              `json:"description,omitempty"`
	Credential        *RegistryCredential `json:"credential,omitempty"`
}

type RegistryCredential struct {

	// Access key, e.g. user name when credential type is 'basic'.
	AccessKey string `json:"access_key,omitempty"`

	// Access secret, e.g. password when credential type is 'basic'.
	AccessSecret string `json:"access_secret,omitempty"`

	// Credential type, such as 'basic', 'oauth'.
	Type string `json:"type,omitempty"`
}
