package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

const GreetingTaskQueue = "GREETING_TASK_QUEUE"
const GreetingWorkflowID = "greeting-workflow"

func ComposeGreeting(ctx context.Context, name string) (string, error) {
	greeting := fmt.Sprintf("Hello %s!", name)
	return greeting, nil
}

func GreetingWorkflow(ctx workflow.Context, name string) (string, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{StartToCloseTimeout: 5 * time.Second})
	var result string
	err := workflow.ExecuteActivity(ctx, ComposeGreeting, name).Get(ctx, &result)
	return result, err
}

func clientmain() {
	c, err := client.Dial(client.Options{})
	if err != nil {
		log.Fatalf("client failed to connect to server: %s", err)
	}
	defer c.Close()

	name := "World"
	run, err := c.ExecuteWorkflow(context.Background(), client.StartWorkflowOptions{ID: GreetingWorkflowID, TaskQueue: GreetingTaskQueue}, GreetingWorkflow, name)
	if err != nil {
		log.Fatalf("workflow failed to complete: %s", err)
	}

	var result string
	err = run.Get(context.Background(), &result)
	if err != nil {
		log.Fatalln("failed to get workflow result", err)
	}

	log.Printf("WorkflowID: %s RunID: %s Result: %s", run.GetID(), run.GetRunID(), result)
}

func workermain() {
	c, err := client.Dial(client.Options{})
	if err != nil {
		log.Fatalf("worker failed to connect to server: %s", err)
	}
	defer c.Close()

	w := worker.New(c, GreetingTaskQueue, worker.Options{})
	w.RegisterWorkflow(GreetingWorkflow)
	w.RegisterActivity(ComposeGreeting)

	if err := w.Run(worker.InterruptCh()); err != nil {
		log.Fatalf("failed to start worker: %s", err)
	}
}
