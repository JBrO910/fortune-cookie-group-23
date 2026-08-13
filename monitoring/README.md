# Monitoring: kube-prometheus-stack

This installs Prometheus + Grafana + the Prometheus Operator into `student-2`, scoped as tightly
as possible to that one namespace. It's a **separate Helm release** from the `fortune-cookie` app
chart (`../chart/`) — deliberately not wired into CI's `deploy` job. It has cluster-scoped
dependencies (CRDs, ClusterRoles) that need a human watching the output, not an unattended install
on every push.

## 1. Pre-flight check — do this first, every time

Backend's `ServiceMonitor` (`../chart/templates/servicemonitor.yaml`) already deploys successfully
to `student-2` today, which proves the `ServiceMonitor` CRD already exists there — installed by
someone. Before running anything below, find out who/what:

```bash
kubectl get crd | grep monitoring.coreos.com
kubectl get crd servicemonitors.monitoring.coreos.com -o jsonpath='{.metadata.annotations}{"\n"}'
helm list -n student-2
kubectl get prometheus,alertmanager -A
```

Read the second command's output:
- If it shows `meta.helm.sh/release-name` / `app.kubernetes.io/managed-by: Helm` for a release
  that's live and working (check `helm list` for it) — **stop, do not install anything below.**
  Something already owns this cluster-wide. Just point `../chart/values.yaml`'s
  `serviceMonitor.labels.release` at whatever that release is actually named (if not already
  `kube-prometheus-stack`), and get Grafana access from whoever runs it.
- If it shows Helm ownership metadata for a release that's *not* in `helm list -n student-2` (i.e.
  a different namespace, or gone) — that's a partial/abandoned install. Don't force-adopt or delete
  cluster-scoped objects on a shared cluster unilaterally; coordinate with whoever has access first.
- If the CRD has no Helm ownership annotations at all, or the annotation inspection genuinely comes
  back empty — proceed to step 2.

## 2. Install

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

Deliberately **not** exposed via NodePort (see `grafana.service.type: ClusterIP` in the values
file) — unlike the app's `frontend`, Grafana is an internal tool with its own login, and NodePort
would make it reachable by anyone who can address any node on this shared cluster. Use
port-forward, which requires already holding `student-2`-scoped credentials:

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

```bash
kubectl get pods -n student-2 | grep -E "prometheus|grafana"
```
Both should be `Running`. Then, via the port-forward from step 3, or a similar one to Prometheus
itself (`kubectl port-forward svc/kube-prometheus-stack-prometheus 9090:9090 -n student-2`):

- Prometheus UI → Status → Targets: `backend` and `frontend` should show as `UP`.
- Grafana → Dashboards: a "Fortune Cookie" dashboard should already be present (auto-discovered via
  the `grafana_dashboard: "1"`-labelled ConfigMap the app chart ships), showing live request rate,
  error rate, p50/p95 latency, and current fortune count.
