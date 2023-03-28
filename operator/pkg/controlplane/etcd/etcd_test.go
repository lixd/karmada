package etcd

import (
	"fmt"
	"github.com/karmada-io/karmada/operator/pkg/constants"
	"github.com/karmada-io/karmada/operator/pkg/util"
	appsv1 "k8s.io/api/apps/v1"
	kuberuntime "k8s.io/apimachinery/pkg/runtime"
	clientsetscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/pointer"
	"os"
	"testing"
)

func Test_installKarmadaEtcd(t *testing.T) {
	name := "karmada-ha"
	namespace := "demo"
	image := "docker.io/lixd96/etcd:3.5.5"
	etcdStatefuleSetBytes, err := util.ParseTemplate(KarmadaEtcdStatefulSetHA, struct {
		StatefulSetName, Namespace, Image                       string
		EtcdClientService, CertsSecretName, EtcdPeerServiceName string
		Replicas                                                *int32
		EtcdListenClientPort, EtcdListenPeerPort                int32
		ClusterDomain                                           string
		VolumeName, StorageClassName                            string
		AccessModes                                             []string
		StorageSize                                             string
	}{
		StatefulSetName:      util.KarmadaEtcdName(name),
		Namespace:            namespace,
		Image:                image,
		EtcdClientService:    util.KarmadaEtcdClientName(name),
		CertsSecretName:      util.EtcdCertSecretName(name),
		EtcdPeerServiceName:  util.KarmadaEtcdName(name),
		Replicas:             pointer.Int32(3),
		EtcdListenClientPort: constants.EtcdListenClientPort,
		EtcdListenPeerPort:   constants.EtcdListenPeerPort,
		ClusterDomain:        "cluster.local",
		VolumeName:           "karmada-ha-etcd",
		StorageClassName:     "nfs-sc",
		AccessModes:          []string{"ReadWriteOnce"},
		StorageSize:          "1Gi",
	})
	if err != nil {
		t.Fatal(fmt.Errorf("error when parsing Etcd statefuelset template: %w", err))
	}
	create, err := os.Create("gen-sts.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer create.Close()
	create.Write(etcdStatefuleSetBytes)

	etcdStatefulSet := &appsv1.StatefulSet{}
	if err := kuberuntime.DecodeInto(clientsetscheme.Codecs.UniversalDecoder(), etcdStatefuleSetBytes, etcdStatefulSet); err != nil {
		t.Fatal(fmt.Errorf("error when decoding Etcd StatefulSet: %w", err))
	}
	t.Log(etcdStatefulSet)
}
