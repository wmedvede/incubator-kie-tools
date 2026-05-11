/*
 * Licensed to the Apache Software Foundation (ASF) under one
 * or more contributor license agreements.  See the NOTICE file
 * distributed with this work for additional information
 * regarding copyright ownership.  The ASF licenses this file
 * to you under the Apache License, Version 2.0 (the
 * "License"); you may not use this file except in compliance
 * with the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

package v1alpha08

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/api"
)

// SonataFlowRegistrySpec defines the desired state of SonataFlowRegistry
type SonataFlowRegistrySpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	WorkflowVersions map[string]string `json:"workflowVersions,omitempty" protobuf:"bytes,7,rep,name=nodeSelector"`
}

// SonataFlowRegistryStatus defines the observed state of SonataFlowRegistry
type SonataFlowRegistryStatus struct {
	api.Status `json:",inline"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// SonataFlowRegistry is the Schema for the sonataflowregistries API
type SonataFlowRegistry struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SonataFlowRegistrySpec   `json:"spec,omitempty"`
	Status SonataFlowRegistryStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// SonataFlowRegistryList contains a list of SonataFlowRegistry
type SonataFlowRegistryList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SonataFlowRegistry `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SonataFlowRegistry{}, &SonataFlowRegistryList{})
}
