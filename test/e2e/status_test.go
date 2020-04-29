package e2e

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/openshift/windows-machine-config-bootstrapper/tools/windows-node-installer/pkg/cloudprovider"
	operator "github.com/openshift/windows-machine-config-operator/pkg/apis/wmc/v1alpha1"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
)

// testStatusWhenSuccessful ensures that the status matches the expected state of the operator when the operator should
// work correctly
func testStatusWhenSuccessful(t *testing.T) {
	testCtx, err := NewTestContext(t)
	require.NoError(t, err)

	// get WMC custom resource
	wmc := &operator.WindowsMachineConfig{}
	err = framework.Global.Client.Get(context.TODO(), types.NamespacedName{Name: wmcCRName,
		Namespace: testCtx.namespace}, wmc)
	require.NoError(t, err, "Could not retrieve instance of WMC")

	assert.Equal(t, wmc.Spec.Replicas, wmc.Status.JoinedVMCount, "Num of nodes in status not equal to spec")

	degraded := operator.GetWindowsMachineConfigCondition(wmc.Status, operator.Degraded)
	require.NotNil(t, degraded)
	reconciling := operator.GetWindowsMachineConfigCondition(wmc.Status, operator.Reconciling)
	assert.Equal(t, corev1.ConditionFalse, degraded.Status, "Status shows operator degraded")
	assert.Equal(t, corev1.ConditionFalse, reconciling.Status, "Status shows operator reconciling")
}

// testFailureSuite contains tests which involve invoking a reconcile failure
func testFailureSuite(t *testing.T) {
	t.Run("VM provision failure", testVMProvisionFail)
	t.Run("VM configuration failure", testVMConfigurationFail)
}

// testWindowsNodeCreation tests the Windows node creation in the cluster
func testVMProvisionFail(t *testing.T) {
	testCtx, err := NewTestContext(t)
	require.NoError(t, err)
	// create WMC custom resource with key pair that will fail
	wmc, err := testCtx.createWMC(gc.numberOfNodes, "fakeKeyPair")
	require.NoError(t, err, "error creating wcmo custom resource")
	defer framework.Global.Client.Delete(context.TODO(), wmc)

	degradedCondition, err := testCtx.waitForDegradedCondition()
	require.NoError(t, err, "error waiting for degraded condition")

	assert.Contains(t, degradedCondition.Reason, operator.VMCreationFailureReason)
}

// newCloudInterface returns an interface to interact with cloud resources
func newCloudInterface() (cloudprovider.Cloud, error) {
	kc := os.Getenv("KUBECONFIG")
	if kc == "" {
		return nil, fmt.Errorf("KUBECONFIG env var not set")
	}
	creds := os.Getenv("AWS_SHARED_CREDENTIALS_FILE")
	if creds == "" {
		return nil, fmt.Errorf("AWS_SHARED_CREDENTIALS_FILE env var not set")
	}
	key := os.Getenv("KUBE_SSH_KEY_PATH")
	if key == "" {
		return nil, fmt.Errorf("KUBE_SSH_KEY_PATH")
	}
	tempDirPath, err := ioutil.TempDir(os.TempDir(), "wmco_test")
	if err != nil {
		return nil, errors.Wrap(err, "could not create temp directory")
	}

	return cloudprovider.CloudProviderFactory(kc, creds, credentialAccountID, tempDirPath, "", "",
		gc.sshKeyPair, key)
}

// testWindowsNodeCreation tests the Windows node creation in the cluster
func testVMConfigurationFail(t *testing.T) {
	testCtx, err := NewTestContext(t)
	require.NoError(t, err)
	// create WMC custom resource with key pair that will fail
	wmc, err := testCtx.createWMC(gc.numberOfNodes, gc.sshKeyPair)
	require.NoError(t, err, "error creating wcmo custom resource")
	defer framework.Global.Client.Delete(context.TODO(), wmc)

	// get node, indicates that inital bootstrap is complete. Kill VM, network setup will fail and configuration
	// degradation reason should occur
	err = testCtx.waitForWindowsNode(false)
	require.NoError(t, err, "error waiting for windows node")
	cloud, err := newCloudInterface()
	require.NoError(t, err, "could not create cloud interface")

	for _, node := range gc.nodes {
		providerId := node.Spec.ProviderID
		require.NotNil(t, providerId, "instance id unexpectedly nil")
		splitID := strings.Split(providerId, "/")
		require.Greaterf(t, len(splitID), 1, "provider ID %s was of unexpected format", providerId)
		idToDelete := splitID[len(splitID)-1]
		log.Printf("Deleting %s", idToDelete)
		err = cloud.DestroyWindowsVM(idToDelete)
		require.NoError(t, err, "error destroying Windows VM")
		log.Printf("Deleted %s", idToDelete)
	}

	degradedCondition, err := testCtx.waitForDegradedCondition()
	require.NoError(t, err, "error waiting for degraded condition")

	assert.Contains(t, degradedCondition.Reason, operator.VMConfigurationFailureReason)
}

func (tc *testContext) waitForDegradedCondition() (*operator.WindowsMachineConfigCondition, error) {
	var degraded *operator.WindowsMachineConfigCondition
	err := wait.Poll(nodeRetryInterval, time.Duration(math.Max(float64(gc.numberOfNodes), 1))*nodeCreationTime, func() (done bool, err error) {
		log.Printf("Waiting for degraded")
		wmc := &operator.WindowsMachineConfig{}
		err = framework.Global.Client.Get(context.TODO(), types.NamespacedName{Name: wmcCRName,
			Namespace: tc.namespace}, wmc)
		if err != nil {
			return true, errors.Wrap(err, "could not get WMC object")
		}
		log.Printf("Status %+v", wmc.Status)
		if degraded = operator.GetWindowsMachineConfigCondition(wmc.Status, operator.Degraded); degraded != nil && degraded.Status == corev1.ConditionTrue {
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		return nil, errors.Wrap(err, "could not get degraded condition")
	}
	if degraded == nil {
		return nil, fmt.Errorf("timed out waiting for degraded condition")
	}
	return degraded, nil
}
