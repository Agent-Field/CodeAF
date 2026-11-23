package main

import (
	"context"
	"io"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

func TestRunnerJoinPassesItsExplicitModelPinToTheWorker(t *testing.T) {
	attachLab(t)
	previous := factoryWorkerParent()
	stock := runFactoryRunner
	t.Cleanup(func() {
		runFactoryRunner = stock
		setFactoryProcess(func() { factoryProcess.parent = previous })
	})
	setFactoryProcess(func() { factoryProcess.parent = session.Config{} })
	called := false
	runFactoryRunner = func(context.Context, factoryAttachment, io.Writer) error {
		called = true
		got := factoryWorkerParent()
		if got.Model != "deepseek/deepseek-v4.1-flash" || !got.OneModel {
			t.Fatalf("worker lost explicit model selection: model=%q, one-model=%v", got.Model, got.OneModel)
		}
		return nil
	}
	err := runRunner([]string{"join", "https://factory.example", "--token", "session-fixture", "--model", "deepseek/deepseek-v4.1-flash", "--one-model"})
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("the selected worker was not started")
	}
}
