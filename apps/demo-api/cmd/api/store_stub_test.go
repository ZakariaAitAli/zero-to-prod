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

func availableDatabase() applicationStore {
	return &stubApplicationStore{}
}
