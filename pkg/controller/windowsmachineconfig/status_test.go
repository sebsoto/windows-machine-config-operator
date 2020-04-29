package windowsmachineconfig

import (
	"strings"
	"testing"

	wmcapi "github.com/openshift/windows-machine-config-operator/pkg/apis/wmc/v1alpha1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestGetDegradedCondition(t *testing.T) {
	testIO := []struct {
		name              string
		reasons           map[string][]string
		expectedCondition wmcapi.WindowsMachineConfigCondition
	}{
		{
			name:              "No degraded reasons",
			reasons:           map[string][]string{},
			expectedCondition: *wmcapi.NewWindowsMachineConfigCondition(wmcapi.Degraded, corev1.ConditionFalse, "", ""),
		},
		{
			name:              "Single degraded reason",
			reasons:           map[string][]string{"reason1": {"message1"}},
			expectedCondition: *wmcapi.NewWindowsMachineConfigCondition(wmcapi.Degraded, corev1.ConditionTrue, "reason1", "reason1: message1"),
		},
		{
			name:              "Multiple degraded reasons",
			reasons:           map[string][]string{"reason1": {"message1"}, "reason2": {"message2a", "message2b"}},
			expectedCondition: *wmcapi.NewWindowsMachineConfigCondition(wmcapi.Degraded, corev1.ConditionTrue, "reason1,reason2", "reason1: message1, reason2: message2a, message2b"),
		},
	}
	for _, tt := range testIO {
		t.Run(tt.name, func(t *testing.T) {
			s := NewStatusManager(nil, types.NamespacedName{})
			s.degradationReasons = tt.reasons
			actual := *s.getDegradedCondition()

			assert.Equal(t, tt.expectedCondition.Status, actual.Status)

			expectedReasons := strings.Split(tt.expectedCondition.Reason, ",")
			actualReasons := strings.Split(actual.Reason, ",")
			assert.ElementsMatch(t, expectedReasons, actualReasons)

			expectedMessage := strings.Split(tt.expectedCondition.Message, ",")
			for i, msg := range expectedMessage {
				expectedMessage[i] = strings.TrimSpace(msg)
			}
			actualMessage := strings.Split(actual.Message, ",")
			for i, msg := range actualMessage {
				actualMessage[i] = strings.TrimSpace(msg)
			}
			assert.ElementsMatch(t, expectedMessage, actualMessage)
		})
	}

}
