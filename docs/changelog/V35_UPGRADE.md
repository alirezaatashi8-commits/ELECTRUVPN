# ELECTRU PANNE V35 — Upgrade Scope

V35 integrates the ten requested upgrade areas into one operational surface rather than creating duplicate subsystems.

- **Multi-node:** existing ServerNode/AgentNode/NodeGroup/Fabric capabilities are surfaced through the command center.
- **Deployment:** existing deployment and rollback records remain the source of truth; preflight now reports readiness before an operator applies changes.
- **Monitoring:** command center aggregates Xray, sing-box, database, disk, node and workload state.
- **Security:** existing Security Center remains authoritative; preflight adds concrete runtime checks.
- **Alerting:** existing deduplicated AlertEvent + automation/scheduler stack is retained.
- **Disaster recovery:** latest-backup verification now checks archive readability, database inclusion and SQLite integrity.
- **Configuration:** existing templates and engine validation remain in place; V35 adds a non-mutating preflight gate.
- **Audit:** existing AuditLog/OperationRecord/Deployment history remains the canonical history.
- **UX:** Operations Center gets a unified command-center data source for a single operational view.
- **Analytics:** existing subscription, traffic, node performance and enterprise metrics remain integrated.

The implementation deliberately does not claim that remote orchestration, external object storage, or PostgreSQL are fully implemented merely because configuration fields exist. Those remain future integrations.
