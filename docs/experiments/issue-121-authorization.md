# Issue #121 — authorization

Recorded 2026-10-12. This record covers the authorization slice designed in
[ADR 0005](../adr/0005-security-lab-authorization.md) (Proposed): Keycloak
client roles as the permission source, enforced in the Work Items API with
deny-by-default route registration. It builds on the merged authentication
baseline ([integration record](issue-121-authentication-integration.md),
[ADR 0004](../adr/0004-security-lab-authentication.md), both unchanged).

## Question

Can every Work Items route be restricted to the principals that need it, with
the same rules on the browser and machine paths? Must failures be
distinguishable (401 versus 403) and malformed or misplaced claims grant
nothing? And how long do role removals take to reach existing and newly
issued tokens?

Environment classification: **LOCAL-FIRST**, in a separate security lab
project on one workstation (Windows 11, WSL 2 in NAT mode, Docker Desktop).

## Setup

- **Separate lab.** All runs used the project `zero-to-prod-security-lab-authz`, with its own state directory, lab CA, realm, and volumes. The original `zero-to-prod-security-lab` project was not started, and its four volumes and its state directory were left unchanged. Images are shared by name, so the original lab's next `start` would build the current API (see [limitations](#limitations)).
- **Principals:** four Work Items principals, three people on the browser path and one machine client, plus a role-management client that never calls Work Items.

| Principal | Path | Permissions |
| --- | --- | --- |
| `lab-user` (composite `editor`) | browser | `items.read`, `items.create`, `items.process` |
| `lab-viewer` (composite `viewer`) | browser | `items.read` |
| `lab-outsider` | browser | none; `items.create` only as a realm role and as a `work-items-gateway` client role |
| `work-items-automation` | bearer | `items.read`, `items.process` |
| `work-items-lab-admin` (not a Work Items principal) | Keycloak admin API on the internal `idp` network | `realm-management`: `view-clients`, `manage-users` |

