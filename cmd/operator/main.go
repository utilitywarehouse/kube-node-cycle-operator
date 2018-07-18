package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

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

	// Listen for force update triggers
	log.Println("[INFO] starting HTTP endpoints ...")
	mux := http.NewServeMux()

	// Add handler func
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t := time.Now()
		err := op.SetLastAcceptedCreationTime(t)
		if err != nil {
			fmt.Fprintf(w, "Error while trying set new force update\n")
			log.Println("[WARN] Error forcing update:", err)
		}
		fmt.Fprintf(w, "Forcing Update for nodes created before: %v\n", t)
	})
	mux.Handle("/forceUpdate", h)

	go func() {
		if err := http.ListenAndServe(":8080", mux); err != nil {
			log.Fatal("could not start HTTP router: ", err)
		}
	}()

	// Run
	op.Run()

}
