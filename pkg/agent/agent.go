package agent

import (
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"k8s.io/api/core/v1"
	policyv1beta1 "k8s.io/api/policy/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	v1meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"

	"github.com/utilitywarehouse/kube-node-cycle-operator/kube/k8sutil"
	"github.com/utilitywarehouse/kube-node-cycle-operator/models"
	"github.com/utilitywarehouse/kube-node-cycle-operator/pkg/annotations"
)

const defaultPollInterval = 10 * time.Second

type Status struct {
	UpdateNeeded     string
	UpdateInProgress string
	LastCheckedTime  time.Time
}

type NodeAgent struct {
	node string
	kc   kubernetes.Interface
	nc   v1core.NodeInterface
	cc   models.NodeClientInterface
	s    *Status
}

type NodeAgentInterface interface {
	Run()
	updateStatus()
	drainNode() error
	getPodsForTermination() ([]v1.Pod, error)
	deletePod(pod v1.Pod) error
	evictPod(pod v1.Pod) error
	waitForPodTermination(pod v1.Pod, podReapTimeOut time.Duration) error
	syncPodsTermination(pods []v1.Pod, timeout time.Duration)
	terminateNode() error
	drainAndTerminate()
}

func New(node, kubeConfig string, nodeClientInterface models.NodeClientInterface) (*NodeAgent, error) {
	// kube client
	kubeClient, err := k8sutil.GetClient(kubeConfig)
	if err != nil {
		return nil, err
	}

	// node interface
	kubeNodeInterface := kubeClient.CoreV1().Nodes()

	// Initial Status
	st := &Status{
		UpdateNeeded:     annotations.AnnoFalse,
		UpdateInProgress: annotations.AnnoFalse,
		LastCheckedTime:  time.Now(),
	}

	agent := &NodeAgent{
		node: node,
		kc:   kubeClient,
		nc:   kubeNodeInterface,
		cc:   nodeClientInterface,
		s:    st,
	}
	return agent, nil
}

func (na *NodeAgent) Run() {

	for t := time.Tick(30 * time.Second); ; <-t {
		n, err := na.nc.Get(na.node, v1meta.GetOptions{})
		if err != nil {
			log.Println("failed to get self node (%q): %v", na.node, err)
			continue
		}

		if _, ok := n.Annotations[annotations.UpdateNeeded]; !ok {
			// First Run
			log.Println("First Run")
			na.updateStatus()
			continue
		}

		needsUpdate, err := na.cc.NeedsUpdate()
		if err != nil {
			log.Println(err)
			continue
		}

		// Update Needed discovery
		if needsUpdate && na.s.UpdateNeeded == annotations.AnnoFalse {
			log.Println("Update Needed Detected")
			na.s.UpdateNeeded = annotations.AnnoTrue
			na.updateStatus()
			continue
		}

		// Force Termination
		if val, ok := n.Annotations[annotations.ForceTermination]; ok {
			if val == annotations.AnnoTrue {
				log.Println("[INFO] Forcing Termination")
				na.s.UpdateInProgress = annotations.AnnoTrue
				na.updateStatus()
				break
			}
		}
		// In case of update needed
		if needsUpdate && na.s.UpdateNeeded == annotations.AnnoTrue {

			// Poll for permission to start
			if _, ok := n.Annotations[annotations.CanStartTermination]; !ok {
				// Ignore and continue to poll on the next iteration
				continue
			}

			if n.Annotations[annotations.CanStartTermination] == annotations.AnnoTrue {
				// Update status and exit main loop
				na.s.UpdateInProgress = annotations.AnnoTrue
				na.updateStatus()
				break
			}

		}
	}
	if na.s.UpdateNeeded == annotations.AnnoTrue && na.s.UpdateInProgress == annotations.AnnoTrue {
		na.drainAndTerminate()

		//sleep and hope fro the best
		log.Println("[INFO] Falling asleep, bye..")
		for {
			time.Sleep(60 * time.Second)
			log.Println("[INFO] sleeping...")
		}
	} else {
		log.Println("[ERROR] Exited main loop with unexpected status")
		os.Exit(1)
	}
}

// write status to node annotations
func (na *NodeAgent) updateStatus() {
	anno := map[string]string{
		annotations.UpdateNeeded:     na.s.UpdateNeeded,
		annotations.LastCheckedTime:  fmt.Sprintf("%v", na.s.LastCheckedTime),
		annotations.UpdateInProgress: na.s.UpdateInProgress,
	}

	wait.PollUntil(defaultPollInterval, func() (bool, error) {
		if err := k8sutil.SetNodeAnnotations(na.nc, na.node, anno); err != nil {
			return false, nil
		}
		return true, nil
	}, wait.NeverStop)
}

