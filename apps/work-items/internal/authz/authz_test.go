package authz

import (
	"encoding/json"
	"reflect"
	"testing"
)

func decode(t *testing.T, claim string) any {
	t.Helper()
	var value any
	if err := json.Unmarshal([]byte(claim), &value); err != nil {
		t.Fatalf("decode %s: %v", claim, err)
	}
	return value
}

func TestParse(t *testing.T) {
	tests := []struct {
		name  string
		claim string
		want  []string
		state Claim
	}{
		{"all three", `{"work-items-api":{"roles":["items.read","items.create","items.process"]}}`,
			[]string{"items.create", "items.process", "items.read"}, ClaimPresent},
		{"read only", `{"work-items-api":{"roles":["items.read"]}}`, []string{"items.read"}, ClaimPresent},
		{"unknown roles ignored", `{"work-items-api":{"roles":["items.read","items.delete","admin"]}}`,
			[]string{"items.read"}, ClaimPresent},
		{"case-sensitive", `{"work-items-api":{"roles":["Items.Read","ITEMS.CREATE"]}}`, []string{}, ClaimPresent},
		{"empty list", `{"work-items-api":{"roles":[]}}`, []string{}, ClaimPresent},
		{"other client only", `{"work-items-gateway":{"roles":["items.create"]}}`, []string{}, ClaimAbsent},
		{"client without roles", `{"work-items-api":{}}`, []string{}, ClaimAbsent},
		{"non-string entry", `{"work-items-api":{"roles":["items.read",7]}}`, []string{}, ClaimMalformed},
		{"null entry", `{"work-items-api":{"roles":["items.create",null]}}`, []string{}, ClaimMalformed},
		{"object entry", `{"work-items-api":{"roles":[{"name":"items.read"}]}}`, []string{}, ClaimMalformed},
		{"roles as string", `{"work-items-api":{"roles":"items.read"}}`, []string{}, ClaimMalformed},
		{"client as list", `{"work-items-api":["items.read"]}`, []string{}, ClaimMalformed},
		{"claim as string", `"items.read"`, []string{}, ClaimMalformed},
		{"claim as list", `[{"work-items-api":{"roles":["items.read"]}}]`, []string{}, ClaimMalformed},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			set, state := Parse(decode(t, test.claim), true)
			if state != test.state {
				t.Fatalf("state = %s, want %s", state, test.state)
			}
			if got := set.Names(); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("permissions = %v, want %v", got, test.want)
			}
		})
	}
}

func TestParseAbsentClaim(t *testing.T) {
	set, state := Parse(nil, false)
	if state != ClaimAbsent || len(set.Names()) != 0 {
		t.Fatalf("absent claim: state %s, permissions %v", state, set.Names())
	}
}

func TestZeroPermissionIsNeverGranted(t *testing.T) {
	set, _ := Parse(decode(t, `{"work-items-api":{"roles":[""]}}`), true)
	if set.Has(Permission{}) {
		t.Fatal("the zero permission must never be granted")
	}
	if (Permission{}).Valid() {
		t.Fatal("the zero permission must be invalid")
	}
}
