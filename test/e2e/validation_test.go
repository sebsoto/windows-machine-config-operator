package e2e

import (
	"context"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"strings"
	"testing"

	nc "github.com/openshift/windows-machine-config-operator/pkg/controller/windowsmachine/nodeconfig"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
	corev1 "k8s.io/api/core/v1"
	kubeTypes "k8s.io/apimachinery/pkg/types"
)

// getInstanceID gets the instanceID of VM for a given cloud provider ID
// Ex: aws:///us-east-1e/i-078285fdadccb2eaa. We always want the last entry which is the instanceID
func getInstanceID(providerID string) string {
	providerTokens := strings.Split(providerID, "/")
	return providerTokens[len(providerTokens)-1]
}

// getInstanceIDsOfNodes returns the instanceIDs of all the Windows nodes created
func (tc *testContext) getInstanceIDsOfNodes() ([]string, error) {
	instanceIDs := make([]string, 0, len(gc.nodes))
	for _, node := range gc.nodes {
		if len(node.Spec.ProviderID) > 0 {
			instanceID := getInstanceID(node.Spec.ProviderID)
			instanceIDs = append(instanceIDs, instanceID)
		}
	}
	return instanceIDs, nil
}

// testWorkerLabel tests if the worker label has been applied properly
func testWorkerLabel(t *testing.T) {
	testCtx := framework.NewTestCtx(t)
	defer testCtx.Cleanup()
	for _, node := range gc.nodes {
		assert.Contains(t, node.Labels, nc.WorkerLabel, "expected node label %s was not present on %s", nc.WorkerLabel, node.GetName())
	}
}

// testVersionAnnotation tests all nodes are annotated with the version of the currently deployed WMCO
func testVersionAnnotation(t *testing.T) {
	for _, node := range gc.nodes {
		require.Containsf(t, node.Annotations, nc.VersionAnnotation, "node %s missing version annotation", node.GetName())
		assert.Equal(t, gc.operatorVersion, node.Annotations[nc.VersionAnnotation], "WMCO version annotation mismatch")
	}
	t.Run("test that version reverts after change", testVersionRevert)
}

// testVersionRevert tests that the version annotation on nodes will be reverted to the proper value if changed
func testVersionRevert(t *testing.T) {
	tc, err := NewTestContext(t)
	require.NoError(t, err, "could not create new test context")

	// Change nodes to incorrect version
	for _, node := range gc.nodes {
		changedNode := node
		changedNode.Annotations[nc.VersionAnnotation] = "notExpected"
		_, err = tc.kubeclient.CoreV1().Nodes().Update(context.TODO(), &changedNode, metav1.UpdateOptions{})
		require.NoError(t, err, "error updating node objects")
	}

	// repopulate gc.nodes with new values
	nodes, err := tc.kubeclient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{LabelSelector: nc.WindowsOSLabel})
	require.NoError(t, err, "error listing Windows nodes")
	require.Lenf(t, nodes.Items, int(gc.numberOfNodes), "Expected num of Windows nodes does not match: %v", nodes.Items)
	gc.nodes = nodes.Items
	for _, node := range gc.nodes {
		require.Containsf(t, node.Annotations, nc.VersionAnnotation, "node %s missing version annotation", node.GetName())
		assert.Equal(t, gc.operatorVersion, node.Annotations[nc.VersionAnnotation], "WMCO version annotation mismatch")
	}

}

// testNodeTaint tests if the Windows node has the Windows taint
func testNodeTaint(t *testing.T) {
	// windowsTaint is the taint that needs to be applied to the Windows node
	windowsTaint := corev1.Taint{
		Key:    "os",
		Value:  "Windows",
		Effect: corev1.TaintEffectNoSchedule,
	}

	for _, node := range gc.nodes {
		hasTaint := func() bool {
			for _, taint := range node.Spec.Taints {
				if taint.Key == windowsTaint.Key && taint.Value == windowsTaint.Value && taint.Effect == windowsTaint.Effect {
					return true
				}
			}
			return false
		}()
		assert.Equal(t, hasTaint, true, "expected Windows Taint to be present on the Node: %s", node.GetName())
	}
}

// createSigner creates a signer using the private key retrieved from the secret
func createSigner() (ssh.Signer, error) {
	privateKeySecret := &corev1.Secret{}
	err := framework.Global.Client.Get(context.TODO(), kubeTypes.NamespacedName{Name: "cloud-private-key", Namespace: "windows-machine-config-operator"}, privateKeySecret)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to retrieve cloud private key secret")
	}

	privateKeyBytes := privateKeySecret.Data["private-key.pem"][:]
	if privateKeyBytes == nil {
		return nil, errors.New("failed to retrieve private key using cloud private key secret")
	}

	signer, err := ssh.ParsePrivateKey(privateKeyBytes)
	if err != nil {
		return nil, errors.Wrapf(err, "unable to parse private key")
	}
	return signer, nil
}

// testUserData tests if the userData created in the 'openshift-machine-api' namespace is valid
func testUserData(t *testing.T) {
	signer, err := createSigner()
	require.NoError(t, err, "error creating signer using private key")
	pubKeyBytes := ssh.MarshalAuthorizedKey(signer.PublicKey())
	require.NotNil(t, pubKeyBytes, "failed to retrieve public key using signer for private key")
	found := &corev1.Secret{}
	err = framework.Global.Client.Get(context.TODO(), kubeTypes.NamespacedName{Name: "windows-user-data", Namespace: "openshift-machine-api"}, found)
	require.NoError(t, err, "could not find windows user data secret in required namespace")
	assert.Contains(t, string(found.Data["userData"][:]), string(pubKeyBytes[:]), "expected user data not present in required namespace")
}
