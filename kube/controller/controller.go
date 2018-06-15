package controller

import (
	"fmt"
	"log"
	"os"

	v1meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	// in case of local kube config with oidc auth client
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"

	"github.com/utilitywarehouse/kube-node-cycle-operator/kube/k8sutil"
)

type KubeController struct {
}

func main() {
	fmt.Println("vim-go")
}
