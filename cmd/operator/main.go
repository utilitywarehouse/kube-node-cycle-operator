package main

import (
	"flag"
	"log"
	"os"

	"github.com/utilitywarehouse/kube-node-cycle-operator/pkg/operator"
)

var (
	// flags
	flagKubeConfig = flag.String("conf_file", "", "(Optional) Path of the kube config file to use. Defaults to incluster config for pods")
	flagStatePath  = flag.String("state_path", "", "(Required) Path of the file where operator shall keep the state info. Shall be part of a persistent volume")
)

func usage() {
	flag.Usage()
	os.Exit(2)
}
func main() {
	// Flag Parsing
	flag.Parse()

	if *flagStatePath == "" {
		usage()
	}

	// create a new agent
	op, err := operator.New(*flagKubeConfig, *flagStatePath)
	if err != nil {
		log.Fatal(err)
	}
	op.Run()

}
