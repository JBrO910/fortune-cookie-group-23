# Monitoring: kube-prometheus-stack

## Summary for course staff

**Question**: is there a shared Prometheus/Grafana already running for the course cluster, and if
so, how do we (group `student-2`) get access to view it?

**Why we're asking**: our `ServiceMonitor` objects (`monitoring.coreos.com/v1`, namespaced,
created in `student-2` via our Helm chart) deploy and work successfully, which means the
`ServiceMonitor` CustomResourceDefinition already exists on the cluster and is presumably being
reconciled by a running Prometheus Operator. However, `student-2`'s service account
(`system:serviceaccount:code-server-workstations:workstation-2-account`) has no cluster-scoped
read access at all, so we can't discover where that Operator/Prometheus/Grafana actually live, or
whether our metrics are actually being scraped. Evidence:

```
$ kubectl get crd | grep monitoring.coreos.com
Error from server (Forbidden): ... cannot list resource "customresourcedefinitions" ... at the cluster scope

$ kubectl get prometheus,alertmanager -A
Error from server (Forbidden): ... cannot list resource "prometheuses" ... at the cluster scope
Error from server (Forbidden): ... cannot list resource "alertmanagers" ... at the cluster scope

$ helm list -n student-2
NAME             NAMESPACE    REVISION  STATUS    CHART                  APP VERSION
fortune-cookie   student-2    4         deployed  fortune-cookie-0.1.0   1.0.0
```
(no monitoring-related Helm release in our own namespace — consistent with it living elsewhere)

**What we'd like from staff**: either (a) confirmation that a shared Prometheus/Grafana exists and
the Service name/namespace or URL to reach it, or (b) if nothing shared exists yet and student
groups are expected to self-host, the cluster-scoped permissions (create `ClusterRole`/
`ClusterRoleBinding`) needed to install `kube-prometheus-stack`'s Prometheus Operator ourselves —
see "Install" below for exactly what that requires.

Full technical detail and the exact commands run follow below.

---

This installs Prometheus + Grafana + the Prometheus Operator into `student-2`, scoped as tightly
as possible to that one namespace. It's a **separate Helm release** from the `fortune-cookie` app
chart (`../chart/`) — deliberately not wired into CI's `deploy` job. It has cluster-scoped
dependencies (CRDs, ClusterRoles) that need a human watching the output, not an unattended install
on every push.

## 1. Pre-flight check — CONFIRMED: skip step 2, this cluster is shared/managed

Already run and confirmed on `student-2` (2026-08-14):

```
$ kubectl get crd | grep monitoring.coreos.com
Error from server (Forbidden): ... cannot list resource "customresourcedefinitions" ... at the cluster scope

$ kubectl get crd servicemonitors.monitoring.coreos.com -o jsonpath='...'
Error from server (Forbidden): ... cannot get resource "customresourcedefinitions" ... at the cluster scope

$ helm list -n student-2
NAME             NAMESPACE    REVISION  STATUS    CHART                  APP VERSION
fortune-cookie   student-2    4         deployed  fortune-cookie-0.1.0   1.0.0

$ kubectl get prometheus,alertmanager -A
Error from server (Forbidden): ... cannot list resource "prometheuses" ... at the cluster scope
Error from server (Forbidden): ... cannot list resource "alertmanagers" ... at the cluster scope
```

