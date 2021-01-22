# node-watcher

Automatically remove PVC and Pods of KubeDB database from a `NotReady` nodes so that it can be scheduled into a different node.

## Available Options

```bash
❯ ./node-watcher --help
Usage of ./node-watcher:
  -alsologtostderr
        log to standard error as well as files
  -kubeconfig string
        Path to a kubeconfig. Only required if out-of-cluster.
  -log_backtrace_at value
        when logging hits line file:N, emit a stack trace
  -log_dir string
        If non-empty, write log files in this directory
  -logtostderr
        log to standard error instead of files
  -master string
        The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.
  -stderrthreshold value
        logs at or above this threshold go to stderr
  -v value
        log level for V logs
  -vmodule value
        comma-separated list of pattern=N settings for file-filtered logging
```

## Build

**Build Binary:**

```bash
go build .
```

**Build Docker Image:**

```bash
docker build -t emruzhossain/node-watcher . \
&& docker push emruzhossain/node-watcher
```

## Usage

**Run Locally:**

```bash
./node-watcher --kubeconfig=/home/emruz/dev/cred/linode/kubeconfig.yaml
```

**Run Inside Kubernetes Cluster:**

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: node-watcher
  namespace: kube-system
spec:
  replicas: 1
  selector:
    matchLabels:
      app: node-watcher
  template:
    metadata:
      labels:
        app: node-watcher
    spec:
      serviceAccountName: node-watcher
      containers:
      - name: node-watcher
        image: emruzhossain/node-watcher:latest
        imagePullPolicy: Always
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: node-watcher
  namespace: kube-system
automountServiceAccountToken: true
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: node-watcher-permissions
rules:
- apiGroups: [""]
  resources: ["nodes"]
  verbs: ["get","list","watch"]
- apiGroups: [""]
  resources: ["pods","persistentvolumeclaims"]
  verbs: ["get", "list","watch","delete"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: node-watcher-permissions
subjects:
- kind: ServiceAccount
  name: node-watcher
  namespace: kube-system
roleRef:
  kind: ClusterRole
  apiGroup: rbac.authorization.k8s.io
  name: node-watcher-permissions
```

```bash
kubectl apply -f ./node-watcher.yaml
```
