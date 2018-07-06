package operator

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"time"

	"github.com/pkg/errors"
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
	// https://golang.org/src/time/format.go
	TimestampFormat string = "Mon, 02 Jan 2006 15:04:05 -0700" //RFC1123Z
)

type State struct {
	NodeCount                int       `json:nodecount`
	LastAcceptedCreationTime time.Time `json:lastAcceptedCreationTime`
}

type Operator struct {
	kc        kubernetes.Interface
	nc        v1core.NodeInterface
	statePath string
	state     *State
}

type OperatorInterface interface {
	readStateFromJson() (State, error)
	flushStateToJson() error
	getNodeCount() (int, error)
	getLastAcceptedCreationTime() (time.Time, error)
	setNodeCount(count int) error
	SetLastAcceptedCreationTime(t time.Time) error
	getNodes() ([]v1.Node, error)
	getReadyNodes() ([]v1.Node, error)
	updateNeeded(nodes []v1.Node) (bool, []v1.Node)
	forceUpdateNeeded(nodes []v1.Node) (forceUpdateNeeded bool, forceUpdateNodes []v1.Node)
	nextToUpdate(updateNodes []v1.Node) (v1.Node, error)
	updateInProgress(nodes []v1.Node) bool
	updatePermissionGiven(nodes []v1.Node) bool
	giveNodeUpdatePermission(nodeName string)
	forceNodeUpdate(nodeName string)
	Run()
}

func New(kubeConfig, statePath string) (*Operator, error) {
	// kube client
	kubeClient, err := k8sutil.GetClient(kubeConfig)
	if err != nil {
		return nil, err
	}

	// node interface
	kubeNodeInterface := kubeClient.CoreV1().Nodes()

	operator := &Operator{
		kc:        kubeClient,
		nc:        kubeNodeInterface,
		statePath: statePath,
		state:     &State{},
	}

	log.Println("[INFO] Initialised new operator")
	log.Println("[INFO] Looking for existing state..")
	s, err := operator.readStateFromJson()
	if err != nil {
		log.Println("[INFO] Cannot get state, flushing the new empty one")
		operator.flushStateToJson()
	} else {
		log.Println("[INFO] Previous state found")
		operator.state = s
	}
	return operator, nil
}

func (op *Operator) readStateFromJson() (*State, error) {
	raw, err := ioutil.ReadFile(op.statePath)
	if err != nil {
		return &State{}, err
	}

	s := &State{}
	if err := json.Unmarshal(raw, s); err != nil {
		return &State{}, err
	}
	return s, nil
}

func (op *Operator) flushStateToJson() error {
	raw, err := json.Marshal(op.state)
	if err != nil {
		return err
	}

	if err = ioutil.WriteFile(op.statePath, raw, 0644); err != nil {
		return err
	}
	return nil
}

func (op *Operator) getNodeCount() (int, error) {

	s, err := op.readStateFromJson()
	if err != nil {
		return 0, err
	}
	return s.NodeCount, nil
}

func (op *Operator) getLastAcceptedCreationTime() (time.Time, error) {

	s, err := op.readStateFromJson()
	if err != nil {
		return time.Time{}, err
	}
	return s.LastAcceptedCreationTime, nil
}

func (op *Operator) setNodeCount(count int) error {

	op.state.NodeCount = count

	if err := op.flushStateToJson(); err != nil {
		return err
	}
	return nil
}

// SetLastAcceptedCreationTime: used to inject lastAcceptedCreationTime into state
// from outside the operator
func (op *Operator) SetLastAcceptedCreationTime(t time.Time) error {

	op.state.LastAcceptedCreationTime = t

	if err := op.flushStateToJson(); err != nil {
		return err
	}
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
			log.Println(fmt.Sprintf("[INFO] node %s has no annotation %s", n.Name, annotations.UpdateNeeded))
		} else {
			if n.Annotations[annotations.UpdateNeeded] == annotations.AnnoTrue {
				updateNeeded = true
				updateNodes = append(updateNodes, n)
			}
		}
	}
	return updateNeeded, updateNodes
}