**Conclusion**: `workstation-2-account` has zero cluster-scoped read access (can't list/get CRDs,
can't list `Prometheus`/`Alertmanager` objects anywhere) — but backend's `ServiceMonitor`
(`../chart/templates/servicemonitor.yaml`) has been deploying successfully to `student-2` this
whole project. Those are two different RBAC grants: the CRD *definition*
(`customresourcedefinitions.apiextensions.k8s.io`, always cluster-scoped, forbidden to us) versus
*instances* of the custom type it defines (`servicemonitors.monitoring.coreos.com`, scoped
`Namespaced` by that CRD, and something we do have rights to create within `student-2`). That's
not a coincidence — it's a deliberate platform design: **course staff already installed the CRDs
and Prometheus Operator cluster-wide**, and scoped student accounts down to "you may create
`ServiceMonitor`/`PodMonitor` objects in your own namespace, nothing more." `helm list -n
student-2` showing only our own `fortune-cookie` release fits too — a shared install wouldn't live
in a student namespace at all.

**Action**: do not run step 2 below — it will fail identically (`Forbidden ... at the cluster
scope`) and there's nothing to fix on our end. Our `ServiceMonitor`s are very likely *already*
being scraped by that shared Prometheus right now (the CRD being functional at all implies
something is actively reconciling it). What's missing is visibility, not infrastructure — **ask
course staff / TAs for the shared Grafana/Prometheus URL or access details** for the course
cluster. Once you have that, skip straight to step 3/4 below using whatever Service name and
namespace they give you instead of `kube-prometheus-stack-grafana`/`student-2`.

The values file and install command below are kept for reference (e.g. if you ever work with a
cluster where you *do* have cluster-admin), not because we expect to run them here.

## 2. Install (not applicable on `student-2` — see confirmed finding above)

```bash
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update

helm install kube-prometheus-stack prometheus-community/kube-prometheus-stack \
  -n student-2 \
  -f kube-prometheus-stack-values.yaml \
  --skip-crds \
  --version 88.3.0
```

`--skip-crds` always, regardless of what step 1 found — the CRD already works on this cluster, so
there's no upside to this release also trying to own/manage it (and per the namespace-scoped RBAC
this account has, it likely can't anyway).

**If this fails** with something like:
```
... is forbidden: User "..." cannot create resource "customresourcedefinitions"/"clusterroles"/"clusterrolebindings" ... at the cluster scope
```
that's a clean, expected signal — not a bug to work around. It means this account's permissions are
namespace-scoped only (consistent with everything else observed about `student-2` this project), and
a full kube-prometheus-stack install isn't something an individual group can self-serve on this
cluster. The fix at that point isn't a workaround — it's asking course staff to provision monitoring
once, cluster-wide, with individual groups only ever adding namespaced `ServiceMonitor`/dashboard
`ConfigMap` objects against it (exactly what `../chart/templates/servicemonitor.yaml` and
`../chart/templates/grafana-dashboard-configmap.yaml` already do).

## 3. Access Grafana

On `student-2`: get the actual Service name/namespace from course staff (see confirmed finding
above — we can't discover this ourselves without cluster-scoped read access), then port-forward to
whatever they tell you instead of the placeholder below.

If you ever do run your own install (step 2, on a cluster where you have cluster-admin): deliberately
**not** exposed via NodePort (see `grafana.service.type: ClusterIP` in the values file) — unlike the
app's `frontend`, Grafana is an internal tool with its own login, and NodePort would make it
reachable by anyone who can address any node on a shared cluster. Use port-forward, which requires
already holding namespace-scoped credentials:

```bash
kubectl port-forward svc/kube-prometheus-stack-grafana 3000:80 -n student-2
```

Then open `http://localhost:3000`.

**Admin password**: the values file leaves `grafana.admin.existingSecret` empty rather than
committing a password. Before installing, create the secret yourself:
```bash
kubectl create secret generic grafana-admin-credentials \
  -n student-2 \
  --from-literal=admin-user=admin \
  --from-literal=admin-password="$(openssl rand -base64 20)"
```
then set `grafana.admin.existingSecret: grafana-admin-credentials` in
`kube-prometheus-stack-values.yaml` before running the install command in step 2. Retrieve the
generated password later with:
```bash
kubectl get secret grafana-admin-credentials -n student-2 -o jsonpath='{.data.admin-password}' | base64 -d
```

## 4. Verify

On `student-2`, don't bother with `kubectl get pods -n student-2 | grep prometheus` — the shared
Prometheus/Grafana almost certainly run in a different namespace we can't see, per the confirmed
finding above. Once course staff give you access (its own namespace, its own Service names), via
that port-forward:

- Prometheus UI → Status → Targets: `backend` and `frontend` should show as `UP`.
- Grafana → Dashboards: a "Fortune Cookie" dashboard should already be present (auto-discovered via
  the `grafana_dashboard: "1"`-labelled ConfigMap the app chart ships), showing live request rate,
  error rate, p50/p95 latency, and current fortune count.

If you ever run your own install (step 2, elsewhere), the same checks apply against
`kube-prometheus-stack-prometheus`/`kube-prometheus-stack-grafana` in whatever namespace you
installed into.