// Get list of pods that run on the node and are not owned by a DaemonSet
func (na *NodeAgent) getPodsForTermination() ([]v1.Pod, error) {

	pods := []v1.Pod{}

	// Get all pods running on the node
	podList, err := na.kc.CoreV1().Pods(v1.NamespaceAll).List(v1meta.ListOptions{
		FieldSelector: fields.SelectorFromSet(fields.Set{"spec.nodeName": na.node}).String(),
	})
	if err != nil {
		return pods, err
	}

	// exclude daemonsets
	for _, pod := range podList.Items {
		exclude := false
		for _, ownerRef := range pod.OwnerReferences {
			// If daemonset reference is present just ignore without testing that the daemonset actually exists
			if ownerRef.Kind == "DaemonSet" {
				log.Println("[Info] excluding %s as part of a daemonset", pod.Name)
				exclude = true
				break
			}
		}
		if !exclude {
			pods = append(pods, pod)
		}
	}
	return pods, nil
}

// drain the node by evicting and then deleting what failed to evict
func (na *NodeAgent) drainNode() error {

	// Mark not Unschedulable
	log.Println("[INFO] Marking node unschedulable")
	if err := k8sutil.Unschedulable(na.nc, na.node, true); err != nil {
		return err
	}

	// First try to evict pods
	pods, err := na.getPodsForTermination()
	if err != nil {
		return err
	}
	for _, pod := range pods {
		log.Println("[INFO] evicting pod: %s", pod.Name)
		if err := na.evictPod(pod); err != nil {
			log.Println("[ERROR] evicting pod: %s %v", pod.Name, err)
			// Just continue and will attempt to delete later
		}
	}
	// Allow 10 minutes for pods eviction
	na.syncPodsTermination(pods, 10*time.Minute)

	// Delete pods that were not drained
	pods, err = na.getPodsForTermination()
	if err != nil {
		return err
	}
	for _, pod := range pods {
		log.Println("[INFO] deleting  pod: %s", pod.Name)
		if err := na.deletePod(pod); err != nil {
			log.Println("[ERROR] deleting pod: %s %v", pod.Name, err)
		}
	}
	// Allow 2 minutes for pods to delete
	na.syncPodsTermination(pods, 2*time.Minute)

	return nil
}

// deletes a pod
func (na *NodeAgent) deletePod(pod v1.Pod) error {
	if err := na.kc.CoreV1().Pods(pod.Namespace).Delete(pod.Name, &v1meta.DeleteOptions{}); err != nil {
		return err
	}
	return nil
}

// evicts pod from the node
func (na *NodeAgent) evictPod(pod v1.Pod) error {

	eviction := &policyv1beta1.Eviction{
		TypeMeta: v1meta.TypeMeta{},
		ObjectMeta: v1meta.ObjectMeta{
			Name:      pod.Name,
			Namespace: pod.Namespace,
		},
		DeleteOptions: &v1meta.DeleteOptions{},
	}

	na.kc.PolicyV1beta1().Evictions(eviction.Namespace).Evict(eviction)
	return nil
}

// waits for a pod to be deleted for podReapTimeOut
func (na *NodeAgent) waitForPodTermination(pod v1.Pod, podReapTimeOut time.Duration) error {

	return wait.PollImmediate(defaultPollInterval, podReapTimeOut, func() (bool, error) {
		p, err := na.kc.CoreV1().Pods(pod.Namespace).Get(pod.Name, v1meta.GetOptions{})
		if errors.IsNotFound(err) || (p != nil && p.ObjectMeta.UID != pod.ObjectMeta.UID) {
			log.Println("[INFO] Terminated pod %q", pod.Name)
			return true, nil
		}

		// most errors will be transient. log the error and continue
		// polling
		if err != nil {
			log.Println("Failed to get pod %q: %v", pod.Name, err)
		}

		return false, nil
	})
}

// Gets a pod list and waits for them a certain amount of time (timeout) to terminate
func (na *NodeAgent) syncPodsTermination(pods []v1.Pod, timeout time.Duration) {

	wg := sync.WaitGroup{}
	for _, pod := range pods {
		wg.Add(1)
		go func(pod v1.Pod) {
			log.Println("[INFO] Waiting for pod %q to terminate", pod.Name)
			if err := na.waitForPodTermination(pod, timeout); err != nil {
				log.Println("[INFO] Skipping wait on pod %q: %v", pod.Name, err)
			}
			wg.Done()
		}(pod)
	}
	wg.Wait()
}

// Call node termination or throw error
func (na *NodeAgent) terminateNode() error {
	if err := na.cc.TerminateNode(); err != nil {
		return err
	}
	return nil
}

// Drain and terminate or loop forever
func (na *NodeAgent) drainAndTerminate() {

	// Drain
	for {
		if err := na.drainNode(); err != nil {
			log.Println("[ERROR] Error while draining node %v, retrying in 10 seconds..", err)
			time.Sleep(10 * time.Second)
		} else {
			log.Println("[INFO] Node drained")
			break
		}
	}

	// Terminate
	for {
		if err := na.terminateNode(); err != nil {
			log.Println("[ERROR] Error while terminating node %v, retrying in 10 seconds..", err)
			time.Sleep(10 * time.Second)
		} else {
			log.Println("[INFO] Issued Node termination")
			break
		}
	}
}
