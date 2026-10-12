// Package authz defines the Work Items permissions and reads them from a
// validated access token's claims.
package authz

import "sort"

// Permission is one protected Work Items action. The set is closed: only the
// package-level values below exist, and the zero value is invalid.
type Permission struct {
	name string
}

var (
	ItemsRead    = Permission{name: "items.read"}
	ItemsCreate  = Permission{name: "items.create"}
	ItemsProcess = Permission{name: "items.process"}
)

var known = []Permission{ItemsRead, ItemsCreate, ItemsProcess}

func (p Permission) String() string { return p.name }

// Valid reports whether p is one of the defined permissions.
func (p Permission) Valid() bool { return p.name != "" }

// Client is the resource_access entry whose roles carry Work Items
// permissions. Roles anywhere else in a token grant nothing.
const Client = "work-items-api"

// Claim describes the role data found in a token.
type Claim int

const (
	// ClaimAbsent: no roles for Client (normal for a principal without
	// Work Items roles).
	ClaimAbsent Claim = iota
	// ClaimMalformed: role data for Client had an unexpected shape.
	ClaimMalformed
	// ClaimPresent: a well-formed list of role names for Client.
	ClaimPresent
)

func (c Claim) String() string {
	switch c {
	case ClaimMalformed:
		return "malformed"
	case ClaimPresent:
		return "present"
	default:
		return "absent"
	}
}

// Set is the permissions granted to a principal.
type Set struct {
	granted map[Permission]bool
}

// Has reports whether p is granted.
func (s Set) Has(p Permission) bool {
	return p.Valid() && s.granted[p]
}

// Names lists the granted permissions, sorted.
func (s Set) Names() []string {
	names := make([]string, 0, len(s.granted))
	for p := range s.granted {
		names = append(names, p.name)
	}
	sort.Strings(names)
	return names
}

// Parse reads permissions from a decoded resource_access claim:
//
//	{"work-items-api": {"roles": ["items.read", ...]}}
//
// present reports whether the token has the claim at all. Every role entry
// must be a string; any other shape at any level grants no permissions.
// Unknown role names are ignored; matching is exact and case-sensitive.
func Parse(resourceAccess any, present bool) (Set, Claim) {
	if !present {
		return Set{}, ClaimAbsent
	}

	clients, ok := resourceAccess.(map[string]any)
	if !ok {
		return Set{}, ClaimMalformed
	}

	entry, ok := clients[Client]
	if !ok {
		return Set{}, ClaimAbsent
	}

	fields, ok := entry.(map[string]any)
	if !ok {
		return Set{}, ClaimMalformed
	}

	rawRoles, ok := fields["roles"]
	if !ok {
		return Set{}, ClaimAbsent
	}

	roles, ok := rawRoles.([]any)
	if !ok {
		return Set{}, ClaimMalformed
	}

	granted := map[Permission]bool{}
	for _, raw := range roles {
		name, ok := raw.(string)
		if !ok {
			return Set{}, ClaimMalformed
		}
		for _, p := range known {
			if p.name == name {
				granted[p] = true
			}
		}
	}

	return Set{granted: granted}, ClaimPresent
}
