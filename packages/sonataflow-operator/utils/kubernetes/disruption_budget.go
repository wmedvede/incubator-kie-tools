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

package kubernetes

import (
	"context"

	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/log"
)

// FindPDB returns the PodDisruptionBudget for the given namespace and name, or nil if it doesn't exist.
func FindPDB(ctx context.Context, c client.Client, namespace string, name string) (*policyv1.PodDisruptionBudget, error) {
	klog.V(log.D).Infof("Querying PodDisruptionBudget %s/%s.", namespace, name)
	pdb := &policyv1.PodDisruptionBudget{}
	err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, pdb)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return pdb, nil
}
