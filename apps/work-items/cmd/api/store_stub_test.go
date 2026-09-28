package main

import "context"

type stubApplicationStore struct {
	readinessErr error

	createItem   workItem
	createErr    error
	createTitle  string
	createStatus string

	listItems []workItem
	listErr   error

	acceptJob        processingJob
	acceptErr        error
	acceptWorkItemID int64
}

func (store *stubApplicationStore) Ready(context.Context) error {
	return store.readinessErr
}

func (store *stubApplicationStore) CreateWorkItem(
	_ context.Context,
	title string,
	status string,
) (workItem, error) {
	store.createTitle = title
	store.createStatus = status

	return store.createItem, store.createErr
}

func (store *stubApplicationStore) ListWorkItems(
	context.Context,
) ([]workItem, error) {
	return store.listItems, store.listErr
}

func (store *stubApplicationStore) AcceptProcessingJob(
	_ context.Context,
	workItemID int64,
) (processingJob, error) {
	store.acceptWorkItemID = workItemID

	return store.acceptJob, store.acceptErr
}

func availableDatabase() applicationStore {
	return &stubApplicationStore{}
}
