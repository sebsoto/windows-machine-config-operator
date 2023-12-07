//go:build windows

package controller

import (
	"context"
	"fmt"
	"strings"

	"github.com/Microsoft/hcsshim"
	"github.com/prometheus/client_golang/prometheus"
	core "k8s.io/api/core/v1"
	k8sapierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

type PodReconciler struct {
	client           client.Client
	networkNameGauge *prometheus.GaugeVec
	nodeName         string
}

// NewPodReconciler responds to Windows pods
func NewPodReconciler(mgr manager.Manager, nodeName string) (*PodReconciler, error) {
	networkNameGauge := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "pod_network_name_info",
	}, []string{"pod",
		"namespace",
		"interface",
		"network_name"})
	metrics.Registry.MustRegister(networkNameGauge)
	return &PodReconciler{client: mgr.GetClient(), networkNameGauge: networkNameGauge, nodeName: nodeName}, nil
}

func (r *PodReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	pod := &core.Pod{}
	klog.Infof("reconciling %s", req.Name)
	if err := r.client.Get(ctx, req.NamespacedName, pod); err != nil {
		if k8sapierrors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return ctrl.Result{}, nil
		}
		// Error reading the object - return error to requeue the request.
		return ctrl.Result{}, err
	}
	// check if containers all started
	for _, container := range pod.Status.ContainerStatuses {
		if container.Started == nil || *container.Started == false {
			klog.Infof("container %s not started yet, requeuing", container.ContainerID)
			return ctrl.Result{Requeue: true}, nil
		}
	}
	trimmedContainerID := strings.TrimPrefix(pod.Status.ContainerStatuses[0].ContainerID, "containerd://")
	endpoint, err := r.getContainerHNSEndpoint(trimmedContainerID)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("error finding HNS endpoint for container %s: %w", trimmedContainerID, err)
	}
	labels := prometheus.Labels{
		"pod":          pod.GetName(),
		"namespace":    pod.GetNamespace(),
		"interface":    strings.ToUpper(endpoint.Id),
		"network_name": endpoint.VirtualNetworkName,
	}
	r.networkNameGauge.With(labels).Add(0)
	return ctrl.Result{}, nil
}

// getContainerHNSEndpoint returns the HNS endpoint used by the given container
func (r *PodReconciler) getContainerHNSEndpoint(containerID string) (*hcsshim.HNSEndpoint, error) {
	hnsEndpoints, err := hcsshim.HNSListEndpointRequest()
	if err != nil {
		return nil, fmt.Errorf("unable to list HNS endpoints: %w", err)
	}
	for _, endpoint := range hnsEndpoints {
		for _, id := range endpoint.SharedContainers {
			if containerID == id {
				return &endpoint, nil
			}
		}
	}
	return nil, fmt.Errorf("unable to find HNS endpoint")
}

// SetupWithManager sets up the controller with the Manager.
func (r *PodReconciler) SetupWithManager(mgr ctrl.Manager) error {
	windowsPodPredicate := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return r.isRunningOnNode(e.Object)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return r.podChanged(e.ObjectOld, e.ObjectNew) && r.isRunningOnNode(e.ObjectNew)
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return false
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// TODO: determine how to clean up metrics
			return false
		},
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&core.Pod{}, builder.WithPredicates(windowsPodPredicate)).
		Complete(r)
}

func (r *PodReconciler) podChanged(oldObj, newObj client.Object) bool {
	if oldObj == nil || newObj == nil {
		return false
	}
	oldSpec, ok := oldObj.(*core.Pod)
	if !ok {
		return false
	}
	newSpec, ok := newObj.(*core.Pod)
	if !ok {
		return false
	}
	return oldSpec.Spec.NodeName != newSpec.Spec.NodeName
}

func (r *PodReconciler) isRunningOnNode(obj client.Object) bool {
	if obj == nil {
		return false
	}
	pod, ok := obj.(*core.Pod)
	if !ok {
		return false
	}
	return pod.Spec.NodeName == r.nodeName
}
