# TODOs

## Features

### Raw capture

TCP/UDP capture to pcap.

* looks like there's already a [kubectl plugin](https://github.com/eldadru/ksniff)
for this, but the implementation by uploading binaries into running Pods is not ideal:

> ksniff use kubectl to upload a statically compiled tcpdump binary to your pod
> and redirecting it's output to your local Wireshark for smooth network debugging
> experience.

* Correct implementation non-trivial, as it involves multi-process management,
potentially modifying the security context to allow capture, and exporting data
to the client.

* Data export implementation still undecided, options under consideration:
  * tcpdump + ( FF Send || S3 || PVC || stream )
  * sharkd stream to operator
  * webshark interactive interface ([stale project](https://bitbucket.org/jwzawadzki/webshark/src))

### "Burp" mode

* Blocked by [kubernetes/kubernetes#20227](https://github.com/kubernetes/kubernetes/issues/20227)
* Reverse-proxy from container to host using native Kubernetes tools is ideal,
  but there are alternatives to consider in the interim such as [ktunnel](https://github.com/omrikiei/ktunnel)
* If raw capture feature is added, implementing our own tunnel is significantly
less of a challenge.

### gRPC support

There is a nice proxy library [here](https://github.com/mwitkow/grpc-proxy), by mwitkow.
It should be feasible to use this to route traffic to an operator. Thanks for
the link, [@rakyll](https://github.com/rakyll)!

### Sidecar yaml

* Optionally ingest sidecar configuration through yaml file.
* This is an optional feature, and will likely only be necessary for
those few environments that require special configuration to function. We would
like to support these environments, but in that case it's up to the operator
to configure. Allowing yaml sidecar definition is the path to enable that.

## Architecture

### Use Ephemeral Containers once in beta

Don't want to force alpha features on anyone, but once this is in beta this will
probably be the way forward. Will revisit.

https://kubernetes.io/docs/concepts/workloads/pods/ephemeral-containers/

## Management

### Add DCO

https://probot.github.io/apps/dco/