- **Baseline control** runs the same unauthorized actions against an API image built from commit `0682b5275998d08c88476429ba6558ebf945417f` (main after #134, before authorization).
- **Method and its limits:** the automated checks use scripted clients (curl with cookie jars) and probe containers; no browser was used. Trusting the separate lab's CA and the manual Brave check are left to the operator. The Windows trust store was not changed.

## Results

Evidence: [`evidence/issue-121/authorization/`](../../evidence/issue-121/authorization).

| Check | Result |
| --- | --- |
| Go unit and request tests (claims parsing, permission matrix per route, 401 before 403, zero-permission registration) | pass |
| `verify-admin-permissions` (`admin-permissions.json`) | 4 / 4 |
| `verify-realm` (`verify-realm.json`) | 11 / 11 |
| `verify-authorization`, full run including role removal (`verify-authorization.json`) | 72 pass, 12 observations, 0 fail; rerun after the revocation observations were split into separate first-observed and boundary entries, with the same values |
| `scripts/test-security-lab-startup-warning.sh` (simulated token responses) | 14 / 14 (10 before the review fixes) |
| `verify-authorization` rerun after the review fixes (`verify-authorization-rerun.json`) | 79 pass, 14 observations, 0 fail; see [review fixes](#review-fixes-and-superseded-results) |
| `verify` (`verify.json`) | 79 pass, 4 observations, 0 fail; the Windows "no lab CA" control ran, because Windows does not trust this project's CA |
| `verify-failures` (`missing-secret-failures.json`) | 10 / 10 |
| `verify-authentication`, adapted to the machine client's permissions (`verify-authentication.json`) | 77 pass, 10 observations, 0 fail |
| `verify-log-hygiene` (`log-hygiene.json`) | 0 of 29 searched strings in ingress and gateway logs; includes the new principals' passwords and the role-management secret |

### Role-management client: minimum permissions

The experiment ran in a throwaway project (`zero-to-prod-security-lab-probe`).
Each run imported the realm into a fresh identity database with a different
`realm-management` role set for `work-items-lab-admin`. It then performed the
ten admin operations that `verify-realm` and `verify-authorization` need:
- look up the client, its roles, a composite, a role, a user, and that user's effective roles;
- look up the machine client and its service account;
- remove and re-add a role mapping.

| Role set | Result |
| --- | --- |
| Candidate: `view-clients`, `query-clients`, `view-users`, `query-users`, `manage-users` | all operations pass |
| Without `view-clients` | client lookup answers 200 but returns no usable client; every dependent operation is blocked |
| Without `query-clients`, `view-users`, or `query-users` (each dropped in turn) | all operations pass |
| Without `manage-users` (from the reduced set) | user lookup and both role-mapping changes: 403 |
| **Final: `view-clients`, `manage-users`** | all pass; dropping either breaks an operation |

The configured role set equals the final set. The throwaway project and its
state were removed afterwards. **Lifecycle:** the client is created with each
realm the tool imports and used only by `verify-realm` and
`verify-authorization`; its secret is a generated file passed on standard
input. It is removed by `destroy` (identity database) and `purge-state`
(secret).

### Realm readiness

Readiness is read from the realm through the admin API, not inferred from one
person's token:

- the `work-items-api` client and its five roles;
- the `editor` and `viewer` composites;
- the effective roles of all four Work Items principals (`lab-outsider`: none);
- the misplaced-claim setup;
- the role-management client's own roles.

No password grant was enabled.

### Permission matrix, browser and machine paths

The editor created fixtures through its browser session before each matrix
run. Every unauthorized action used a fixture on which the pre-authorization
API succeeds, so a business error cannot mask the result.

| Unauthorized action | Current API | Baseline API (`0682b52`) |
| --- | --- | --- |
| `lab-viewer` `POST /items` | 403 `insufficient_permissions`; item count unchanged | 201 |
| `lab-viewer` `POST /items/{pending}/process` | 403; the item still `pending` after 5 s (no job ran) | 202 |
| `lab-outsider` `GET /items` | 403 | 200 |
| `lab-outsider` `GET /processing-jobs/{existing}` | 403 | 200 |
| `lab-outsider` `POST /items/{pending}/process` | 403; the item still `pending` | 202 |
| `work-items-automation` `POST /items` | 403; item count unchanged | 201 |

Allowed actions succeeded on both APIs:
- editor: create, then process and poll to `succeeded`;
- viewer: list, and read an existing job;
- automation: process and poll to `succeeded`, and read an existing job.

After the control, the current API was restored, and `lab-viewer`'s create
was refused again.

### Claims and ordering

| Check | Result |
| --- | --- |
| `lab-outsider`: `items.create` only as a realm role and a gateway-client role | 403 |
| Issuer-signed ID token for another audience | 401 (authentication fails first) |
| `lab-viewer` token with `items.create` added under its original signature | 401 (signature) |
| `lab-viewer` processing a done item (editor control: 409) | 403 before 409 |
| `lab-viewer` processing an unknown item (editor control: 404 `work_item_not_found`) | 403 before 404 |
| `lab-outsider` reading an unknown job | 403 before 404 |

Malformed role data (non-string entries, wrong types at any level) is covered
by unit tests with signed tokens and grants no permissions. No such token was
produced through Keycloak in this experiment.

### Authorization decisions versus CSRF rejections

| Request | Response | API log | Ingress access log |
| --- | --- | --- | --- |
| `lab-viewer` create (authorization denial) | 403 `insufficient_permissions` | exactly one `authorization denied` line | `upstream=403` |
| `lab-user` create without the UI header (CSRF) | 403 `csrf_rejected` | no authorization line | `upstream=-` |

No API authorization line contained a JWT.

### Role removal (measured)

`lab-user`'s `editor` mapping and the automation client's `items.read` mapping
were removed at the same moment, 21 s after the browser session signed in.
Three credentials were then polled every 5 s.

The first column is the first poll that observed a rejection. The change
happened between that poll and the last successful request before it, which
was one polling cycle earlier. A cycle is a 5 s sleep plus the time the
requests and the shell took; those were not measured, so the interval has no
strict upper bound in this record. The second column is the boundary computed
from the token itself, which the polls did not measure directly.

| Credential | First observed rejection after removal (polling every 5 s plus request time) | Computed boundary |
| --- | --- | --- |
| Browser session (gateway refreshes after 1 min of session age) | 43 s, 403 | refresh due at about 39 s (session age 21 s at removal, 60 s refresh threshold), applied on the next request |
| Browser token captured before removal, used directly as a bearer token | 312 s, 401 (expired) | `exp` + 30 s skew at 309 s |
| Machine token issued before removal | 312 s, 401 (expired) | `exp` + 30 s skew at 310 s |
| Machine token issued after removal | first request, 403 (its roles: `items.process` only) | — |

Each first-observed rejection came at or after its computed boundary. The
last successful request came one polling cycle earlier, consistent with
rejection at the boundary. The pre-removal tokens were accepted on every
poll before that. The captured browser token was refused as expired,
not by role, because it still carried the removed role. Restoring both
mappings made new sessions and new tokens work again (201, 200).

### Review fixes and superseded results

A review found three gaps. They were fixed, and the affected checks were run
again in the same isolated lab
(`verify-authorization-rerun.json`: 79 pass, 14 observations, 0 fail). The
earlier `verify-authorization.json` is kept unchanged. The original lab was
not touched.

| Gap in the earlier run | Fix | Rerun result |
| --- | --- | --- |
| **Revocation polling** counted any non-200 response as a token's rejection, so a timeout or 5xx would have been reported as revocation | A session counts as rejected only on 403 `insufficient_permissions`, and a pre-removal token only on 401. Any other response is counted separately and polling continues. The suite asserts each rejection status, that both tokens were rejected at or after their `exp`, and that the API logged exactly two `expired` authentication rejections after removal | Session 403 `insufficient_permissions`; captured browser token and machine token 401 `unauthenticated`, both at or after `exp`; API: `expired=2`; 0 other responses. First-observed times unchanged: 43 s, 312 s, 312 s |
| **Authorization log hygiene** was not asserted for the denial flows; the API log was only checked for a JWT prefix | The suite collects the actual credentials of the denied principals: `lab-viewer` and `lab-outsider` passwords, session cookies, the access tokens their sessions forwarded, and the machine token used for denials. It searches the run's API log for them and their percent-encoded forms (9 strings), and requires zero matches. Controls: the scanner finds all 9 in a synthetic log, and the captured log contains both principals' denial lines. A match fails the suite | 0 matches; both controls passed |
| **Startup check** ignored non-string role entries, unlike the API parser | Any malformed role data invalidates the whole list | 14 / 14 simulated cases, including both expected permissions plus a non-string entry |

**Superseded:** in `verify-authorization.json`, the role-removal rejection
results (counted on any non-200) and the absence of a credential search of
the API log. The rerun confirms the earlier matrix, claims, ordering, logs,
outage, and baseline-control results with the same outcomes.

### Startup permission check

`start` reads a token for the machine client and warns when it lacks the
permissions that client is configured to hold (`items.read`,
`items.process`). The warning names what is missing and recommends
`verify-realm`. It does not conclude a cause: a missing permission can come
from realm configuration, a changed role mapping, or a failed token request.
It does not suggest cleanup.

Two different kinds of evidence:

- **Simulated token responses** (`scripts/test-security-lab-startup-warning.sh`,
  also run in CI): 14 cases, all passing, run without a lab, Docker, or
  network:
  - expected permissions present, with or without other roles: silent;
  - no `resource_access`, no `work-items-api` entry, one permission missing, or names only under another client: the missing permissions are named;
  - any malformed role data invalidates the whole list, as in the API parser: a non-string entry (also next to both expected permissions), roles that are not a list, or a `resource_access` that is not an object. The warning says the role data is malformed and names both permissions as missing;
  - an empty, error, or non-JWT response: "could not be checked".

  Every warning recommends `verify-realm` and contains no cause or cleanup advice. Before the review fix, the check ignored non-string entries and kept the rest. Run against the previous version, the case with both permissions plus a `null` entry printed nothing; it now fails that version.
- **Runtime:** one `start` of the authorization lab after the change printed no warning (permissions present). The warning path has not been exercised at runtime: the original lab, whose realm lacks the roles, was not started or changed.

### Identity provider down, keys cached

With Keycloak stopped:
- `lab-viewer`: list 200, create 403;
- `lab-user`: create 201;
- automation: list 200, create 403.

Authorization needs no call to the identity provider.

### Manual browser verification (Brave, 2026-10-12)

The operator ran the authorization part of the
[manual browser check](../guides/local-security-lab.md#manual-browser-check-brave)
in the isolated authorization lab. The Brave version was not recorded.

**Visible in screenshots** (three: two of `lab-viewer`, one of `lab-user`):

- `lab-viewer` screenshots:
  - signed in as `lab-viewer`;
  - the items list;
  - "Not allowed";
  - the open Create dialog obscuring the page-level error.
- `lab-user` screenshot:
  - item #14 ("viewer create") processed successfully;
  - in Network, 202 for the processing request and 200 for polling;
  - DevTools device emulation enabled.

**Confirmed verbally by the operator, not shown in a screenshot:**

- `lab-viewer`: Create and Process were both denied with "Not allowed", and the session remained active.
- `lab-user`: item #14 showed status `done`, job `succeeded`, and a result of 13 characters and 2 words.
- `lab-user`: creating a new item succeeded.
- The authorization lab was stopped afterward.

**Not recorded:**

- the HTTP status of the viewer's denied requests (no screenshot or report covers it);
- browser settings, such as device emulation, outside the `lab-user` screenshot.

**Defect found:** the viewer's Create denial was shown in the page-level
alert, behind the open dialog and its blurred overlay, rather than in the
dialog. The UI now shows create failures inside the dialog as an alert, keeps
the entered title, and clears the message when the title is edited, a new
attempt starts, or the dialog is opened or closed. That change was checked
with lint and a production build only; it has not been retested in a
browser.

## Limitations

- **Coarse rules:** role-based authorization only, with no per-item or ownership rules.
- **Revocation:** bounded by token lifetime. A token issued before a role removal keeps working until expiry plus skew; browser sessions pick up changes at the gateway's next refresh.
- **No upgrade of existing realms:** the original lab is not upgraded. With the current API, its realm (no `work-items-api` roles) would refuse every Work Items request. The startup permission check would name the machine client's missing permissions and recommend `verify-realm`; that path was tested only with simulated token responses (see [startup permission check](#startup-permission-check)).
- **Standing privileged credential:** `work-items-lab-admin` exists in every realm the tool creates. It is internal-network only, its secret is a generated file passed on standard input, and it is removed with the lab.
- **Route-registration safety:** it prevents accidental omission inside the API package, not a handler deliberately registered on the mux in the constructor. Request tests cover only the routes in the matrix.
- **UI:** it explains a 403 after the fact and does not know permissions in advance. One manual Brave session checked it (see [manual browser verification](#manual-browser-verification-brave-2026-10-12)); the corrected Create dialog has not been retested in a browser.
- **Single runs.**

## Capability levels

No change.
