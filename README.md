# kluster-capacity  

![kluster-capacity-logo](docs/images/capacity-management-capacity-icon.jpeg)  

[![Build Status](https://github.com/k-cloud-labs/kluster-capacity/actions/workflows/ci.yml/badge.svg)](https://github.com/k-cloud-labs/kluster-capacity/actions?query=workflow%3Abuild)
[![Go Report Card](https://goreportcard.com/badge/github.com/k-cloud-labs/kluster-capacity)](https://goreportcard.com/report/github.com/k-cloud-labs/kluster-capacity)
[![Go doc](https://img.shields.io/badge/go.dev-reference-brightgreen?logo=go&logoColor=white&style=flat)](https://pkg.go.dev/github.com/k-cloud-labs/kluster-capacity)


Cluster capacity tool support capacity estimation、scheduler simulation、cluster compression.  
This repository is inspired by https://github.com/kubernetes-sigs/cluster-capacity.  
And the code of this repository is based on https://github.com/kubernetes-sigs/cluster-capacity.


## Capacity Estimation
### Intro
As new pods get scheduled on nodes in a cluster, more resources get consumed. Monitoring available resources in the cluster is very important as operators can increase the current resources in time before all of them get exhausted. Or, carry different steps that lead to increase of available resources.

Cluster capacity consists of capacities of individual cluster nodes. Capacity covers CPU, memory, disk space and other resources.

Overall remaining allocatable capacity is a rough estimation since it does not assume all resources being distributed among nodes. Goal is to analyze remaining allocatable resources and estimate available capacity that is still consumable in terms of a number of instances of a pod with given requirements that can be scheduled in a cluster.

### Enhancement
Some enhancement than cluster capacity mentioned above.
- Support use existing pod as pod template directly from cluster.
- Support batch simulation for different pod template.

### Build and Run

Build the framework:

```sh
$ cd $GOPATH/src/github.com/k-cloud-labs/
$ git clone https://github.com/k-cloud-labs/kluster-capacity
$ cd kluster-capacity
$ make build
```
and run the analysis:

```sh
# use an specified pod yaml file as pod template
$ ./kluster-capacity ce --kubeconfig <path to kubeconfig> --pod-templates <path to pod templates> 
# use an existing pod from cluster as pod template
$ ./kluster-capacity ce --kubeconfig <path to kubeconfig> --pods-from-cluster <namespace/name key of the pod> 
```
For more information about available options run:

```sh
$ ./kluster-capacity ce --help
```

### Demonstration

Assuming a cluster is running with 4 nodes and 1 master with each node with 2 CPUs and 4GB of memory.
With pod resource requirements to be `150m` of CPU and ``100Mi`` of Memory.

```sh
$ ./kluster-capacity ce --kubeconfig <path to kubeconfig> --pod-templates <path to pod templates> --verbose
Pod requirements:
	- cpu: 150m
	- memory: 100Mi

The cluster can schedule 52 instance(s) of the pod.
Termination reason: FailedScheduling: pod (small-pod-52) failed to fit in any node
fit failure on node (kube-node-1): Insufficient cpu
fit failure on node (kube-node-4): Insufficient cpu
fit failure on node (kube-node-2): Insufficient cpu
fit failure on node (kube-node-3): Insufficient cpu


Pod distribution among nodes:
	- kube-node-1: 13 instance(s)
	- kube-node-4: 13 instance(s)
	- kube-node-2: 13 instance(s)
	- kube-node-3: 13 instance(s)
```

Once the number of running pods in the cluster grows and the analysis is run again,
the number of schedulable pods decreases as well:

```sh
$ ./kluster-capacity ce --kubeconfig <path to kubeconfig> --pod-templates <path to pod templates> --verbose
Pod requirements:
	- cpu: 150m
	- memory: 100Mi

The cluster can schedule 46 instance(s) of the pod.
Termination reason: FailedScheduling: pod (small-pod-46) failed to fit in any node
fit failure on node (kube-node-1): Insufficient cpu
fit failure on node (kube-node-4): Insufficient cpu
fit failure on node (kube-node-2): Insufficient cpu
fit failure on node (kube-node-3): Insufficient cpu


Pod distribution among nodes:
	- kube-node-1: 11 instance(s)
	- kube-node-4: 12 instance(s)
	- kube-node-2: 11 instance(s)
	- kube-node-3: 12 instance(s)
```

### Output format
`ce` command has a flag `--output (-o)` to format its output as json or yaml.

```sh
$ ./kluster-capacity ce --kubeconfig <path to kubeconfig> --pod-templates <path to pod templates> -o json
$ ./kluster-capacity ce --kubeconfig <path to kubeconfig> --pod-templates <path to pod templates> -o yaml
```

The json or yaml output is not versioned and is not guaranteed to be stable across various releases.

## Scheduler Simulation


## Cluster Compression

## Feature
- [ ] ...