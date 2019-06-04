#!/bin/bash
set -uo pipefail

WHAT="${WHAT:-managed}"

# Disable the CVO
oc scale --replicas 0 -n openshift-cluster-version deployments/cluster-version-operator

# Uninstall the ingress-operator
oc delete clusteroperator.config.openshift.io/externaldns
oc delete --force --grace-period=0 -n openshift-externaldns-operator externaldns/default-public-zone
oc patch -n openshift-externaldns-operator externaldns/default-public-zone --patch '{"metadata":{"finalizers": []}}' --type=merge
oc delete --force --grace-period=0 -n openshift-externaldns-operator externaldns/default-private-zone
oc patch -n openshift-externaldns-operator externaldns/default-private-zone --patch '{"metadata":{"finalizers": []}}' --type=merge
oc delete -n openshift-externaldns-operator deployments/externaldns-operator

if [ "$WHAT" == "all" ]; then
  oc delete namespaces/openshift-externaldns-operator
fi

oc delete namespaces/openshift-externaldns

if [ "$WHAT" == "all" ]; then
  oc delete clusterroles/openshift-externaldns-operator
  oc delete clusterrolebindings/openshift-externaldns-operator
  oc delete customresourcedefinition.apiextensions.k8s.io/externaldns.operator.openshift.io
fi
