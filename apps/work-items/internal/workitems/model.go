// Package workitems defines the shared API/worker business-state contract.
package workitems

const (
	Pending = "pending"
	Done    = "done"
	// Use the two-int advisory-lock space, distinct from single-bigint locks.
	// Hashing supports the full bigint ID range; collisions only serialize unrelated items.
	ItemLockSQL = "SELECT pg_advisory_xact_lock(1464423501, hashint8($1::bigint))"
)
