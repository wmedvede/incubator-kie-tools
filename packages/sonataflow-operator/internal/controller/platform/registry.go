// Licensed to the Apache Software Foundation (ASF) under one
// or more contributor license agreements.  See the NOTICE file
// distributed with this work for additional information
// regarding copyright ownership.  The ASF licenses this file
// to you under the Apache License, Version 2.0 (the
// "License"); you may not use this file except in compliance
// with the License.  You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package platform

import (
	"sigs.k8s.io/yaml"
)

const WorkflowsField = "workflows.yaml"
const SkipField = "skip.yaml"

// Version represents a workflow version. (keep a struct, will facilitate future version attributes adding if necessary).
type Version struct {
	Version string `json:"version" yaml:"version"`
	Enabled *bool  `json:"enabled,omitempty" yaml:"enabled,omitempty"`
}

// Workflows holds the versions for the different workflows.
type Workflows map[string][]Version

// SkipConfig holds the versions that can skip hte validations for a workflow.
type SkipConfig map[string][]string

type WorkflowRuntime struct {
	Versions []Version
	Skip     map[string]struct{} // fast lookup set
}

type RuntimeConfig map[string]WorkflowRuntime

func ParseWorkflows(data string) (Workflows, error) {
	var wf Workflows
	err := yaml.Unmarshal([]byte(data), &wf)
	return wf, err
}

func ParseSkipConfig(data string) (SkipConfig, error) {
	var cfg SkipConfig
	err := yaml.Unmarshal([]byte(data), &cfg)
	return cfg, err
}

func MarshalWorkflows(wf Workflows) (string, error) {
	b, err := yaml.Marshal(wf)
	if err != nil {
		return "", err
	}
	return string(b), err
}

func MarshalSkipConfig(cfg SkipConfig) (string, error) {
	b, err := yaml.Marshal(cfg)
	if err != nil {
		return "", err
	}
	return string(b), err
}
