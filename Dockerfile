FROM registry.svc.ci.openshift.org/openshift/release:golang-1.10 AS builder
WORKDIR /go/src/github.com/openshift/cluster-externaldns-operator
COPY . .
RUN make build

FROM registry.svc.ci.openshift.org/openshift/origin-v4.0:base
COPY --from=builder /go/src/github.com/openshift/cluster-externaldns-operator/externaldns-operator /usr/bin/
COPY manifests /manifests
RUN useradd externaldns-operator
USER externaldns-operator
ENTRYPOINT ["/usr/bin/externaldns-operator"]
LABEL io.openshift.release.operator true
LABEL io.k8s.display-name="OpenShift externaldns-operator" \
      io.k8s.description="This is a component of OpenShift Container Platform and provides external DNS management." \
      maintainer="Dan Mace <dmace@redhat.com>"
