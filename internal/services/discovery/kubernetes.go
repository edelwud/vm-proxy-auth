package discovery

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/edelwud/vm-proxy-auth/internal/domain"
)

const (
	k8sDefaultUpdateInterval   = 30 * time.Second
	k8sDefaultWatchChannelSize = 100
	k8sWatchRetryDelay         = 5 * time.Second
	defaultHTTPPortName        = "http"
	defaultRaftPortName        = "raft"
)

// KubernetesDiscoveryConfig holds Kubernetes service discovery configuration.
type KubernetesDiscoveryConfig struct {
	Namespace            string        `json:"namespace"`
	PeerLabelSelector    string        `json:"peer_label_selector"`
	BackendLabelSelector string        `json:"backend_label_selector"`
	RaftPortName         string        `json:"raft_port_name"`
	HTTPPortName         string        `json:"http_port_name"`
	WatchTimeout         time.Duration `json:"watch_timeout"`
}

// KubernetesDiscovery implements ServiceDiscovery using Kubernetes API.
type KubernetesDiscovery struct {
	client   kubernetes.Interface
	config   KubernetesDiscoveryConfig
	logger   domain.Logger
	watchCh  chan domain.ServiceDiscoveryEvent
	stopCh   chan struct{}
	lastSeen map[string]time.Time
}

// NewKubernetesDiscovery creates a new Kubernetes service discovery client.
func NewKubernetesDiscovery(config KubernetesDiscoveryConfig, logger domain.Logger) (*KubernetesDiscovery, error) {
	clientConfig, err := getKubernetesConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get Kubernetes config: %w", err)
	}

	client, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// Default namespace if not specified
	if config.Namespace == "" {
		config.Namespace = "default"
	}

	// Default selectors
	if config.PeerLabelSelector == "" {
		config.PeerLabelSelector = "app=vm-proxy-auth"
	}
	if config.BackendLabelSelector == "" {
		config.BackendLabelSelector = "app=victoriametrics"
	}
	if config.RaftPortName == "" {
		config.RaftPortName = defaultRaftPortName
	}
	if config.HTTPPortName == "" {
		config.HTTPPortName = defaultHTTPPortName
	}
	if config.WatchTimeout == 0 {
		config.WatchTimeout = k8sDefaultWatchTimeout
	}

	kd := &KubernetesDiscovery{
		client:   client,
		config:   config,
		logger:   logger.With(domain.Field{Key: "component", Value: "k8s.discovery"}),
		watchCh:  make(chan domain.ServiceDiscoveryEvent, k8sDefaultWatchChannelSize),
		stopCh:   make(chan struct{}),
		lastSeen: make(map[string]time.Time),
	}

	return kd, nil
}

// getKubernetesConfig attempts to get Kubernetes configuration.
func getKubernetesConfig() (*rest.Config, error) {
	// Try in-cluster config first
	if config, err := rest.InClusterConfig(); err == nil {
		return config, nil
	}

	// Try kubeconfig file
	kubeconfigPath := os.Getenv("KUBECONFIG")
	if kubeconfigPath == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		kubeconfigPath = filepath.Join(homeDir, ".kube", "config")
	}

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to build config from kubeconfig: %w", err)
	}

	return config, nil
}

