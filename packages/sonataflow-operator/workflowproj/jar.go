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

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"strings"
)

// ReadWorkflowFilesFromJar iterates a .jar file and returns all the files that contains a workflow.
func ReadWorkflowFilesFromJar(jar []byte) (map[string][]byte, error) {
	workflowFiles := make(map[string][]byte)
	jarReader, err := zip.NewReader(bytes.NewReader(jar), int64(len(jar)))
	if err != nil {
		return nil, fmt.Errorf("failed to create jar reader: %v", err)
	}
	for _, file := range jarReader.File {
		fmt.Printf("Jar entry: %s\n", file.Name)
		if IsWorkflowFile(file.Name) {
			rc, err := file.Open()
			if err != nil {
				return nil, fmt.Errorf("failed to open file: %s from jar, %v", file.Name, err)
			}
			content, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return nil, fmt.Errorf("failed to read file: %s from jar, %v", file.Name, err)
			}
			workflowFiles[file.Name] = content
		}
	}
	return workflowFiles, nil
}

// WM TODO remove
func extractFileName(name string) string {
	index := strings.LastIndex(name, "/")
	if index >= 0 {
		return name[index+1 : len(name)]
	}
	return name
}
