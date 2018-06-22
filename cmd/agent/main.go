package main

import (
	"flag"
	"log"
	"os"

	gclient "github.com/utilitywarehouse/kube-node-cycle-operator/cloud/gcp/client"
	"github.com/utilitywarehouse/kube-node-cycle-operator/cloud/gcp/meta"
	"github.com/utilitywarehouse/kube-node-cycle-operator/pkg/agent"
)

var (
	// flags
	flagProject    = flag.String("project", "", "(Required) GCP Project to use")
	flagRegion     = flag.String("region", "", "(Required) Region where the node lives")
	flagKubeConfig = flag.String("conf_file", "", "(Optional) Path of the kube config file to use. Defaults to incluster config for pods")
)

func usage() {
	flag.Usage()
	os.Exit(2)
}
func main() {
	// Flag Parsing
	flag.Parse()

	if *flagProject == "" {
		usage()
	}
	project := *flagProject

	// Data from instance metadata
	nodeName := meta.InstanceName()
	hostName := meta.InstanceHostname()
	zone := meta.InstanceZone()

	region := *flagRegion

	// gcp client
	gc, err := gclient.NewNodeClient(project, nodeName, region, zone)
	if err != nil {
		log.Fatal(err)
	}

	// create a new agent
	a, err := agent.New(hostName, *flagKubeConfig, gc)
	if err != nil {
		log.Fatal(err)
	}
	a.Run()

}
