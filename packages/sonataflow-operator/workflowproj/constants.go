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

package workflowproj

import "strings"

type WorkflowExtension string

func (p WorkflowExtension) String() string {
	return string(p)
}

const (
	YamlWorkflow WorkflowExtension = ".sw.yaml"
	YmlWorkflow  WorkflowExtension = ".sw.yml"
	JsonWorkflow WorkflowExtension = ".sw.json"
)

func IsWorkflowFile(name string) bool {
	return IsYmlWorkflow(name) || IsYamlWorkflow(name) || IsJsonWorkflow(name)
}

func IsYmlWorkflow(name string) bool {
	return strings.HasSuffix(name, YmlWorkflow.String())
}

func IsYamlWorkflow(name string) bool {
	return strings.HasSuffix(name, YamlWorkflow.String())
}

func IsJsonWorkflow(name string) bool {
	return strings.HasSuffix(name, JsonWorkflow.String())
}
