#!/usr/bin/env zsh

script_dir=${0:A:h}
#source ${script_dir}/_pre.zsh

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
helm repo add stable https://charts.helm.sh/stable --force-update
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
trap '{ e=${?}; sleep 1; kind delete cluster --name kubetap ; exit ${e} }' SIGINT SIGTERM EXIT
kind create cluster --name kubetap

#
# Test kubetap using helm ${chart}
#
_kubetap_helm_charts=('stable/grafana' 'stable/dokuwiki')
_kubetap_helm_services=('grafana' 'dokuwiki')
_kubetap_helm_svc_port=('80' '80')

typeset -i _kubetap_iter
for chart in ${_kubetap_helm_charts[@]}; do
  ((_kubetap_iter+=1))
  _kubetap_helm=${chart:t}
  _kubetap_port=${_kubetap_helm_svc_port[${_kubetap_iter}]}
  _kubetap_service=${_kubetap_helm_services[${_kubetap_iter}]}

  helm install --kube-context kind-kubetap ${_kubetap_helm} ${chart}
  kubectl tap on ${_kubetap_service} -p${_kubetap_port} --context kind-kubetap
  sleep 20

  _kubetap_ready_state=""
  for i in {0..20}; do
    sleep 6
    _kubetap_pod=($(kubectl --context kind-kubetap get pods -ojsonpath='{.items[*].metadata.name}'))
    if (( ${#_kubetap_pod} != 1 )); then
      continue
    fi
    _kubetap_ready_state=$(kubectl --context kind-kubetap get pod ${_kubetap_pod} -ojsonpath='{.status.containerStatuses[*].ready}')
    if [[ ${_kubetap_ready_state} == 'true true' ]]; then
      break
    fi
  done
  if [[ ${_kubetap_ready_state} != 'true true' ]]; then
    echo "container did not come up within 90 seconds"
    return 1
  fi
  unset _kubetap_pod _kubetap_ready_state i

  sleep 1
  kubectl port-forward svc/${_kubetap_service} -n default 2244:2244 &
  _kubetap_pf_one_pid=${!}
  kubectl port-forward svc/${_kubetap_service} -n default 4000:${_kubetap_port} &
  _kubetap_pf_two_pid=${!}
  sleep 5

  # check that we can reach both services
  curl -v http://127.0.0.1:2244 || return 1
  curl -v http://127.0.0.1:4000 || return 1
  # TODO: should also check the mitmproxy JSON resp body to check that it's connected
  kill ${_kubetap_pf_one_pid}
  kill ${_kubetap_pf_two_pid}
  unset _kubetap_pf_one_pid _kubetap_pf_two_pid

  # cleanup test
  kubectl tap off ${_kubetap_service} --context kind-kubetap
  helm delete --kube-context kind-kubetap ${_kubetap_helm}

  unset _kubetap_helm _kubetap_port _kubetap_service
done
unset _kubetap_helm_charts _kubetap_helm_services _kubetap_helm_svc_port _kubetap_iter

#source ${script_dir}/_post.zsh
