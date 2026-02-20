# Connectivity Guide

Legator agents live on Kubernetes but manage infrastructure *beyond* the cluster — servers, databases, network devices, cloud resources. The connectivity layer handles how agents reach those targets.

## Connectivity Types

### Direct (default)

No mesh VPN. Agents reach targets via the cluster's existing network — suitable when targets are on the same network or reachable via existing VPN/firewall rules.

```yaml
spec:
  connectivity:
    type: direct
```

Or simply omit the `connectivity` field entirely.

### Headscale (recommended)

Self-hosted WireGuard mesh VPN. Agents connect to targets through an encrypted overlay network, traversing NAT and firewalls automatically. Free, no vendor dependency.

```yaml
spec:
  connectivity:
    type: headscale
    headscale:
      controlServer: "https://headscale.example.com"
      authKeySecretRef: headscale-auth-key
      tags:
        - "tag:agent-runtime"
      acceptRoutes: true
```

**Prerequisites:**
1. Headscale server running and accessible
2. Pre-auth key generated: `headscale preauthkeys create --user legator --expiration 8760h`
3. Target servers running Tailscale client, or a subnet router on the target VLAN
4. ACL policy allowing `tag:agent-runtime` to reach target tags/ports

**Helm values to enable the sidecar:**

```yaml
connectivity:
  enabled: true
  controlServer: "https://headscale.example.com"
  authKeySecret: headscale-auth-key
  authKeySecretKey: key
  hostname: legator-controller
  acceptRoutes: true
```

### Tailscale

Commercial SaaS alternative to Headscale. Same WireGuard protocol, managed control plane.

```yaml
spec:
  connectivity:
    type: tailscale
    headscale:
      controlServer: ""  # Uses Tailscale's control plane
      authKeySecretRef: tailscale-auth-key
      tags:
        - "tag:agent-runtime"
```

## How It Works

### Architecture

```
┌─────────────────────────────────────────────┐
│ Agent Pod                                    │
│  ┌──────────────┐  ┌──────────────────────┐ │
│  │ Controller   │  │ Tailscale Sidecar    │ │
│  │ (legator)    │  │ (WireGuard mesh)     │ │
│  │              │──│                      │ │
│  │ SSH/SQL/HTTP │  │ 100.64.x.x overlay  │ │
│  │ tools        │  │ Encrypted tunnels    │ │
│  └──────────────┘  └──────────────────────┘ │
└─────────────────────────────────────────────┘
            │                    │
            │         WireGuard tunnels
            │                    │
    ┌───────┴──────┐    ┌───────┴──────────┐
    │ Target       │    │ Subnet Router    │
    │ (Tailscale)  │    │ (Tailscale)      │
    │ centos-proxy │    │ Exposes entire   │
    │ 100.64.0.5   │    │ 10.20.5.0/24     │
    └──────────────┘    └──────────────────┘
```

### Pre-Run Health Check

Before each agent run, the connectivity manager verifies:
1. **Sidecar health** — Tailscale daemon is running and connected
2. **Endpoint reachability** — TCP dial to each configured endpoint

If the sidecar is not ready, the check logs a warning. If critical endpoints are unreachable, the agent is informed.

### Network Scoping via ACLs

Headscale ACLs restrict which targets each agent can reach:

```json
{
  "acls": [
    {
      "action": "accept",
      "src": ["tag:agent-runtime"],
      "dst": ["tag:managed-servers:22"]
    },
    {
      "action": "accept",
      "src": ["tag:agent-runtime"],
      "dst": ["tag:managed-databases:5432,3306"]
    }
  ]
}
```

### Subnet Routers

For targets that can't run a Tailscale client (network devices, legacy systems, entire VLANs):

1. Install Tailscale on a machine in the target network
2. Advertise the subnet: `tailscale up --advertise-routes=10.20.5.0/24`
3. Approve in Headscale: `headscale routes enable --route 10.20.5.0/24`
4. Enable `acceptRoutes: true` in the environment spec

The agent can now reach any host on `10.20.5.0/24` through the subnet router.

## Credential Integration

Connectivity (Layer 1) handles network reachability. Credentials (Layer 3) handle authentication. They work together:

1. Headscale provides encrypted network path to target
2. Vault issues ephemeral credentials per-run (SSH cert, DB user)
3. Tool layer injects credentials automatically
4. LLM never sees credentials
5. Credentials expire with the run

See the [Vault integration guide](environment-binding.md) for credential configuration.

## Deployment Without Mesh VPN

If Headscale/Tailscale is not suitable (e.g., firewall rules already exist), use `type: direct`:

- Agents use the cluster's existing network
- Static SSH credentials via Vault or Kubernetes Secrets
- No sidecar container
- Works for same-network and pre-configured VPN scenarios

## Troubleshooting

### Sidecar not starting
- Check the Secret exists: `kubectl get secret <authKeySecret> -n <namespace>`
- Verify the pre-auth key hasn't expired
- Check sidecar logs: `kubectl logs <pod> -c tailscale`

### Endpoint unreachable
- Verify the target is on the Headscale network: `headscale nodes list`
- Check ACL policy allows traffic from `tag:agent-runtime`
- For subnet routes, verify the route is approved: `headscale routes list`
- Check firewall rules on the target host

### Performance
- WireGuard overhead is minimal (~3-5ms added latency)
- Subnet routers add one extra hop
- DERP relays (used when direct peer-to-peer fails) add more latency
