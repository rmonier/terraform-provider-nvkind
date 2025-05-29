package nvkind

import (
	"fmt"
	"log"
	"os"
	"reflect"
	"unsafe"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	clientcmd "k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"sigs.k8s.io/kind/pkg/cluster"
	"sigs.k8s.io/kind/pkg/cmd"

	nvkind "github.com/NVIDIA/nvkind/pkg/nvkind"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

func resourceCluster() *schema.Resource {
	return &schema.Resource{
		Create: resourceKindClusterCreate,
		Read:   resourceKindClusterRead,
		// Update: resourceKindClusterUpdate,
		Delete: resourceKindClusterDelete,

		Timeouts: &schema.ResourceTimeout{
			Create: schema.DefaultTimeout(defaultCreateTimeout),
			Update: schema.DefaultTimeout(defaultUpdateTimeout),
			Delete: schema.DefaultTimeout(defaultDeleteTimeout),
		},

		Schema: map[string]*schema.Schema{
			"name": {
				Type:        schema.TypeString,
				Description: "The kind name that is given to the created cluster.",
				Required:    true,
				ForceNew:    true,
			},
			"node_image": {
				Type:        schema.TypeString,
				Description: `The node_image that kind will use (ex: kindest/node:v1.29.7).`,
				Optional:    true,
				ForceNew:    true,
				Computed:    true,
			},
			"wait_for_ready": {
				Type:        schema.TypeBool,
				Description: `Defines wether or not the provider will wait for the control plane to be ready. Defaults to false`,
				Default:     false,
				ForceNew:    true, // TODO remove this once we have the update method defined.
				Optional:    true,
			},
			"kind_config": {
				Type:        schema.TypeList,
				Description: `The kind_config that kind will use to bootstrap the cluster.`,
				Optional:    true,
				ForceNew:    true,
				MaxItems:    1,
				Elem: &schema.Resource{
					Schema: kindConfigFields(),
				},
			},
			"kubeconfig_path": {
				Type:        schema.TypeString,
				Description: `Kubeconfig path set after the the cluster is created or by the user to override defaults.`,
				ForceNew:    true,
				Optional:    true,
				Computed:    true,
			},
			"kubeconfig": {
				Type:        schema.TypeString,
				Description: `Kubeconfig set after the the cluster is created.`,
				Computed:    true,
			},
			"client_certificate": {
				Type:        schema.TypeString,
				Description: `Client certificate for authenticating to cluster.`,
				Computed:    true,
			},
			"client_key": {
				Type:        schema.TypeString,
				Description: `Client key for authenticating to cluster.`,
				Computed:    true,
			},
			"cluster_ca_certificate": {
				Type:        schema.TypeString,
				Description: `Client verifies the server certificate with this CA cert.`,
				Computed:    true,
			},
			"endpoint": {
				Type:        schema.TypeString,
				Description: `Kubernetes APIServer endpoint.`,
				Computed:    true,
			},
			"completed": {
				Type:        schema.TypeBool,
				Description: `Cluster successfully created.`,
				Computed:    true,
			},
		},
	}
}

func resourceKindClusterCreate(d *schema.ResourceData, meta interface{}) error {
	log.Println("Creating local Kubernetes cluster...")
	name := d.Get("name").(string)
	nodeImage := d.Get("node_image").(string)
	config := d.Get("kind_config")
	waitForReady := d.Get("wait_for_ready").(bool)
	kubeconfigPath := d.Get("kubeconfig_path")

	var copts []cluster.CreateOption
	var clusterConfig *v1alpha4.Cluster
	kubeconfigPathStr := ""

	if kubeconfigPath != nil {
		kubeconfigPathStr = kubeconfigPath.(string)
		if kubeconfigPathStr == "" {
			// Let's add the nvkind default path
			// see: https://github.com/NVIDIA/nvkind/blob/b52126989300fb22e728f741943b1d43d5cf1e4f/pkg/nvkind/cluster.go#L79-L83
			if home := homedir.HomeDir(); home != "" {
				kubeconfigPathStr = home + "/.kube/config"
			}
		}
		copts = append(copts, cluster.CreateWithKubeconfigPath(kubeconfigPathStr))
	}

	if config != nil {
		cfg := config.([]interface{})
		if len(cfg) == 1 { // there is always just one kind_config allowed
			if data, ok := cfg[0].(map[string]interface{}); ok {
				clusterConfig = flattenKindConfig(data)
				copts = append(copts, cluster.CreateWithV1Alpha4Config(clusterConfig))
			}
		}
	}

	if nodeImage != "" {
		copts = append(copts, cluster.CreateWithNodeImage(nodeImage))
		log.Printf("Using defined node_image: %s\n", nodeImage)
	}

	if waitForReady {
		copts = append(copts, cluster.CreateWithWaitForReady(defaultCreateTimeout))
		log.Printf("Will wait for cluster nodes to report ready: %t\n", waitForReady)
	}

	log.Println("=================== Creating NVKind Cluster ==================")
	provider := cluster.NewProvider(cluster.ProviderWithLogger(cmd.NewLogger()))
	err := provider.Create(name, copts...)
	if err != nil {
		return err
	}

	d.SetId(fmt.Sprintf("%s-%s", name, nodeImage))

	// -----------------------------------------------------------------------
	// --- Get the node names for nvkind setup and register Nvidia runtime ---
	// -----------------------------------------------------------------------

	// Get all the nodes and their container name
	nodeList, err := provider.ListNodes(name)
	if err != nil {
		log.Printf("error listing nodes: %v", err)
		return err
	}

	// Let's work on the nvkind main configuration
	var configOptions []nvkind.ConfigOption
	configOptions = append(configOptions, nvkind.WithDefaultName(name))
	if nodeImage != "" {
		configOptions = append(configOptions, nvkind.WithImage(nodeImage))
	}
	nvConfig, err := nvkind.NewConfig(configOptions...)
	if err != nil {
		log.Printf("new nvConfig: %v", err)
		return err
	}

	// Dirty reflection ahead, but we don't want to use the default init
	// as it uses the kind shell commands instead of the package API

	nvConfigReflected := reflect.ValueOf(nvConfig).Elem()

	// We get the automatically generated fileds from NewConfig
	// as we want to let it handle these internals
	// Also, we don't care about the Cluster field
	// as we won't use the config after this fields init
	nvmlReflected := nvConfigReflected.FieldByName("nvml")
	stdoutReflected := nvConfigReflected.FieldByName("stdout")
	stderrReflected := nvConfigReflected.FieldByName("stderr")

	for i, node := range nodeList {
		// Let's create the nvkind Node struct to apply the patches
		nvNode := &nvkind.Node{
			Name: node.String(),
		}
		nvNodeReflected := reflect.ValueOf(nvNode).Elem()

		// We need to override the default placeholders and set or current instances
		// because we can't use the classic Node initialization that uses the kind
		// shell commands instead of the package API

		nodeField := nvNodeReflected.FieldByName("config")
		if nodeField.IsValid() && nodeField.CanAddr() {
			ptr := unsafe.Pointer(nodeField.UnsafeAddr())
			// The clusterConfig.Nodes[i] should work as the nodes are sorted in the same way as they
			// are in the cluster YAML or HCL definition
			reflect.NewAt(nodeField.Type(), ptr).Elem().Set(reflect.ValueOf(&clusterConfig.Nodes[i]))
		}

		nvmlField := nvNodeReflected.FieldByName("nvml")
		if nvmlField.IsValid() && nvmlField.CanAddr() {
			ptr := unsafe.Pointer(nvmlField.UnsafeAddr())
			reflect.NewAt(nvmlField.Type(), ptr).Elem().Set(reflect.ValueOf(nvmlReflected))
		}

		stdoutField := nvNodeReflected.FieldByName("stdout")
		if stdoutField.IsValid() && stdoutField.CanAddr() {
			ptr := unsafe.Pointer(stdoutField.UnsafeAddr())
			reflect.NewAt(stdoutField.Type(), ptr).Elem().Set(reflect.ValueOf(stdoutReflected))
		}

		stderrField := nvNodeReflected.FieldByName("stderr")
		if stderrField.IsValid() && stderrField.CanAddr() {
			ptr := unsafe.Pointer(stderrField.UnsafeAddr())
			reflect.NewAt(stderrField.Type(), ptr).Elem().Set(reflect.ValueOf(stderrReflected))
		}

		// Let's patch the runtime nodes
		// TODO: bypass the docker shell calls in runScript and use cluster.NewProvider instead

		if !nvNode.HasGPUs() {
			continue
		}
		if err := nvNode.InstallContainerToolkit(); err != nil {
			log.Printf("installing container toolkit on node '%v': %v", nvNode.Name, err)
			return err
		}
		if err := nvNode.ConfigureContainerRuntime(); err != nil {
			log.Printf("configuring container runtime on node '%v': %v", nvNode.Name, err)
			return err
		}
		if err := nvNode.PatchProcDriverNvidia(); err != nil {
			log.Printf("patching /proc/driver/nvidia on node '%v': %v", nvNode.Name, err)
			return err
		}
	}

	// Let's create the nvkind Cluster struct to apply the patches

	nvCluster := &nvkind.Cluster{
		Name: name,
	}

	// Again, we need to override the default placeholders and set or current instances
	// because the classic Cluster initialization with NewCluster uses the kind
	// shell commands for the setConfig part instead of the package API

	nvClusterReflected := reflect.ValueOf(nvCluster).Elem()

	clusterField := nvClusterReflected.FieldByName("config")
	if clusterField.IsValid() && clusterField.CanAddr() {
		ptr := unsafe.Pointer(clusterField.UnsafeAddr())
		reflect.NewAt(clusterField.Type(), ptr).Elem().Set(reflect.ValueOf(clusterConfig))
	}

	kubeconfigField := nvClusterReflected.FieldByName("kubeconfig")
	if kubeconfigField.IsValid() && kubeconfigField.CanAddr() {
		ptr := unsafe.Pointer(kubeconfigField.UnsafeAddr())
		reflect.NewAt(kubeconfigField.Type(), ptr).Elem().Set(reflect.ValueOf(kubeconfigPathStr))
	}

	nvmlField := nvClusterReflected.FieldByName("nvml")
	if nvmlField.IsValid() && nvmlField.CanAddr() {
		ptr := unsafe.Pointer(nvmlField.UnsafeAddr())
		reflect.NewAt(nvmlField.Type(), ptr).Elem().Set(reflect.ValueOf(nvmlReflected))
	}

	stdoutField := nvClusterReflected.FieldByName("stdout")
	if stdoutField.IsValid() && stdoutField.CanAddr() {
		ptr := unsafe.Pointer(stdoutField.UnsafeAddr())
		reflect.NewAt(stdoutField.Type(), ptr).Elem().Set(reflect.ValueOf(stdoutReflected))
	}

	stderrField := nvClusterReflected.FieldByName("stderr")
	if stderrField.IsValid() && stderrField.CanAddr() {
		ptr := unsafe.Pointer(stderrField.UnsafeAddr())
		reflect.NewAt(stderrField.Type(), ptr).Elem().Set(reflect.ValueOf(stderrReflected))
	}

	// Let's patch the runtime cluster with kubectl
	// TODO: bypass the kubectl shell call and use clientcmd instead

	if err := nvCluster.RegisterNvidiaRuntimeClass(); err != nil {
		log.Printf("registering runtime class: %v", err)
		return err
	}

	// -----------------------------------------------------------------------
	// -----------------------------------------------------------------------
	// -----------------------------------------------------------------------

	return resourceKindClusterRead(d, meta)
}

func resourceKindClusterRead(d *schema.ResourceData, meta interface{}) error {
	name := d.Get("name").(string)
	provider := cluster.NewProvider(cluster.ProviderWithLogger(cmd.NewLogger()))
	id := d.Id()
	log.Printf("ID: %s\n", id)

	kconfig, err := provider.KubeConfig(name, false)
	if err != nil {
		d.SetId("")
		return err
	}
	d.Set("kubeconfig", kconfig)

	currentPath, err := os.Getwd()
	if err != nil {
		d.SetId("")
		return err
	}

	if _, ok := d.GetOk("kubeconfig_path"); !ok {
		exportPath := fmt.Sprintf("%s%s%s-config", currentPath, string(os.PathSeparator), name)
		err = provider.ExportKubeConfig(name, exportPath, false)
		if err != nil {
			d.SetId("")
			return err
		}
		d.Set("kubeconfig_path", exportPath)
	}

	// use the current context in kubeconfig
	config, err := clientcmd.RESTConfigFromKubeConfig([]byte(kconfig))
	if err != nil {
		return err
	}

	d.Set("client_certificate", string(config.CertData))
	d.Set("client_key", string(config.KeyData))
	d.Set("cluster_ca_certificate", string(config.CAData))
	d.Set("endpoint", string(config.Host))

	d.Set("completed", true)

	return nil
}

func resourceKindClusterDelete(d *schema.ResourceData, meta interface{}) error {
	log.Println("Deleting local Kubernetes cluster...")
	name := d.Get("name").(string)
	kubeconfigPath := d.Get("kubeconfig_path").(string)
	provider := cluster.NewProvider(cluster.ProviderWithLogger(cmd.NewLogger()))

	log.Println("=================== Deleting Kind Cluster ==================")
	err := provider.Delete(name, kubeconfigPath)
	if err != nil {
		return err
	}
	d.SetId("")
	return nil
}
