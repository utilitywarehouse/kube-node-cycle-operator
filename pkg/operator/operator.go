package operator

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strconv"
	"time"

	"k8s.io/api/core/v1"
	v1meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"

	"github.com/utilitywarehouse/kube-node-cycle-operator/kube/k8sutil"
	"github.com/utilitywarehouse/kube-node-cycle-operator/pkg/annotations"
)

const (
	defaultPollInterval = 10 * time.Second
	configMapName       = "kube-node-cycle-operator-config"
)

type State struct {
	NodeCount       int
	ForceUpdateDate string
}

type Operator struct {
	kc        kubernetes.Interface
	nc        v1core.NodeInterface
	namespace string
	state     State
}

type OperatorInterface interface {
	getNodeCountFromJson() (int, error)
	setNodeCountToJson(count int) error
	getNodes() ([]v1.Node, error)
	getReadyNodes() ([]v1.Node, error)
	updateNeeded(nodes []v1.Node) (bool, []v1.Node)
	updateInProgress(nodes []v1.Node) bool
	updatePermissionGiven(nodes []v1.Node) bool
	giveNodeUpdatePermission(nodeName string)
	Run()
}

func New(kubeConfig string) (*Operator, error) {
	// kube client
	kubeClient, err := k8sutil.GetClient(kubeConfig)
	if err != nil {
		return nil, err
	}

	// node interface
	kubeNodeInterface := kubeClient.CoreV1().Nodes()

	// Get namespace for configMap
	namespace := os.Getenv("POD_NAMESPACE")
	if namespace == "" {
		return nil, fmt.Errorf("unable to get operator namespace: please ensure POD_NAMESPACE environment variable is set")
	}

	state := &State{}

	operator := &Operator{
		kc:        kubeClient,
		nc:        kubeNodeInterface,
		namespace: namespace,
		state:     state,
	}
	operator.configInit()
	return operator, nil
}

func (op *Operator) configInit() {

	// Check for configMap
	log.Println("[INFO] Initializing config")
	configMaps, err := op.kc.CoreV1().ConfigMaps(namespace).List(metav1.ListOptions{})
	if err != nil {
		log.Fatal("Failed to list configMaps", err)
	}
	for _, cm := range configMaps.Items {
		if cm.GetName() == configMapName {
			log.Println("[INFO] configMap already present")
			return
		}
	}

	log.Println("[INFO] ConfigMap not found, creating new..")
	cm := &v1core.ConfigMap{}
	cm.SetName(configMapName)
	cm.Data["NodeCount"] = ""
	cm.Data["ForceUpdateDate"] = ""

	_, err := op.kc.CoreV1().ConfigMaps(namespace).Create(&cm)
	if err != nil {
		return err
	}
}

func (op *Operator) getNodeCountFromConf() (int, error) {
	cm, err := op.kc.CoreV1().ConfigMaps(namespace).Get(configMapName, v1meta.GetOptions{})
	if err != nil {
		return 0, err
	}
	if n, ok := cm.Data["NodeCount"]; !ok {
		return 0, fmt.Errorf("Cannot find NodeCount in config")
	}

	return strconv.Atoi(n)
}

func (op *Operator) updateConf() error {
	cm, err := op.kc.CoreV1().ConfigMaps(namespace).Get(configMapName, v1meta.GetOptions{})
	if err != nil {
		return err
	}

	cm.Data["NodeCount"] = strconv.Itoa(vop.state.NodeCount)
	cm.Data["ForceUpdateDate"] = op.state.ForceUpdateDate

	_, err := op.kc.CoreV1().ConfigMaps(namespace).Update(&cm)
	if err != nil {
		return err
	}
	return nil
}

func (op *Operator) getNodeCountFromJson() (int, error) {
	raw, err := ioutil.ReadFile(op.statePath)
	if err != nil {
		return 0, err
	}

	s := &State{}
	if err := json.Unmarshal(raw, s); err != nil {
		return 0, err
	}
	return s.NodeCount, nil
}

func (op *Operator) setNodeCountToJson(count int) error {
	s := &State{
		NodeCount: count,
	}

	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}

	if err = ioutil.WriteFile(op.statePath, raw, 0644); err != nil {
		return err
	}
	return nil
}

