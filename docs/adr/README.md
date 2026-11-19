# Design decisions

Read these when you need the reason for a design choice. Setup belongs in the
guides; recovery procedures belong in runbooks. A `Proposed` status does not
become `Accepted` merely because code exists.

| Decision | Topic |
|---|---|
| [0001](0001-work-items-async-recovery-boundary.md) | Durable accepted-work recovery boundary |
| [0002](0002-work-items-web-ui-boundary.md) | Separate browser UI artifact |
| [0003](0003-work-items-async-success-semantics.md) | Results and atomic business completion |
| [0004](0004-security-lab-authentication.md) | Identity provider and session gateway |
| [0005](0005-security-lab-authorization.md) | API authorization |
| [0006](0006-security-lab-internal-transport.md) | TLS to database and broker |
| [0007](0007-security-lab-identity-provider-transport.md) | TLS to identity provider |
| [0008](0008-security-lab-application-transport.md) | TLS to API and gateway |
| [0009](0009-restore-delivery-recovery.md) | Delivery identity and offline redrive |
| [0010](0010-security-lab-rate-limiting.md) | Ingress admission limits |
| [0011](0011-security-lab-secret-distribution-rotation.md) | Secrets and database password rotation |
| [0012](0012-work-items-observability.md) | Logs, metrics and traces |
| [0013](0013-local-capacity-measurement.md) | Capacity measurement |
| [0014](0014-local-availability-recovery.md) | Local failover and recovery |
| [0015](0015-safe-local-delivery.md) | Immutable blue/green delivery |
