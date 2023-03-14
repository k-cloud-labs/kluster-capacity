# kluster-capacity
[[English](./README.md)]

![kluster-capacity-logo](docs/images/capacity-management-capacity-icon.jpeg)

[![Build Status](https://github.com/k-cloud-labs/kluster-capacity/actions/workflows/ci.yml/badge.svg)](https://github.com/k-cloud-labs/kluster-capacity/actions?query=workflow%3Abuild)
[![Go Report Card](https://goreportcard.com/badge/github.com/k-cloud-labs/kluster-capacity)](https://goreportcard.com/report/github.com/k-cloud-labs/kluster-capacity)
[![Go doc](https://img.shields.io/badge/go.dev-reference-brightgreen?logo=go&logoColor=white&style=flat)](https://pkg.go.dev/github.com/k-cloud-labs/kluster-capacity)

集群容量分析工具支持剩余容量估算、调度器模拟和集群压缩等功能。  
这个仓库的灵感来自于 https://github.com/kubernetes-sigs/cluster-capacity。

## 安装
### Homebrew
通过 [Homebrew](https://brew.sh/) 安装:
```
brew tap k-cloud-labs/tap
brew install k-cloud-labs/tap/kluster-capacity
```

### Krew
通过 [Krew](https://github.com/GoogleContainerTools/krew) 安装:
```
kubectl krew install kluster-capacity
```

### 从源码编译
编译整个程序:

```sh
$ cd $GOPATH/src/github.com/k-cloud-labs/
$ git clone https://github.com/k-cloud-labs/kluster-capacity
$ cd kluster-capacity
$ make build
```

有三个可用的子命令：ce、cc和ss，分别表示剩余容量估算、集群压缩和调度模拟。

## 容量评估
### 介绍
随着集群中节点上新的 Pod 被调度，消耗的资源越来越多。监控集群中可用的资源非常重要，因为运维人员可以及时增加当前的资源，以免所有资源都耗尽。或者，采取不同的步骤来增加可用资源。

集群容量包括单个集群节点的容量。容量涵盖了 CPU、内存、磁盘空间和其他资源。

整体剩余可分配容量是一个估计值。目标是分析剩余可分配的资源并估计可用容量，即可以在集群中安排给定资源需求的 Pod 实例数量。

### 增强
以下是对原集群容量的一些增强功能：

- 支持直接从集群中使用现有的 Pod 作为 Pod 模板。
- 支持针对不同的 Pod 模板进行批量模拟。

### 运行

```sh
# 直接使用指定的 pod 模板
$ ./kluster-capacity ce --pods-from-template <path to pod templates> 
# 使用集群中指定的 pod 作为模板
$ ./kluster-capacity ce --pods-from-cluster <namespace/name key of the pod> 
```
更多运行参数及功能，请执行如下命令：

```sh
$ ./kluster-capacity ce --help
```

### 演示
假设集群运行有 4 个节点和 1 个主节点，每个节点有 2 个 CPU 和 4GB 内存。而每个 Pod 所需的资源为 150m CPU 和 100Mi 内存。

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

随着集群中运行的 pod 数量增加，再次运行分析时，可调度的 pod 数量也会减少。

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

### 输出格式
`ce` 命令有一个 `--output (-o)` 标志，可以将其输出格式化为 json 或 yaml。

```sh
$ ./kluster-capacity ce --pods-from-template <path to pod templates> -o json|yaml
```

## 调度模拟
### 介绍

调度器模拟以当前集群中的所有 node、pod 等相关资源为输入，模拟从没有 pod 到创建并调度所有 pod 的过程。这可以用来计算集群压缩率比，以评估调度效果或衡量调度算法的质量。

与集群压缩相比，其结果更加激进和理想化。

### 运行

```shell
 ./kluster-capacity ss 
```
更多运行参数及功能，请执行如下命令：

```sh
$ ./kluster-capacity ss --help
```
它支持两种终止条件：`AllSucceed` 和 `AllScheduled`。前者是指所有pod调度成功后程序结束，后者是指所有 pod 至少被调度一次后程序退出。默认值为 `AllSucceed`。可以使用 `--exit-condition` 标志设置退出条件。

### 演示

假设集群运行有 4 个节点和 1 个主节点，每个节点有 2 个 CPU 和 4GB 内存。有 40 个资源需求是 100m CPU 和 200Mi 内存的 Pod 需要被调度。

如果调度器使用 `LeastAllocated` 策略，调度结果可能如下所示：

```sh
$ ./kluster-capacity ss --verbose
Termination reason: AllSucceed: 40 pod(s) have been scheduled successfully.

Pod distribution among nodes:
        - kube-node-1: 10 instance(s)
        - kube-node-2: 10 instance(s)
        - kube-node-3: 10 instance(s)
        - kube-node-4: 10 instance(s)
```

如果调整调度器使用 `MostAllocated` 策略，调度结果可能如下所示：

```sh
$ ./kluster-capacity ss --verbose
Termination reason: AllSucceed: 40 pod(s) have been scheduled successfully.

Pod distribution among nodes:
        - kube-node-1: 20 instance(s)
        - kube-node-2: 20 instance(s)
```

可以分析上面的调度结果来评估调度策略的有效性和集群容量压缩比。例如，上面的结果表示集群压缩比为2，这意味着在理想情况下有50%的资源浪费。


## 集群压缩
### 介绍
集群压缩以集群的当前状态，包括所有 node、pod 和其他相关资源作为输入，模拟通过移除节点来压缩集群的过程。它可用于计算集群的压缩比，这是衡量资源利用效率的指标。

与模拟调度相比，集群压缩的结果通常更显示，可操作性更强。

### 运行

```shell
 ./kluster-capacity cc --verbose
```
更多运行参数及功能，请执行如下命令：

```sh
$ ./kluster-capacity cc --help
```

### 演示

假设集群运行有 4 个节点和 1 个主节点，每个节点有 2 个 CPU 和 4GB 内存。运行有 40 个资源需求是 100m CPU 和 200Mi 内存的 Pod。

```shell
./kluster-capacity cc --verbose
2 node(s) in the cluster can be scaled down.

Termination reason: FailedSelectNode: could not find a node that satisfies the condition, 1 master node(s); 2 node(s) can't be scale down because of insufficient resource in other nodes;

nodes selected to be scaled down:
        - kube-node-1
        - kube-node-3
```

上面的结果表明，给定 40 个 pod 的资源需求，在保证所有 pod 都能被调度的情况下，集群可以去掉 2 个节点，压缩比为 2，也就是有 50% 的资源浪费。

## Feature
- [x] 集群压缩
- [x] 容量评估
- [x] 调度模拟
- [ ] 基于 snapshot 的模拟
- [ ] 资源碎片分析

欢迎体验并提出您的宝贵意见，谢谢！