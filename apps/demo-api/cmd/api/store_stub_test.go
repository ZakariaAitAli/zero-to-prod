package main

import "context"

type stubApplicationStore struct {
	readinessErr error

	createItem workItem
	createErr  error

	listItems []workItem
	listErr   error
}

func (store *stubApplicationStore) Ready(context.Context) error {
	return store.readinessErr
}

func (store *stubApplicationStore) CreateWorkItem(
	context.Context,
	string,
) (workItem, error) {
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