func (op *Operator) forceUpdateNeeded(nodes []v1.Node) (forceUpdateNeeded bool, forceUpdateNodes []v1.Node) {

	lastAccepted, err := op.getLastAcceptedCreationTime()
	if err != nil {
		log.Fatal("error getting Last Accepted Time")
	}
	// Make time comply with k8s apimachinery time
	t := v1meta.NewTime(lastAccepted)
	for _, n := range nodes {
		if n.CreationTimestamp.Before(&t) {
			forceUpdateNeeded = true
			forceUpdateNodes = append(forceUpdateNodes, n)
		}
	}
	return forceUpdateNeeded, forceUpdateNodes
}

// nextToUpdate: gets a list of nodes and searches for `role=master` label. It returns the first `master`
// it may find or else the first node in the list
// errors on empty input list.
func (op *Operator) nextToUpdate(updateNodes []v1.Node) (v1.Node, error) {

	if len(updateNodes) <= 0 {
		return v1.Node{}, errors.New("Empty list passed to nextToUpdate function")
	}
	for _, n := range updateNodes {
		if _, ok := n.Labels["role"]; !ok {
			log.Println("[WARN] node without role label:", n.Name)
		} else {
			if n.Labels["role"] == "master" {
				log.Println("[INFO] found master node that needs updating: ", n.Name)
				return n, nil
			}
		}
	}
	log.Println("[INFO] next node to update:", updateNodes[0].Name)
	return updateNodes[0], nil
}

func (op *Operator) updateInProgress(nodes []v1.Node) bool {
	for _, n := range nodes {
		if _, ok := n.Annotations[annotations.UpdateInProgress]; !ok {
			log.Println(fmt.Sprintf("[INFO] node %s has no annotation %s", n.Name, annotations.UpdateInProgress))
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
			log.Println(fmt.Sprintf("[INFO] node %s has no annotation %s", n.Name, annotations.CanStartTermination))
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

func (op *Operator) forceNodeUpdate(nodeName string) {
	anno := map[string]string{
		annotations.CanStartTermination: annotations.AnnoTrue,
		annotations.ForceTermination:    annotations.AnnoTrue,
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
			log.Println("[ERROR] error getting nodes:", err)
			continue
		}

		nodes, err := op.getReadyNodes()
		if err != nil {
			log.Println("[ERROR] error getting nodes:", err)
			continue
		}

		// Check for Not Ready Nodes
		if len(allNodes) > len(nodes) {
			log.Println("[INFO] Not Ready nodes found, waiting..")
			continue
		}

		// Get nodes that need updating
		updateNeeded, updateNodes := op.updateNeeded(nodes)
		// Get nodes tha we need to force Update
		forceUpdateNeeded, forceUpdateNodes := op.forceUpdateNeeded(nodes)

		// If no update is needed just update the node count with the current number and continue
		if !updateNeeded && !forceUpdateNeeded {
			log.Println("[INFO] no updated needed, updating node count to:", len(nodes))
			op.setNodeCount(len(nodes))
			continue
		}

		// Update needed.
		// If update is in progress or permission already given just wait
		if op.updateInProgress(nodes) || op.updatePermissionGiven(nodes) {
			log.Println("[INFO] updating in progress")
			continue
		}

		nodeCount, err := op.getNodeCount()
		if err != nil {
			log.Fatal("Failed to get node count, exiting")
		}

		// Force Update nodes take precedence
		// If we have enough nodes give permission to start updating
		if len(nodes) >= nodeCount && forceUpdateNeeded {
			n, err := op.nextToUpdate(forceUpdateNodes)
			if err != nil {
				log.Println("[ERROR] error while searching for next node to update:", err)
			}
			op.forceNodeUpdate(n.Name)
		}

		if len(nodes) >= nodeCount && !forceUpdateNeeded {
			n, err := op.nextToUpdate(updateNodes)
			if err != nil {
				log.Println("[ERROR] error while searching for next node to update:", err)
			}
			op.giveNodeUpdatePermission(n.Name)
		}
	}
}