// DiscoverPeers discovers peer nodes for Raft cluster formation.
func (kd *KubernetesDiscovery) DiscoverPeers(ctx context.Context) ([]*domain.PeerInfo, error) {
	pods, err := kd.client.CoreV1().Pods(kd.config.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: kd.config.PeerLabelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	var peers []*domain.PeerInfo
	for _, pod := range pods.Items {
		peer, ok := kd.processPodForPeer(pod)
		if ok {
			peers = append(peers, peer)
		}
	}

	kd.logger.Debug("Discovered Raft peers",
		domain.Field{Key: "peers_count", Value: len(peers)},
		domain.Field{Key: "namespace", Value: kd.config.Namespace})

	return peers, nil
}

// processPodForPeer extracts peer information from a Kubernetes pod.
func (kd *KubernetesDiscovery) processPodForPeer(pod corev1.Pod) (*domain.PeerInfo, bool) {
	if pod.Status.Phase != corev1.PodRunning {
		return nil, false
	}

	nodeID := kd.extractNodeID(pod)
	raftAddress, httpAddress := kd.extractPodAddresses(pod)

	if raftAddress == "" {
		kd.logger.Warn("Pod has no Raft port defined",
			domain.Field{Key: "pod_name", Value: pod.Name},
			domain.Field{Key: "expected_port_name", Value: kd.config.RaftPortName})
		return nil, false
	}

	healthy := kd.isPodHealthy(pod)
	metadata := kd.buildPodMetadata(pod)

	return &domain.PeerInfo{
		NodeID:      nodeID,
		HTTPAddress: httpAddress,
		RaftAddress: raftAddress,
		Healthy:     healthy,
		LastSeen:    time.Now(),
		Metadata:    metadata,
	}, true
}

// extractNodeID extracts node ID from pod labels or uses pod name as fallback.
func (kd *KubernetesDiscovery) extractNodeID(pod corev1.Pod) string {
	if nodeID := pod.Labels["node-id"]; nodeID != "" {
		return nodeID
	}
	return pod.Name
}

// extractPodAddresses finds Raft and HTTP addresses from pod containers.
func (kd *KubernetesDiscovery) extractPodAddresses(pod corev1.Pod) (string, string) {
	var raftAddress, httpAddress string
	for _, container := range pod.Spec.Containers {
		for _, port := range container.Ports {
			switch port.Name {
			case kd.config.RaftPortName:
				raftAddress = fmt.Sprintf("%s:%d", pod.Status.PodIP, port.ContainerPort)
			case kd.config.HTTPPortName:
				httpAddress = fmt.Sprintf("%s:%d", pod.Status.PodIP, port.ContainerPort)
			}
		}
	}
	return raftAddress, httpAddress
}

// isPodHealthy checks if pod is ready.
func (kd *KubernetesDiscovery) isPodHealthy(pod corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

// buildPodMetadata creates metadata map from pod information.
func (kd *KubernetesDiscovery) buildPodMetadata(pod corev1.Pod) map[string]string {
	metadata := map[string]string{
		"pod_name":      pod.Name,
		"pod_namespace": pod.Namespace,
		"pod_ip":        pod.Status.PodIP,
		"node_name":     pod.Spec.NodeName,
	}

	// Add custom metadata from pod labels
	for key, value := range pod.Labels {
		if strings.HasPrefix(key, "discovery.") {
			metadata[key] = value
		}
	}

	return metadata
}

// DiscoverBackends discovers backend services (VictoriaMetrics instances).
func (kd *KubernetesDiscovery) DiscoverBackends(ctx context.Context) ([]*domain.BackendInfo, error) {
	services, err := kd.client.CoreV1().Services(kd.config.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: kd.config.BackendLabelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list services: %w", err)
	}

	var backends []*domain.BackendInfo
	for _, service := range services.Items {
		backend, ok := kd.processServiceForBackend(service)
		if ok {
			backends = append(backends, backend)
		}
	}

	// Enrich with endpoints health information
	kd.enrichBackendsWithEndpointsIfAvailable(ctx, backends)

	kd.logger.Debug("Discovered backend services",
		domain.Field{Key: "backends_count", Value: len(backends)},
		domain.Field{Key: "namespace", Value: kd.config.Namespace})

	return backends, nil
}

// processServiceForBackend converts a Kubernetes service to BackendInfo.
func (kd *KubernetesDiscovery) processServiceForBackend(service corev1.Service) (*domain.BackendInfo, bool) {
	port := kd.findHTTPPort(service)
	if port == 0 {
		kd.logger.Warn("Service has no HTTP port defined",
			domain.Field{Key: "service_name", Value: service.Name})
		return nil, false
	}

	url := fmt.Sprintf("http://%s.%s.svc.cluster.local:%d",
		service.Name, service.Namespace, port)

	weight := kd.extractServiceWeight(service)
	metadata := kd.buildServiceMetadata(service)

	return &domain.BackendInfo{
		URL:      url,
		Weight:   weight,
		Healthy:  true, // Assume healthy, health checker will validate
		LastSeen: time.Now(),
		Metadata: metadata,
	}, true
}

// findHTTPPort finds the HTTP port in service spec.
func (kd *KubernetesDiscovery) findHTTPPort(service corev1.Service) int32 {
	for _, svcPort := range service.Spec.Ports {
		if svcPort.Name == kd.config.HTTPPortName || svcPort.Name == defaultHTTPPortName {
			return svcPort.Port
		}
	}
	return 0
}

// extractServiceWeight extracts weight from service annotations.
func (kd *KubernetesDiscovery) extractServiceWeight(service corev1.Service) int {
	weight := 1
	if weightStr, exists := service.Annotations["vm-proxy-auth/weight"]; exists {
		if w, err := strconv.Atoi(weightStr); err == nil && w > 0 {
			weight = w
		}
	}
	return weight
}

// buildServiceMetadata creates metadata from service information.
func (kd *KubernetesDiscovery) buildServiceMetadata(service corev1.Service) map[string]string {
	metadata := map[string]string{
		"service_name":      service.Name,
		"service_namespace": service.Namespace,
		"service_type":      string(service.Spec.Type),
		"cluster_ip":        service.Spec.ClusterIP,
	}

	// Add custom metadata from service annotations
	for key, value := range service.Annotations {
		if strings.HasPrefix(key, "vm-proxy-auth/") {
			metadata[key] = value
		}
	}

	return metadata
}

// enrichBackendsWithEndpointsIfAvailable enriches backends with endpoint health if available.
func (kd *KubernetesDiscovery) enrichBackendsWithEndpointsIfAvailable(
	ctx context.Context, backends []*domain.BackendInfo,
) {
	endpoints, err := kd.client.CoreV1().Endpoints(kd.config.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: kd.config.BackendLabelSelector,
	})
	if err == nil {
		kd.enrichBackendsWithEndpoints(backends, endpoints.Items)
	}
}

// enrichBackendsWithEndpoints adds endpoint health information to backends.
func (kd *KubernetesDiscovery) enrichBackendsWithEndpoints(
	backends []*domain.BackendInfo,
	endpoints []corev1.Endpoints,
) {
	for i := range backends {
		serviceName := backends[i].Metadata["service_name"]

		for _, endpoint := range endpoints {
			if endpoint.Name == serviceName {
				// Check if any subset has ready addresses
				hasReady := false
				totalAddresses := 0
				readyAddresses := 0

				for _, subset := range endpoint.Subsets {
					totalAddresses += len(subset.Addresses) + len(subset.NotReadyAddresses)
					readyAddresses += len(subset.Addresses)
					if len(subset.Addresses) > 0 {
						hasReady = true
					}
				}

				backends[i].Healthy = hasReady
				backends[i].Metadata["ready_addresses"] = strconv.Itoa(readyAddresses)
				backends[i].Metadata["total_addresses"] = strconv.Itoa(totalAddresses)
				break
			}
		}
	}
}

// Watch monitors changes in service topology.
func (kd *KubernetesDiscovery) Watch(ctx context.Context) (<-chan domain.ServiceDiscoveryEvent, error) {
	// Start watching pods for peer changes
	go kd.watchPods(ctx)

	// Start watching services for backend changes
	go kd.watchServices(ctx)

	return kd.watchCh, nil
}

// mapWatchEventType converts Kubernetes watch event to service discovery event type.
func mapWatchEventType(eventType watch.EventType) (domain.ServiceDiscoveryEventType, bool) {
	switch eventType {
	case watch.Added:
		return domain.ServiceDiscoveryEventTypePeerJoined, true
	case watch.Deleted:
		return domain.ServiceDiscoveryEventTypePeerLeft, true
	case watch.Modified:
		return domain.ServiceDiscoveryEventTypePeerUpdated, true
	case watch.Bookmark, watch.Error:
		return "", false
	default:
		return "", false
	}
}

// createPeerInfoFromPod creates PeerInfo from Kubernetes pod.
func (kd *KubernetesDiscovery) createPeerInfoFromPod(pod *corev1.Pod) *domain.PeerInfo {
	nodeID := pod.Name
	if pod.Labels["node-id"] != "" {
		nodeID = pod.Labels["node-id"]
	}

	httpAddress, raftAddress := kd.extractPodAddresses(*pod)

	return &domain.PeerInfo{
		NodeID:      nodeID,
		HTTPAddress: httpAddress,
		RaftAddress: raftAddress,
		Healthy:     pod.Status.Phase == corev1.PodRunning,
		LastSeen:    time.Now(),
		Metadata: map[string]string{
			"pod_name":      pod.Name,
			"pod_namespace": pod.Namespace,
			"pod_ip":        pod.Status.PodIP,
		},
	}
}

// sendDiscoveryEvent sends a discovery event through the watch channel.
func (kd *KubernetesDiscovery) sendDiscoveryEvent(
	eventType domain.ServiceDiscoveryEventType, peerInfo *domain.PeerInfo, podName string,
) {
	discoveryEvent := domain.ServiceDiscoveryEvent{
		Type:      eventType,
		Peer:      peerInfo,
		Timestamp: time.Now(),
	}

	select {
	case kd.watchCh <- discoveryEvent:
	default:
		kd.logger.Warn("Service discovery event channel full, dropping event",
			domain.Field{Key: "event_type", Value: string(eventType)},
			domain.Field{Key: "pod_name", Value: podName})
	}
}

// processPodWatchEvent processes a single pod watch event.
func (kd *KubernetesDiscovery) processPodWatchEvent(event watch.Event) {
	pod, ok := event.Object.(*corev1.Pod)
	if !ok {
		return
	}

	eventType, validEvent := mapWatchEventType(event.Type)
	if !validEvent {
		return
	}

	peerInfo := kd.createPeerInfoFromPod(pod)
	kd.sendDiscoveryEvent(eventType, peerInfo, pod.Name)
}

// mapServiceWatchEventType converts Kubernetes service watch event to backend discovery event type.
func mapServiceWatchEventType(eventType watch.EventType) (domain.ServiceDiscoveryEventType, bool) {
	switch eventType {
	case watch.Added:
		return domain.ServiceDiscoveryEventTypeBackendAdded, true
	case watch.Deleted:
		return domain.ServiceDiscoveryEventTypeBackendRemoved, true
	case watch.Modified:
		return domain.ServiceDiscoveryEventTypeBackendUpdated, true
	case watch.Bookmark, watch.Error:
		return "", false
	default:
		return "", false
	}
}

// createBackendInfoFromService creates BackendInfo from Kubernetes service.
func (kd *KubernetesDiscovery) createBackendInfoFromService(service *corev1.Service) *domain.BackendInfo {
	var port int32
	for _, svcPort := range service.Spec.Ports {
		if svcPort.Name == kd.config.HTTPPortName || svcPort.Name == defaultHTTPPortName {
			port = svcPort.Port
			break
		}
	}

	if port == 0 {
		return nil
	}

	url := fmt.Sprintf("http://%s.%s.svc.cluster.local:%d",
		service.Name, service.Namespace, port)

	weight := 1
	if weightStr, exists := service.Annotations["vm-proxy-auth/weight"]; exists {
		if w, parseErr := strconv.Atoi(weightStr); parseErr == nil && w > 0 {
			weight = w
		}
	}

	return &domain.BackendInfo{
		URL:      url,
		Weight:   weight,
		Healthy:  true,
		LastSeen: time.Now(),
		Metadata: map[string]string{
			"service_name":      service.Name,
			"service_namespace": service.Namespace,
			"service_type":      string(service.Spec.Type),
		},
	}
}

// sendBackendDiscoveryEvent sends a backend discovery event through the watch channel.
func (kd *KubernetesDiscovery) sendBackendDiscoveryEvent(
	eventType domain.ServiceDiscoveryEventType, backendInfo *domain.BackendInfo, serviceName string,
) {
	discoveryEvent := domain.ServiceDiscoveryEvent{
		Type:      eventType,
		Backend:   backendInfo,
		Timestamp: time.Now(),
	}

	select {
	case kd.watchCh <- discoveryEvent:
	default:
		kd.logger.Warn("Service discovery event channel full, dropping event",
			domain.Field{Key: "event_type", Value: string(eventType)},
			domain.Field{Key: "service_name", Value: serviceName})
	}
}

// processServiceWatchEvent processes a single service watch event.
func (kd *KubernetesDiscovery) processServiceWatchEvent(event watch.Event) {
	service, ok := event.Object.(*corev1.Service)
	if !ok {
		return
	}

	eventType, validEvent := mapServiceWatchEventType(event.Type)
	if !validEvent {
		return
	}

	backendInfo := kd.createBackendInfoFromService(service)
	if backendInfo == nil {
		return
	}

	kd.sendBackendDiscoveryEvent(eventType, backendInfo, service.Name)
}

// watchPods watches for pod changes (peer nodes).
func (kd *KubernetesDiscovery) watchPods(ctx context.Context) {
	kd.logger.Info("Starting pod watcher for peer discovery")

	for {
		select {
		case <-ctx.Done():
			return
		case <-kd.stopCh:
			return
		default:
		}

		watcher, err := kd.client.CoreV1().Pods(kd.config.Namespace).Watch(ctx, metav1.ListOptions{
			LabelSelector: kd.config.PeerLabelSelector,
		})
		if err != nil {
			kd.logger.Error("Failed to create pod watcher",
				domain.Field{Key: "error", Value: err.Error()})
			time.Sleep(k8sWatchRetryDelay)
			continue
		}

		for event := range watcher.ResultChan() {
			kd.processPodWatchEvent(event)
		}

		watcher.Stop()
		time.Sleep(1 * time.Second) // Brief pause before reconnecting
	}
}

// watchServices watches for service changes (backend services).
func (kd *KubernetesDiscovery) watchServices(ctx context.Context) {
	kd.logger.Info("Starting service watcher for backend discovery")

	for {
		select {
		case <-ctx.Done():
			return
		case <-kd.stopCh:
			return
		default:
		}

		watcher, err := kd.client.CoreV1().Services(kd.config.Namespace).Watch(ctx, metav1.ListOptions{
			LabelSelector: kd.config.BackendLabelSelector,
		})
		if err != nil {
			kd.logger.Error("Failed to create service watcher",
				domain.Field{Key: "error", Value: err.Error()})
			time.Sleep(k8sWatchRetryDelay)
			continue
		}

		for event := range watcher.ResultChan() {
			kd.processServiceWatchEvent(event)
		}

		watcher.Stop()
		time.Sleep(1 * time.Second)
	}
}

// RegisterSelf registers this node in the service discovery system.
func (kd *KubernetesDiscovery) RegisterSelf(ctx context.Context, nodeInfo domain.NodeInfo) error {
	// In Kubernetes, self-registration is typically handled by the deployment
	// We can optionally update pod annotations with node information

	podName := os.Getenv("POD_NAME")
	if podName == "" {
		podName = os.Getenv("HOSTNAME")
	}

	if podName == "" {
		kd.logger.Debug("No pod name available for self-registration")
		return nil
	}

	// Get current pod
	pod, err := kd.client.CoreV1().Pods(kd.config.Namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get current pod: %w", err)
	}

	// Update pod annotations with node info
	if pod.Annotations == nil {
		pod.Annotations = make(map[string]string)
	}

	pod.Annotations["vm-proxy-auth/node-id"] = nodeInfo.NodeID
	pod.Annotations["vm-proxy-auth/version"] = nodeInfo.Version
	pod.Annotations["vm-proxy-auth/start-time"] = nodeInfo.StartTime.Format(time.RFC3339)
	pod.Annotations["vm-proxy-auth/raft-address"] = nodeInfo.RaftAddress

	// Add custom metadata
	for key, value := range nodeInfo.Metadata {
		pod.Annotations[fmt.Sprintf("vm-proxy-auth/meta-%s", key)] = value
	}

	_, err = kd.client.CoreV1().Pods(kd.config.Namespace).Update(ctx, pod, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update pod annotations: %w", err)
	}

	kd.logger.Info("Registered node in Kubernetes",
		domain.Field{Key: "node_id", Value: nodeInfo.NodeID},
		domain.Field{Key: "pod_name", Value: podName})

	return nil
}

// Start begins the service discovery process.
func (kd *KubernetesDiscovery) Start(ctx context.Context) error {
	kd.logger.Info("Starting Kubernetes service discovery")

	go kd.watchPods(ctx)
	go kd.watchServices(ctx)

	return nil
}

// Stop gracefully shuts down the service discovery.
func (kd *KubernetesDiscovery) Stop() error {
	kd.logger.Info("Shutting down Kubernetes service discovery")

	close(kd.stopCh)
	close(kd.watchCh)

	return nil
}

// Events returns a channel for receiving discovery events.
func (kd *KubernetesDiscovery) Events() <-chan domain.ServiceDiscoveryEvent {
	return kd.watchCh
}

// GetClusterInfo returns information about the Kubernetes cluster.
func (kd *KubernetesDiscovery) GetClusterInfo(ctx context.Context) (map[string]interface{}, error) {
	version, err := kd.client.Discovery().ServerVersion()
	if err != nil {
		return nil, fmt.Errorf("failed to get server version: %w", err)
	}

	nodes, err := kd.client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	return map[string]interface{}{
		"kubernetes_version": version.String(),
		"node_count":         len(nodes.Items),
		"namespace":          kd.config.Namespace,
		"peer_selector":      kd.config.PeerLabelSelector,
		"backend_selector":   kd.config.BackendLabelSelector,
	}, nil
}
