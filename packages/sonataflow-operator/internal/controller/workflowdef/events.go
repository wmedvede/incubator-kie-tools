package workflowdef

import (
	"context"
	"fmt"
	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/cloudevents/sdk-go/v2/binding"
	cehttp "github.com/cloudevents/sdk-go/v2/protocol/http"
	"log"
	"net/http"
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
