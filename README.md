# Kube Node Cycle Operator

Highly inspired by ([container-linux-update-operator]https://github.com/coreos/container-linux-update-operator/) and borrows `k8sutil` from there.

Includes 2 applications (`agent` and `operator`) to automate the graceful termination of kubernetes cluster nodes in case their configuration is updated.

## Agent

**Currently only GCP Support**

Designed to run on every node as a `DaemonSet` and compares the current template with the template that the group manager that this node belongs to is using.

If it finds a difference it updates the node's annotations to ask for termination/update.

Terminates the node when it grants permission from operator

Needs `GOOGLE_APPLICATION_CREDENTIALS` to point to the `gcp` key file of a service account with `compute.instanceAdmin.v1` role permissions.

```
Usage of agent:
  -alsologtostderr
        log to standard error as well as files
  -conf_file string
        (Optional) Path of the kube config file to use. Defaults to incluster config for pods
  -log_backtrace_at value
        when logging hits line file:N, emit a stack trace
  -log_dir string
        If non-empty, write log files in this directory
  -logtostderr
        log to standard error instead of files
  -project string
        (Required) GCP Project to use
  -region string
        (Required) Region where the node lives
  -stderrthreshold value
        logs at or above this threshold go to stderr
  -v value
        log level for V logs
  -vmodule value
        comma-separated list of pattern=N settings for file-filtered logging
```

Example ([manifest]https://github.com/utilitywarehouse/kube-node-cycle-operator/deploy/agent.yaml)
 
## Operator

Stateful application that polls nodes annotations to see whether there are nodes that need updating and handles permission to do so if needed.

Only one node is allowed to terminate/update at a time.

Keeps config about the number of nodes that gets updated every time that no update is needed and all cluster nodes report `Ready`

Usage:

```
Usage of operator:
  -alsologtostderr
        log to standard error as well as files
  -conf_file string
        (Optional) Path of the kube config file to use. Defaults to incluster config for pods
  -log_backtrace_at value
        when logging hits line file:N, emit a stack trace
  -log_dir string
        If non-empty, write log files in this directory
  -logtostderr
        log to standard error instead of files
  -state_path string
        (Required) Path of the file where operator shall keep the state info. Shall be part of a persistent volume
  -stderrthreshold value
        logs at or above this threshold go to stderr
  -v value
        log level for V logs
  -vmodule value
        comma-separated list of pattern=N settings for file-filtered logging
```

Example ([manifest]https://github.com/utilitywarehouse/kube-node-cycle-operator/deploy/operator.yaml)

Even though this works to successfully rotate nodes on a manual cluster on `gcp` it is still work in progress and might require heavy changes.
