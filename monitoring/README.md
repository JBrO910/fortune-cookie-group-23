# Monitoring: Prometheus & Grafana

This document describes how metrics collection and dashboards work for the Fortune Cookie app,
why we can't self-host that monitoring stack on the course workstation cluster (`student-2`), and
how to stand the whole thing up yourself on a local minikube cluster instead.

## How monitoring is wired into the app

Two separate Helm releases are involved:

- **`../chart`** — the app itself (`frontend`, `backend`, `redis`). It ships the monitoring
  *instrumentation*, not the monitoring stack:
  - `frontend` and `backend` each expose a `/metrics` endpoint and are annotated with
    `prometheus.io/scrape`, `prometheus.io/port`, `prometheus.io/path`
    (`../chart/templates/deployment.yaml`, driven by `../chart/values.yaml`).
  - A `ServiceMonitor` per service (`../chart/templates/servicemonitor.yaml`) tells a running
    Prometheus Operator to scrape that service's `/metrics` endpoint on a 15s interval.
  - A `ConfigMap` carrying a pre-built Grafana dashboard
    (`../chart/templates/grafana-dashboard-configmap.yaml`,
    `../chart/dashboards/fortune-cookie.json`), labelled `grafana_dashboard: "1"` so Grafana's
    dashboard sidecar auto-discovers and loads it — no manual import needed. It shows request
    rate, error rate, p50/p95 latency, and current fortune count.
- **`kube-prometheus-stack`** (this directory) — the monitoring *infrastructure*: the Prometheus
  Operator, Prometheus itself, and Grafana, installed from the `prometheus-community` Helm
  chart with `kube-prometheus-stack-values.yaml`.

These are deliberately separate releases. The app chart is namespace-scoped and safe to deploy
anywhere via CI. `kube-prometheus-stack` has cluster-scoped dependencies (CRDs, `ClusterRole`s)
that need a human watching the install, so it's never wired into the automated `deploy` job.

For the `ServiceMonitor`s to actually get scraped, a Prometheus Operator reconciling that CRD has
to be running somewhere with visibility into the app's namespace. That's the crux of the next
section.

## Why we can't self-host this on `student-2`

Our `ServiceMonitor` objects deploy successfully to `student-2` via the app chart, which tells us
the `ServiceMonitor` CRD already exists on that cluster and something is presumably reconciling
it. But `student-2`'s service account
(`system:serviceaccount:code-server-workstations:workstation-2-account`) has **no cluster-scoped
read access at all**:

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

That's two different RBAC grants: the CRD *definition*
(`customresourcedefinitions.apiextensions.k8s.io`, always cluster-scoped, forbidden to us) versus
*instances* of the type it defines (`servicemonitors.monitoring.coreos.com`, namespace-scoped,
something we can create in `student-2`). This points to a deliberate platform design: **course
staff already installed the CRDs and a Prometheus Operator cluster-wide**, and scoped student
accounts down to "create `ServiceMonitor`/`PodMonitor` objects in your own namespace, nothing
more." A self-service `kube-prometheus-stack` install would additionally need to create
`ClusterRole`/`ClusterRoleBinding` objects, which this account also can't do.

**Net effect**: we can tell Prometheus what to scrape (`ServiceMonitor`), but we have no way to
confirm anything is actually scraping it, and no way to install or reach a Grafana to look at the
result — from `student-2` alone.

**What's needed from course staff**: either (a) the shared Prometheus/Grafana's Service
name/namespace or URL, so we can point a `kubectl port-forward` at it, or (b) if nothing shared
exists, cluster-scoped permissions to install `kube-prometheus-stack` ourselves.

## Deploying the full stack on minikube

Since we can't verify the setup on `student-2`, minikube is the way to check that the app-side
monitoring config (`ServiceMonitor`, dashboard `ConfigMap`, pod annotations) actually works
end-to-end, using the exact same chart and values file. On minikube your local kubeconfig user
has cluster-admin, so the install isn't blocked by the RBAC issue above.

`kube-prometheus-stack-values.yaml` scopes the Prometheus Operator to only watch its own release
namespace (`prometheusOperator.namespaces.releaseNamespace: true`), so **install it into the same
namespace as the app chart** — otherwise the Operator won't see the app's `ServiceMonitor`s.

```bash
minikube start

helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update

# Same namespace the app chart is (or will be) deployed into.
NS=fortune-cookie
kubectl create namespace "$NS" --dry-run=client -o yaml | kubectl apply -f -

# Grafana needs an admin credentials Secret to exist before install.
kubectl create secret generic grafana-admin-credentials \
  -n "$NS" \
  --from-literal=admin-user=admin \
  --from-literal=admin-password="$(openssl rand -base64 20)"

helm install kube-prometheus-stack prometheus-community/kube-prometheus-stack \
  -n "$NS" \
  -f kube-prometheus-stack-values.yaml \
  --set grafana.admin.existingSecret=grafana-admin-credentials \
  --version 88.3.0 \
  --wait --timeout 5m

# Deploy the app into the same namespace so its ServiceMonitors are visible to the Operator.
helm install fortune-cookie ../chart -n "$NS" --wait --timeout 3m
```

Unlike a `student-2` install, do **not** pass `--skip-crds` here — a fresh minikube cluster has no
pre-existing `monitoring.coreos.com` CRDs, so this install needs to create them itself.

## Accessing Grafana

Grafana is deliberately not exposed via `NodePort` (`grafana.service.type: ClusterIP` in the
values file) — unlike the app's `frontend`, it's an internal tool with its own login, and
`NodePort` would make it reachable by anyone who can address any node on a shared cluster.
Port-forward instead:

```bash
kubectl port-forward svc/kube-prometheus-stack-grafana 3000:80 -n "$NS"
```

Open `http://localhost:3000`. Username: admin. Retrieve the generated admin password with:

```bash
kubectl get secret grafana-admin-credentials -n "$NS" -o jsonpath='{.data.admin-password}' | base64 -d
```

## Verifying it works

```bash
# Prometheus itself, and the Operator's CRD instance:
kubectl get prometheus,alertmanager -n "$NS"

# The app's ServiceMonitors:
kubectl get servicemonitor -n "$NS" backend frontend
```

Then, via a Prometheus port-forward (`kubectl port-forward svc/kube-prometheus-stack-prometheus
9090:9090 -n "$NS"`):

- **Status → Targets** (or `/api/v1/targets`): `backend` and `frontend` should show as `UP`.
- Query `fortune_http_requests_total` — should return live values that increase as you hit the
  app's `frontend`.

In Grafana:

- **Dashboards** → a **"Fortune Cookie"** dashboard should already be present (auto-discovered via
  the `grafana_dashboard: "1"`-labelled `ConfigMap`), showing live request rate, error rate,
  p50/p95 latency, and current fortune count.
- Other stock dashboards kube-prometheus-stack ships (e.g. under `Kubernetes / Compute
  Resources / Multi-Cluster`) may show "No data" — those filter by a `cluster` label that isn't
  set here (`prometheus.prometheusSpec.externalLabels`), which is unrelated to the app's own
  dashboard and not something this project needs.

## Tearing down

```bash
helm uninstall kube-prometheus-stack -n "$NS"
helm uninstall fortune-cookie -n "$NS"
kubectl delete namespace "$NS"   # only if you don't need it for anything else
```
