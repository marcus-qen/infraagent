# Changelog

All notable changes to this project will be documented in this file.

## [v0.3.0] — 2026-02-20

### Added

#### Vault Integration
- `internal/vault/client.go` — HashiCorp Vault API client with K8s auth and token auth
- SSH Certificate Authority signing: short-lived SSH certs (5-min TTL) per agent run
- Dynamic database credentials: Vault creates temporary DB users, auto-revoked on expiry
- KV v2 secret reading for static secrets (API keys, tokens)
- `CredentialManager` with per-run lifecycle: request at start → inject into tool → revoke at end
- LLM never sees credentials — all injection happens at the tool layer
- New credential types in CRD: `vault-kv`, `vault-ssh-ca`, `vault-database`
- `VaultCredentialSpec` and `VaultConfig` CRD types
- `RunConfig.Cleanup` wired into runner for automatic lease revocation + key zeroing
- 17 Vault-specific tests

#### SQL Tool
- `sql.query` tool for read-only database queries (PostgreSQL + MySQL)
- Driver-level read-only enforcement via `sql.TxOptions{ReadOnly: true}`
- Four-tier query classification: SELECT (read) / CREATE INDEX (service) / DROP TABLE (destructive) / INSERT (data)
- SQL injection detection: multi-statement, comment injection, suspicious UNION patterns
- Result truncation: configurable max rows (default 1000) and max bytes (default 8KB)
- `pgx` (PostgreSQL) and `go-sql-driver/mysql` database drivers
- Vault credential injection: `requestVaultDBCredentials()` creates ephemeral DB users per-run
- `buildSQLDatabases()` constructs DSNs from environment credentials
- SQL protection class added to built-in defaults (DELETE, INSERT, UPDATE, DROP all blocked)
- 16 SQL-specific tests
- 3 example agents: `database-health-monitor`, `schema-drift-detector`, `query-performance-auditor`
- Example environment with Vault database credentials

#### Headscale/Tailscale Connectivity
- `ConnectivitySpec` CRD type: `direct`, `headscale`, `tailscale`
- `HeadscaleConnectivity`: control server, auth key, ACL tags, hostname, accept routes, exit node
- `internal/connectivity/` package: health checks, endpoint reachability, pre-run validation
- Tailscale sidecar in Helm chart (optional, disabled by default)
- Userspace mode, shared Unix socket for controller ↔ sidecar communication
- Pre-run connectivity check wired into RunConfigFactory
- `docs/connectivity.md`: architecture, ACLs, subnet routers, troubleshooting
- Example environment (`headscale-environment.yaml`) with Headscale + Vault
- Example agent (`server-health-monitor`) using SSH via Headscale mesh
- 12 connectivity tests

### Changed
- Protection engine now includes SQL protection class as built-in (alongside K8s and SSH)
- Environment resolver exposes `Connectivity` field
- Updated protection engine tests for 4 built-in classes (was 3)
- 409 total tests across 18 packages (was 360 across 17)

## [v0.2.0] — 2026-02-20

### Added

#### SSH Tool
- `ssh.exec` tool for executing commands on remote servers via `golang.org/x/crypto/ssh`
- 150+ commands auto-classified into four action tiers (read/service/destructive/data)
- Blocked command list: `dd`, `mkfs`, `fdisk`, `parted`, `psql`, `mysql`, `mongo`, `mongosh`, `redis-cli`, `shred`, `srm`, `wipefs`
- Protected paths: `/etc/shadow`, `/etc/gshadow`, `/boot/`, `/dev/`, SSH keys
- Per-host sudo and root login controls (opt-in)
- Connection pooling (reuse within a run), 8KB output truncation, configurable timeouts
- Automatic credential injection from LegatorEnvironment secrets
- Subcommand matching (e.g., `mkfs.ext4` → `mkfs` → blocked)

