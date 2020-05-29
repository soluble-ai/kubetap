# Caveats

As with everything, there are some caveats and pitfalls.

## Kubetap Usage

### High-Volume Taps

If the target Service has a high request to pod ratio, you may encounter
memory issues, as mitmweb retains all requsets in memory. If you are in this
situation but willing to sacrafice the realtime interactive interface, you can
use the `--comand-args` flag to modify the sidecar command to use mitmdump. Or,
you can use a custom image using the `--image` flag to provide your own custom tooling.

### Custom Images

You can use a custom proxy image using the `--image` flag, but be warned that
there are no compatibility guarantees at the present time. In the future, support
for additional proxy types and configurations may be added. Until then, there is
no official support for custom images. You're on your own.

### Kubernetes dashboard

The kubernetes dashboard cannot be tapped by kubetap because the dashboard
does not have a Pod for a sidecar to be injected into.

### >1 Replica

If there are multiple replicas, the proxy sidecar will be deployed to all replicas.
This behavior ensures that all Service traffic is collected, but never leaves the
local Pod during the proxy.

Connecting to (what the situation dictates being) the **correct** proxy is left
to the operator, as it is not possible for Kubetap to know the circumstances of a
given environment and desired proxy configuration.

### Ports 7777 and 2244

These are "magic ports" used by kubetap. The former is used as the proxy listener
and the latter is the proxy web interface.

If the target Pod container has already reserved these ports, kubetap will fail.

We should eventually automate incrementing and fixing this, but for now kubetap
cannot target ports 7777 or 2244.

### Security Restrictions

If you use PSPs, the target Pod will need access to `ConfigMap`s and `EmptyDir`s.

If the target Pod does not have access to these resources, kubetap will fail.
