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

package workflowdef

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/api/metadata"

	operatorapi "github.com/apache/incubator-kie-tools/packages/sonataflow-operator/api/v1alpha08"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/cloudevents/sdk-go/v2/binding"
	cehttp "github.com/cloudevents/sdk-go/v2/protocol/http"
)

func NewCloudEvent() *cloudevents.Event {

	ctx := cloudevents.ContextWithTarget(context.TODO(), "http://localhost:8180/definitions")
	ctx = binding.WithForceStructured(ctx)
	p, err := cloudevents.NewHTTP()
	if err != nil {
		log.Fatalf("failed to create protocol: %s", err.Error())
	}

	//by default it sends binary mode
	c, err := cloudevents.NewClient(p, cloudevents.WithTimeNow(), cloudevents.WithUUIDs())
	if err != nil {
		log.Fatalf("failed to create client, %v", err)
	}

	event := cloudevents.NewEvent(cloudevents.VersionV1)
	event.SetType("ProcessDefinitionEvent")
	event.SetSource("http://operator.com")
	event.SetExtension("kogitoprocid", "hello_world")
	data := make(map[string]interface{})
	data["id"] = "hello_world"
	data["version"] = "1.0"
	data["metadata"] = map[string]interface{}{
		"status": "operator-status changed 2",
	}
	data["nodes"] = [0]string{}
	_ = event.SetData(cloudevents.ApplicationJSON, data)

	res := c.Send(ctx, event)

	if cloudevents.IsUndelivered(res) {
		log.Printf("Failed to send: %v", res)
	} else {
		var httpResult *cehttp.Result
		if cloudevents.ResultAs(res, &httpResult) {
			var err error
			if !ResultOK(httpResult) {
				err = fmt.Errorf(httpResult.Format, httpResult.Args...)
			}
			log.Printf("Sent event with status code %d, error: %v", httpResult.StatusCode, err)
		} else {
			log.Printf("Send did not return an HTTP response: %s", res)
		}
	}
	return &event
}

func ResultOK(httpResult *cehttp.Result) bool {
	return httpResult.StatusCode == http.StatusOK || httpResult.StatusCode == http.StatusAccepted
}

func NewWorkflowDefinitionAvailabilityEvent(workflow *operatorapi.SonataFlow, eventSource string, serviceUrl string,
	available bool) *cloudevents.Event {
	var status = "unavailable"
	if available {
		status = "available"
	}
	event := cloudevents.NewEvent(cloudevents.VersionV1)
	event.SetType("ProcessDefinitionEvent")
	event.SetSource(eventSource)
	event.SetExtension("kogitoprocid", workflow.Name)
	data := make(map[string]interface{})
	data["id"] = workflow.Name
	//WM TODO, can the version be null or empty?
	version := workflow.ObjectMeta.Annotations[metadata.Version]
	data["version"] = version
	data["type"] = "SW"
	//TODO continue here
	data["endpoint"] = serviceUrl
	data["metadata"] = map[string]interface{}{
		"status": status,
	}
	data["nodes"] = [0]string{}
	_ = event.SetData(cloudevents.ApplicationJSON, data)
	return &event
}