#### Tool Capability Framework
- `ClassifiableTool` interface: tools declare capabilities and classify actions
- `ToolCapability` struct: domain, supported tiers, credential/connection requirements
- `ActionClassification` struct: tier, target, description, blocked status, block reason
- Domain inference from tool names (`kubectl.*` → kubernetes, `ssh.*` → ssh, `mcp.X.*` → X)

#### Protection Engine
- Configurable protection classes with glob-style pattern matching
- `ProtectionClass` / `ProtectionRule` types with block/approve/audit actions
- Built-in Kubernetes protection class (PVC, PV, namespace, DB CR deletion)
- Built-in SSH protection class (shadow file, disk tools, partition tools)
- User-extensible: add protection classes for SQL, HTTP, cloud APIs, etc.
- Built-in rules cannot be weakened by user classes
- Wired into Action Sheet Engine via `WithProtectionEngine()`

#### CLI (`legator` binary)
- `legator agents list` — tabular view of agents with phase, autonomy, schedule
- `legator agents get <name>` — detailed agent view with emoji, description, config
- `legator runs list [--agent NAME]` — recent runs sorted newest-first with emoji status
- `legator runs logs <name>` — full run detail: actions, guardrails, report, errors
- `legator status` — cluster summary: agents, environments, runs, success rate, tokens
- `legator version` — version and git commit info

#### Example Agents
- `legacy-server-scanner` — SSH into servers, parse web configs, produce migration report
- `patch-compliance-checker` — SSH into fleet, audit OS/kernel/packages
- `log-rotation-auditor` — SSH into servers, check logrotate config, disk pressure
- `server-fleet` environment example with SSH credentials

### Changed

#### Renamed from InfraAgent
- Repository: `marcus-qen/infraagent` → `marcus-qen/legator`
- Go module: `github.com/marcus-qen/legator`
- API group: `legator.io/v1alpha1` (from `core.infraagent.io/v1alpha1`)
- CRD kinds: `LegatorAgent`, `LegatorEnvironment`, `LegatorRun` (from `InfraAgent`, etc.)
- Helm chart: `charts/legator/`

#### Guardrail Engine
- `WithProtectionEngine()` — optional configurable protection class evaluation
- `WithToolRegistry()` — optional ClassifiableTool-based action classification
- Protection engine check runs after hardcoded data protection (defence in depth)
- ClassifiableTool blocks fire before engine evaluation (double enforcement)
- `inferDomain()` and `mapToolTierToAPITier()` helper functions

### Fixed
- CiliumNetworkPolicy DNS egress for legator-system namespace
- RBAC kubebuilder markers: resource plural names match CRD plurals

## [v0.1.0] — 2026-02-20

Initial release. See [RELEASE-NOTES-v0.1.0.md](RELEASE-NOTES-v0.1.0.md).

### Features
- Kubernetes operator with 4 CRDs (InfraAgent, InfraAgentEnvironment, InfraAgentRun, ModelTierConfig)
- Scheduled execution (cron, interval, webhook, annotation triggers)
- Four-tier action classification with runtime-enforced guardrails
- Graduated autonomy (observe → recommend → automate-safe → automate-destructive)
- Action Sheet Engine with allowlist enforcement
- Hardcoded data protection (PVC, PV, namespace, DB CRD deletion blocked)
- LLM provider abstraction (Anthropic, OpenAI, any OpenAI-compatible)
- MCP tool integration (Go SDK v1.3.1, Streamable HTTP)
- Skill distribution (Git sources, ConfigMap, bundled)
- Multi-cluster support with remote kubeconfig client factory
- Reporting and escalation (Slack, Telegram, webhook channels)
- 9 Prometheus metrics, OTel tracing, Grafana dashboard
- AgentRun retention with TTL cleanup
- Rate limiting (per-agent + cluster-wide concurrency)
- Credential sanitization in audit trail
- 253 unit tests across 17 packages

### Dogfooding
- 10 agents running on 4-node Talos K8s cluster
- 277+ runs, 82% success rate
- Token usage: ~3K to ~42K per agent per run