func (op *Operator) setForceUpdateDate(forceUpdateDate time.Time) error {
	return nil
}

func (op *Operator) getNodes() ([]v1.Node, error) {
	nodesList, err := op.nc.List(v1meta.ListOptions{})
	if err != nil {
		return []v1.Node{}, err
	}
	return nodesList.Items, nil
}

func (op *Operator) getReadyNodes() ([]v1.Node, error) {
	nodes, err := op.getNodes()
	if err != nil {
		return []v1.Node{}, err
	}

	readyNodes := []v1.Node{}
	for _, n := range nodes {
		for _, c := range n.Status.Conditions {
			if c.Type == v1.NodeReady {
				if c.Status == v1.ConditionTrue {
					readyNodes = append(readyNodes, n)
				}
				break
			}
		}
	}
	return readyNodes, nil
}

func (op *Operator) updateNeeded(nodes []v1.Node) (updateNeeded bool, updateNodes []v1.Node) {
	for _, n := range nodes {
		if _, ok := n.Annotations[annotations.UpdateNeeded]; !ok {
			log.Println("[INFO] node %s has no annotation %s", n.Name, annotations.UpdateNeeded)
		} else {
			if n.Annotations[annotations.UpdateNeeded] == annotations.AnnoTrue {
				updateNeeded = true
				updateNodes = append(updateNodes, n)
			}
		}
	}
	return updateNeeded, updateNodes
}

func (op *Operator) updateInProgress(nodes []v1.Node) bool {
	for _, n := range nodes {
		if _, ok := n.Annotations[annotations.UpdateInProgress]; !ok {
			log.Println("[INFO] node %s has no annotation %s", n.Name, annotations.UpdateInProgress)
		} else {
			if n.Annotations[annotations.UpdateInProgress] == annotations.AnnoTrue {
				return true
			}
		}
	}
	return false
}

func (op *Operator) updatePermissionGiven(nodes []v1.Node) bool {
	for _, n := range nodes {
		if _, ok := n.Annotations[annotations.CanStartTermination]; !ok {
			log.Println("[INFO] node %s has no annotation %s", n.Name, annotations.CanStartTermination)
		} else {
			if n.Annotations[annotations.CanStartTermination] == annotations.AnnoTrue {
				return true
			}
		}
	}
	return false
}

func (op *Operator) giveNodeUpdatePermission(nodeName string) {
	anno := map[string]string{
		annotations.CanStartTermination: annotations.AnnoTrue,
	}

	wait.PollUntil(defaultPollInterval, func() (bool, error) {
		if err := k8sutil.SetNodeAnnotations(op.nc, nodeName, anno); err != nil {
			return false, nil
		}
		return true, nil
	}, wait.NeverStop)
}

func (op *Operator) Run() {
	for t := time.Tick(30 * time.Second); ; <-t {

		allNodes, err := op.getNodes()
		if err != nil {
			log.Println("[ERROR] error getting nodes %v", err)
			continue
		}

		nodes, err := op.getReadyNodes()
		if err != nil {
			log.Println("[ERROR] error getting nodes %v", err)
			continue
		}

		// Check for Not Ready Nodes
		if len(allNodes) > len(nodes) {
			log.Println("[INFO] Not Ready nodes found, waiting..")
			continue
		}

		// If no update is needed just update the node count with the current number and continue
		updateNeeded, updateNodes := op.updateNeeded(nodes)
		if !updateNeeded {
			log.Println("[INFO] no updated needed, updating node count to: %d", len(nodes))
			op.setNodeCountToJson(len(nodes))
			continue
		}

		// Update needed.
		// If update is in progress or permission already given just wait
		if op.updateInProgress(nodes) || op.updatePermissionGiven(nodes) {
			log.Println("[INFO] updating in progress")
			continue
		}

		nodeCount, err := op.getNodeCountFromJson()
		if err != nil {
			log.Fatal("Failed to get node count, exiting")
		}

		// If we have enough nodes give permission to start updating
		if len(nodes) >= nodeCount {
			op.giveNodeUpdatePermission(updateNodes[0].Name)
		}
	}
}
