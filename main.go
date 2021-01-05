/*
Copyright 2017 The Kubernetes Authors.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"flag"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/labels"

	"kmodules.xyz/client-go/meta"

	"gomodules.xyz/pointer"

	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/informers"
	coreLister "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"kmodules.xyz/client-go/tools/queue"

	"github.com/golang/glog"

	core "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/sample-controller/pkg/signals"
)

var (
	masterURL  string
	kubeconfig string
)

func main() {
	flag.Parse()

	// set up signals so we handle the first shutdown signal gracefully
	stopCh := signals.SetupSignalHandler()

	cfg, err := clientcmd.BuildConfigFromFlags(masterURL, kubeconfig)
	if err != nil {
		glog.Fatalf("Error building kubeconfig: %s", err.Error())
	}

	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		glog.Fatalf("Error building kubernetes clientset: %s", err.Error())
	}

	ctrl := NewController(kubeClient)

	if err = ctrl.RunController(stopCh); err != nil {
		glog.Fatalf("Error running controller: %s", err.Error())
	}
}

func init() {
	flag.StringVar(&kubeconfig, "kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	flag.StringVar(&masterURL, "master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
}

type Controller struct {
	kubeClient          kubernetes.Interface
	kubeInformerFactory informers.SharedInformerFactory
	nodeQueue           *queue.Worker
	nodeInformer        cache.SharedIndexInformer
	nodeLister          coreLister.NodeLister
}

func NewController(kubeClient kubernetes.Interface) *Controller {

	ctrl := &Controller{
		kubeClient:          kubeClient,
		kubeInformerFactory: informers.NewSharedInformerFactory(kubeClient, 10*time.Minute),
	}

	ctrl.nodeInformer = ctrl.kubeInformerFactory.Core().V1().Nodes().Informer()
	ctrl.nodeQueue = queue.New("Node", 5, 1, ctrl.processNodeEvent)
	ctrl.nodeInformer.AddEventHandler(queue.DefaultEventHandler(ctrl.nodeQueue.GetQueue()))
	ctrl.nodeLister = ctrl.kubeInformerFactory.Core().V1().Nodes().Lister()
	return ctrl
}

func (c *Controller) RunController(stopCh <-chan struct{}) error {
	defer runtime.HandleCrash()

	fmt.Println("Starting Node controller")

	c.kubeInformerFactory.Start(stopCh)

	// Wait for all involved caches to be synced, before processing items from the queue is started
	for _, v := range c.kubeInformerFactory.WaitForCacheSync(stopCh) {
		if !v {
			runtime.HandleError(fmt.Errorf("timed out waiting for caches to sync"))
			return nil
		}
	}

	c.nodeQueue.Run(stopCh)
	<-stopCh
	fmt.Println("Stopping Node controller")
	return nil
}

func (c *Controller) processNodeEvent(key string) error {
	obj, exists, err := c.nodeInformer.GetIndexer().GetByKey(key)
	if err != nil {
		fmt.Println("Fetching object with key %s from store failed with %v", key, err)
		return err
	}

	if !exists {
		glog.Warningf("Node %s does not exist anymore\n", key)
	} else {
		fmt.Printf("Sync/Add/Update for Node %s\n", key)

		node := obj.(*core.Node).DeepCopy()
		if !nodeReady(node) {
			// delete the pod of this node
			err = c.deletePodFromNode(node)
			if err != nil {
				fmt.Println("err: ", err)
				return err
			}
			// delete PVCs from this node
			err = c.deletePVCFromNode(node)
			if err != nil {
				fmt.Println("err: ", err)
				return err
			}
		}
	}
	return nil
}

func (c *Controller) deletePodFromNode(node *core.Node) error {
	fmt.Println("Deleting pods from node: ", node.Name)
	pods, err := c.kubeClient.CoreV1().Pods(core.NamespaceAll).List(context.TODO(), metav1.ListOptions{
		LabelSelector: labels.SelectorFromSet(map[string]string{
			"app.kubernetes.io/managed-by": "kubedb.com",
		}).String(),
	})
	if err != nil {
		return err
	}
	deleteForground := metav1.DeletePropagationForeground
	for _, p := range pods.Items {
		if p.Spec.NodeName == node.Name {
			fmt.Println("Deleting Pod: ", p.Name)
			err = c.kubeClient.CoreV1().Pods(p.Namespace).Delete(context.TODO(), p.Name, metav1.DeleteOptions{
				GracePeriodSeconds: pointer.Int64P(0),
				PropagationPolicy:  &deleteForground,
			})
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Controller) deletePVCFromNode(node *core.Node) error {
	fmt.Println("Deleting PVCs from node: ", node.Name)
	pvc, err := c.kubeClient.CoreV1().PersistentVolumeClaims(core.NamespaceAll).List(context.TODO(), metav1.ListOptions{
		LabelSelector: labels.SelectorFromSet(map[string]string{
			"app.kubernetes.io/managed-by": "kubedb.com",
		}).String(),
	})
	if err != nil {
		return err
	}
	deleteBackground := metav1.DeletePropagationBackground
	keySelectedNode := "volume.kubernetes.io/selected-node"
	for _, p := range pvc.Items {
		nodeName, err := meta.GetStringValue(p.Annotations, keySelectedNode)
		if err != nil {
			return err
		}
		if nodeName == node.Name {
			fmt.Println("Deleting PVC: ", p.Name)
			err = c.kubeClient.CoreV1().PersistentVolumeClaims(p.Namespace).Delete(context.TODO(), p.Name, metav1.DeleteOptions{
				GracePeriodSeconds: pointer.Int64P(0),
				PropagationPolicy:  &deleteBackground,
			})
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func nodeReady(node *core.Node) bool {
	for _, c := range node.Status.Conditions {
		if c.Type == core.NodeReady && c.Status == core.ConditionTrue {
			return true
		}
	}
	return false
}
