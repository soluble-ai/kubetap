#!/usr/bin/env zsh

script_dir=${0:A:h}
source ${script_dir}/_pre.zsh

#
# Prep env
#

KIND_VERSION=v0.8.1
HELM_VERSION=v3.2.1

# modfile hack to avoid package collisions and work around azure go-autorest bug
print "module kubetap-ig-tests

go 1.13

require (
)

replace (
  github.com/Azure/go-autorest => github.com/Azure/go-autorest v13.0.0+incompatible
)" >! ig-tests.mod

# return error if kubectl is not available
if [[ =kubectl == '' ]]; then
  echo "kubectl not installed"
  return 1
fi

# install helm if not available
if [[ =helm == '' ]]; then
  GO111MODULE=on go get -modfile=ig-tests.mod helm/cmd/helm@${HELM_VERSION}
fi
helm repo add stable https://kubernetes-charts.storage.googleapis.com
helm repo update

# we use kind to establish a local testing cluster
GO111MODULE=on go get -modfile=ig-tests.mod sigs.k8s.io/kind@${KIND_VERSION}

#
# Establish a local testing cluster
#

# remove stale kubetap clusters if they exist
_kubetap_kind_clusters=$(kind get clusters 2>&1)
if [[ ${_kubetap_kind_clusters} == *'kubetap'* ]]; then
  kind delete cluster --name kubetap
fi
unset _kubetap_kind_clusters

# catch sigints and exits to delete the cluster, keeping the last exit code
trap '{ e=${?}; kind delete cluster --name kubetap ; exit ${e} }' SIGINT SIGTERM EXIT
kind create cluster --name kubetap

#
# Test kubetap using helm stable/grafana
#

# TODO: install helm
helm install --kube-context kind-kubetap grafana stable/grafana

kubectl tap on grafana -p80 --context kind-kubetap

typeset -i readyCt
for ((i=0; i <= 20; i++)); do
  readyCt=$(kubectl --context kind-kubetap get deployments.apps grafana -ojsonpath='{.status.readyReplicas}')
  if (( readyCt == 1)); then
    break
  fi
  sleep 6
done
if (( readyCt != 1 )); then
  echo "container did not come up within 90 seconds"
  return 1
fi
unset readyCt

# without a delay here, port forwards occasionally fail. Need
# to implement kubectl ready check like in kubetap.
sleep 15
kubectl port-forward svc/grafana -n default 2244:2244 &
_kubetap_pf_one_pid=${!}
kubectl port-forward svc/grafana -n default 4000:80 &
_kubetap_pf_two_pid=${!}
sleep 5

# check that we can reach both services
curl -v http://127.0.0.1:2244 || return 1
curl -v http://127.0.0.1:4000 || return 1
# TODO: should also check the mitmproxy JSON resp body to check that it's connected
kill ${_kubetap_pf_one_pid}
kill ${_kubetap_pf_two_pid}
unset _kubetap_pf_one_pid _kubetap_pf_two_pid

kubectl tap off grafana --context kind-kubetap

helm delete --kube-context kind-kubetap grafana

#
# Test kubetap using helm stable/dokuwiki
#

helm install --kube-context kind-kubetap dw stable/dokuwiki

kubectl tap on dw-dokuwiki -p80

typeset -i readyCt
for ((i=0; i <= 20; i++)); do
  readyCt=$(kubectl --context kind-kubetap get deployments.apps dw-dokuwiki -ojsonpath='{.status.readyReplicas}')
  if (( readyCt == 1)); then
    break
  fi
  sleep 6
done
if (( readyCt != 1 )); then
  echo "container did not come up within 90 seconds"
  return 1
fi
unset readyCt

# without a delay here, port forwards occasionally fail. Need
# to implement kubectl ready check like in kubetap.
sleep 15
kubectl port-forward svc/dw-dokuwiki -n default 2244:2244 &
_kubetap_pf_one_pid=${!}
kubectl port-forward svc/dw-dokuwiki -n default 4000:80 &
_kubetap_pf_two_pid=${!}
sleep 5

# check that we can reach both services
curl -v http://127.0.0.1:2244 || return 1
curl -v http://127.0.0.1:4000 || return 1
# TODO: should also check the mitmproxy JSON resp body to check that it's connected
kill ${_kubetap_pf_one_pid}
kill ${_kubetap_pf_two_pid}
unset _kubetap_pf_one_pid _kubetap_pf_two_pid

kubectl tap off dw-dokuwiki --context kind-kubetap

helm delete --kube-context kind-kubetap dw

source ${script_dir}/_post.zsh
