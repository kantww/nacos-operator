package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/client-go/kubernetes"

	nacosClient "nacos.io/nacos-operator/pkg/service/nacos"
	"nacos.io/nacos-operator/test/e2e/util"
)

var namespace = "default"
var name = "nacos"
var clientSet *kubernetes.Clientset

func Init() {
	flag.StringVar(&namespace, "namespace", "default", "namespace")
	flag.StringVar(&name, "name", "nacos", "namespace")
}

func main() {
	Init()
	flag.Parse()
	clientSet = util.GetClientSet()
	podList, err := clientSet.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app=%s", name),
	})
	if err != nil {
		fmt.Println(err)
		return
	}

	if len(podList.Items) == 0 {
		fmt.Println("pod 数量为0")
		return
	}
	leader := ""
	client := nacosClient.NacosClient{}
    for _, pod := range podList.Items {
        svc, err := client.GetClusterNodes(pod.Status.PodIP)
        if err != nil {
            fmt.Println(err)
            return
        }
        if len(podList.Items) != len(svc.Data) {
            fmt.Println(pod.Name)
            str, _ := json.Marshal(svc)
            fmt.Printf("%s\n", str)
            fmt.Println("servers 数量 不匹配")
            return
        }
        // Prefer v2 leader path, fallback to other groups
        var gotLeader string
        if len(svc.Data) > 0 {
            gotLeader = svc.Data[0].ExtendInfo.RaftMetaData.MetaDataMap.NamingPersistentServiceV2.Leader
            if gotLeader == "" {
                gotLeader = svc.Data[0].ExtendInfo.RaftMetaData.MetaDataMap.NamingServiceMetadata.Leader
            }
            if gotLeader == "" {
                gotLeader = svc.Data[0].ExtendInfo.RaftMetaData.MetaDataMap.NamingInstanceMetadata.Leader
            }
        }
        if leader == "" {
            leader = gotLeader
        } else {
            if leader != gotLeader {
                fmt.Println("leader 不匹配")
                return
            }
        }

    }
	fmt.Printf("leader is %s\n", leader)
	fmt.Println("success")
}
