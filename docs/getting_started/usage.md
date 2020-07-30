# Usage

Kubetap's binary is `kubectl-tap`. This allows kubetap to be invoked as
`kubectl tap`.

Kubetap inherits many options from the `kubectl` command, including useful
options such as:

`--context`, `--user`, `--as`, etc.

## Tap On

Deploy a proxy to tap the target Service, in the case of this example,
the `argocd-server` Service's exposed port `443` which uses HTTPS.

```sh
kubectl tap on -n argocd argocd-server -p443 --https
```

## Tap Off

Remove the tap from the `argocd-server` Service.

```sh
kubectl tap off -n argocd argocd-server
```

## Tap List

The namespaces can be constrained with `-n`, but by default it lists taps in
all namespaces:

```sh
$ kubectl tap list
Tapped Namespace/Service:

argocd/argocd-server
```

# In a container

It is possible to schedule kubetap as a Pod in Kubernetes using the
`grc.io/soluble-oss/kubectl-tap:latest` container. When run in a cluster,
kubetap will automatically detect and use ServiceAccount tokens that are
mounted to the container's filesystem.

Additionally, it is possible to run the containers from a developer laptop as follows:

```sh
docker run -v "${HOME}/.kube/:/.kube/:ro" 'gcr.io/soluble-oss/kubectl-tap:latest' on -n mynamespace -p80 myservice
```

```sh
docker run -v "${HOME}/.kube/:.kube/:ro" 'gcr.io/soluble-oss/kubectl-tap:latest' off -n mynamespace myservice
```

## Image variations

Kubetap is built on alpine, and available at `gcr.io/soluble-ass/kubectl-tap`.
Images are distributed under two major tags:

| Image and tag                           | Description                                                                 |
| ---                                     | ---                                                                         |
| `gcr.io/soluble-oss/kubectl-tap:latest` | Alpine build and `scratch` execution environment. Tiny container, no shell. |
| `gcr.io/soluble-oss/kubectl-tap:alpine` | Alpine build and execution environment. Useful for debugging, has a shell.  |

