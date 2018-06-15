package client

type GCPNodeClient struct {
	gc      *GCPClient
	Project string
	Node    string
	Zone    string
	Region  string
}

type GCPNodeClientInterface interface {
	NeedsUpdate() (bool, error)
	TerminateNode() error
}

func NewNodeClient(project, node, region, zone string) (*GCPNodeClient, error) {
	gc, err := NewGCPClient(project)
	if err != nil {
		return nil, err
	}

	gcn := &GCPNodeClient{
		gc:      gc,
		Project: project,
		Node:    node,
		Zone:    zone,
		Region:  region,
	}
	return gcn, nil

}

func (gcn *GCPNodeClient) NeedsUpdate() (bool, error) {
	return gcn.gc.NeedsUpdate(gcn.Node, gcn.Region, gcn.Zone)
}

func (gcn *GCPNodeClient) TerminateNode() error {
	return gcn.gc.TerminateInstance(gcn.Node, gcn.Region, gcn.Zone)
}
