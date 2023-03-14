# kluster-capacity  
[[中文](./README-ZH.md)]

![kluster-capacity-logo](docs/images/capacity-management-capacity-icon.jpeg)  

[![Build Status](https://github.com/k-cloud-labs/kluster-capacity/actions/workflows/ci.yml/badge.svg)](https://github.com/k-cloud-labs/kluster-capacity/actions?query=workflow%3Abuild)
[![Go Report Card](https://goreportcard.com/badge/github.com/k-cloud-labs/kluster-capacity)](https://goreportcard.com/report/github.com/k-cloud-labs/kluster-capacity)
[![Go doc](https://img.shields.io/badge/go.dev-reference-brightgreen?logo=go&logoColor=white&style=flat)](https://pkg.go.dev/github.com/k-cloud-labs/kluster-capacity)


Cluster capacity tool supports capacity estimation, scheduler simulation, and cluster compression.
This repository was inspired by https://github.com/kubernetes-sigs/cluster-capacity.

## Build
Build the framework:

```sh
$ cd $GOPATH/src/github.com/k-cloud-labs/
$ git clone https://github.com/k-cloud-labs/kluster-capacity
$ cd kluster-capacity
$ make build
```

There are three available sub-commands: ce, cc, and ss, which represent capacity estimation, cluster compression, and scheduler simulation, respectively.

## Capacity Estimation
### Intro
When new pods get scheduled on nodes in a cluster, more resources get consumed. Monitoring available resources in the cluster is very important as operators can increase the current resources in time before all of them get exhausted. Or, carry different steps that lead to increase of available resources.

Cluster capacity consists of capacities of individual cluster nodes. Capacity covers CPU, memory, disk space and other resources.

Overall remaining allocatable capacity is an estimation. The goal is to analyze the remaining allocatable resources and estimate the available capacity that can still be consumed in terms of the number of pod instances with given requirements that can be scheduled in a cluster.  

### Enhancement
Here are some enhancements to the cluster capacity mentioned above.
- Support using an existing pod as a pod template directly from the cluster.
- Support batch simulation for different pod templates.

### Run
run the analysis:

```sh
# use an specified pod yaml file as pod template
$ ./kluster-capacity ce --pods-from-template <path to pod templates> 
# use an existing pod from cluster as pod template
$ ./kluster-capacity ce --pods-from-cluster <namespace/name key of the pod> 
```
For more information about available options run:

```sh
$ ./kluster-capacity ce --help
```

### Demonstration

Assuming a cluster is running with 4 nodes and 1 master with each node with 2 CPUs and 4GB of memory.
With pod resource requirements to be `150m` of CPU and `100Mi` of Memory.

```sh
$ ./kluster-capacity ce --pods-from-template <path to pod templates> --verbose
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
$ ./kluster-capacity ce --pods-from-template <path to pod templates> --verbose
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
$ ./kluster-capacity ce --pods-from-template <path to pod templates> -o json|yaml
```

The json or yaml output is not versioned and is not guaranteed to be stable across various releases.

## Scheduler Simulation
### Intro
The scheduler simulation takes all nodes, pods, and other related resources in the current cluster as input to simulate the process from having no pods to creating and scheduling all pods. This can be used to calculate the cluster compression ratio to evaluate the effectiveness of the scheduling or to measure the quality of the scheduling algorithm.

Compared to cluster compression, its results are more extreme and idealized.

### Run
run the analysis:

```shell
 ./kluster-capacity ss
```
For more information about available options run:

```sh
$ ./kluster-capacity ss --help
```
It supports two termination conditions: `AllSucceed` and `AllScheduled`. The former means the program ends when all pods are successfully scheduled, while the latter means it exits after all pods have been scheduled at least once. The default is `AllSucceed`. The exit condition can be set using the `--exit-condition` flag.

### Demonstration

Assuming a cluster is running with 4 nodes and 1 master with each node with 2 CPUs and 4GB of memory.
With 40 pod with resource requirements to be `100m` of CPU and `200Mi` of Memory to schedule.

If the scheduler uses the `LeastAllocated` strategy, the scheduling result may be as follows:

```sh
$ ./kluster-capacity ss --verbose
Termination reason: AllSucceed: 40 pod(s) have been scheduled successfully.

Pod distribution among nodes:
        - kube-node-1: 10 instance(s)
        - kube-node-2: 10 instance(s)
        - kube-node-3: 10 instance(s)
        - kube-node-4: 10 instance(s)
```

Once the scheduler uses the `MostAllocated` strategy, the scheduling result may be as follows:

```sh
$ ./kluster-capacity ss --verbose
Termination reason: AllSucceed: 40 pod(s) have been scheduled successfully.

Pod distribution among nodes:
        - kube-node-1: 20 instance(s)
        - kube-node-2: 20 instance(s)
```

The scheduling result above can be analyzed to evaluate the effectiveness of the scheduling strategy and the cluster capacity compression ratio. For example, the above result represents a cluster compression ratio of 2, which means that there is 50% resource waste in an ideal situation.


## Cluster Compression
### Intro
Cluster compression takes the current state of the cluster, including all nodes, pods, and other relevant resources, as input, and simulates the process of compressing the cluster by removing nodes. It can be used to calculate the compression ratio of the cluster, which is a measure of how efficiently the resources are being utilized.   

Compared to simulation scheduling, the results of cluster compression are generally more realistic.

### Run
run the analysis:

```shell
 ./kluster-capacity cc --verbose
```
For more information about available options run:

```sh
$ ./kluster-capacity cc --help
```

### Demonstration

Assuming a cluster is running with 4 nodes and 1 master with each node with 2 CPUs and 4GB of memory.
With 40 pod with resource requirements to be `100m` of CPU and `200Mi` of Memory bind to the 4 nodes.

```shell
./kluster-capacity cc --verbose
2 node(s) in the cluster can be scaled down.

Termination reason: FailedSelectNode: could not find a node that satisfies the condition, 1 master node(s); 2 node(s) can't be scale down because of insufficient resource in other nodes;

nodes selected to be scaled down:
        - kube-node-1
        - kube-node-3
```

The above result indicates that with the given resource requirements for 40 pods, ensuring that all pods can be scheduled, the cluster can remove 2 additional nodes, resulting in a compression ratio of 2, which means there is 50% resource waste.

## Feature
- [x] cluster compression
- [x] capacity estimation
- [x] scheduler simulation
- [ ] snapshot based simulation 
- [ ] fragmentation rate analysis

Enjoy it and feel free to give your opinion, thanks!