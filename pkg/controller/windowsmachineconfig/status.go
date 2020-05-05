package windowsmachineconfig

import (
	"context"
	"github.com/pkg/errors"
	"strings"

	wmcapi "github.com/openshift/windows-machine-config-operator/pkg/apis/wmc/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// StatusManager is in charge of updating the WindowsMachineConfig object status
type StatusManager struct {
	client          client.Client
	wmcName         types.NamespacedName
	joinedVMCount   int
	reconciling     bool
	conditionsToSet []wmcapi.WindowsMachineConfigCondition
}

// NewStatusManager returns a new instance of StatusManager
func NewStatusManager(client client.Client, name types.NamespacedName) *StatusManager {
	return &StatusManager{
		client:  client,
		wmcName: name,
	}
}

// updateStatus syncs the current status with the cluster and removes the reconciling status condition
func (s *StatusManager) updateStatus() error {
	object := &wmcapi.WindowsMachineConfig{}
	err := s.client.Get(context.TODO(), s.wmcName, object)
	if err != nil {
		return errors.Wrapf(err, "could not get %v", s.wmcName)
	}

	object.Status.JoinedVMCount = s.joinedVMCount
	for _, condition := range s.conditionsToSet {
		object.Status.SetWindowsMachineConfigCondition(condition)
	}
	if err = s.client.Status().Update(context.TODO(), object); err != nil {
		return errors.Wrapf(err, "could not update status to %v", object.Status)
	}
	return nil
}

// newDegradedCondition returns a degraded condition defined by the degradationReasons present in the StatusManager
func newDegradedCondition(reconcileErrs []*reconcileError) *wmcapi.WindowsMachineConfigCondition {
	// If there are no degradation reasons the WMC is not degraded
	if reconcileErrs == nil || len(reconcileErrs) == 0 {
		return wmcapi.NewWindowsMachineConfigCondition(wmcapi.Degraded, corev1.ConditionFalse, "", "")
	}

	// Add all reasons separated by a comma
	degradedReason := ""
	degradedMessage := ""
	for _, reconcileErr := range reconcileErrs {
		if reconcileErr == nil {
			continue
		}
		degradedMessage += reconcileErr.Error() + ","
		degradedReason += reconcileErr.reason + ","
	}
	degradedReason = strings.TrimSuffix(degradedReason, ",")
	degradedMessage = strings.TrimSuffix(degradedMessage, ",")

	return wmcapi.NewWindowsMachineConfigCondition(wmcapi.Degraded, corev1.ConditionTrue,
		degradedReason, degradedMessage)
}
