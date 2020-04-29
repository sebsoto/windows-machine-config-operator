package windowsmachineconfig

import (
	"context"
	"fmt"
	"strings"

	wmcapi "github.com/openshift/windows-machine-config-operator/pkg/apis/wmc/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	k8sapierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type StatusManager struct {
	client             client.Client
	name               types.NamespacedName
	status             *wmcapi.WindowsMachineConfigStatus
	reconciling        bool
	degradationReasons map[string][]string
}

func NewStatusManager(client client.Client, name types.NamespacedName) *StatusManager {
	return &StatusManager{
		degradationReasons: make(map[string][]string),
		client:             client,
		name:               name,
		status:             &wmcapi.WindowsMachineConfigStatus{},
	}
}
func (s *StatusManager) addDegradationReason(reason, message string) {
	messages, present := s.degradationReasons[reason]
	if present {
		s.degradationReasons[reason] = append(messages, message)
	} else {
		s.degradationReasons[reason] = []string{message}
	}
}

// setReconciling updates the CR's status to indicate that the reconciler is active
func (s *StatusManager) setReconciling() {
	object := &wmcapi.WindowsMachineConfig{}
	err := s.client.Get(context.TODO(), s.name, object)
	if err != nil {
		if k8sapierrors.IsNotFound(err) {
			return
		}
		log.Error(err, fmt.Sprintf("could not get %v", s.name))
	}
	wmcapi.SetWindowsMachineConfigCondition(&object.Status,
		*wmcapi.NewWindowsMachineConfigCondition(wmcapi.Reconciling, corev1.ConditionTrue, "", ""))
	if err = s.client.Status().Update(context.TODO(), object); err != nil {
		log.Error(err, fmt.Sprintf("could not update status to %v", object.Status))
	} else {
		log.Info("updated status", "status", object.Status)
	}
}

// updateStatus syncs the current status with the cluster and removes the reconciling status condition
func (s *StatusManager) updateStatus() {
	object := &wmcapi.WindowsMachineConfig{}
	err := s.client.Get(context.TODO(), s.name, object)
	if err != nil {
		if k8sapierrors.IsNotFound(err) {
			return
		}
		log.Error(err, fmt.Sprintf("could not get %v", s.name))
	}
	object.Status.JoinedVMCount = s.status.JoinedVMCount
	degraded := s.getDegradedCondition()
	wmcapi.SetWindowsMachineConfigCondition(&object.Status, *degraded)
	wmcapi.SetWindowsMachineConfigCondition(&object.Status,
		*wmcapi.NewWindowsMachineConfigCondition(wmcapi.Reconciling, corev1.ConditionFalse, "", ""))
	if err = s.client.Status().Update(context.TODO(), object); err != nil {
		log.Error(err, fmt.Sprintf("could not update status to %v", object.Status))
	} else {
		log.Info("updated status", "status", object.Status)
	}
}

// getDegradedCondition returns a degraded condition defined by the degradationReasons present in the StatusManager
func (s *StatusManager) getDegradedCondition() *wmcapi.WindowsMachineConfigCondition {
	// If there are no degradation reasons the WMC is not degraded
	if len(s.degradationReasons) == 0 {
		return wmcapi.NewWindowsMachineConfigCondition(wmcapi.Degraded, corev1.ConditionFalse, "", "")
	}

	// Add all reasons separated by a comma
	degradedReason := ""
	degradedMessage := ""
	for reason, message := range s.degradationReasons {
		degradedReason += reason + ","
		degradedMessage += reason + ": " + strings.Join(message, ",") + ","
	}
	degradedReason = strings.TrimSuffix(degradedReason, ",")
	degradedMessage = strings.TrimSuffix(degradedMessage, ",")

	return wmcapi.NewWindowsMachineConfigCondition(wmcapi.Degraded, corev1.ConditionTrue,
		degradedReason, degradedMessage)
}
