package configfsm

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Timeout in seconds before timing out policies enforcement
const configTimeout = 5

func TestFSMTimedout(t *testing.T) {
	var aTestHelper BaseFSMHelper
	aTestHelper.Init(configTimeout)
	aFSM, err := InitFSM(Start)
	assert.Equal(t, nil, err, "err should be nil")

	_, err = RunFSM(context.Background(), aFSM, &aTestHelper)
	if err != nil {
		fmt.Println(err)
	}

	var state any
	state, err = aFSM.State(context.Background())
	assert.Equal(t, nil, err, "err should be nil")
	assert.Equal(t, "Missing", state, "State should be Missing")

	aTestHelper.PoliciesMatched = true

	_, err = RunFSM(context.Background(), aFSM, &aTestHelper)
	if err != nil {
		fmt.Println(err)
	}

	state, err = aFSM.State(context.Background())
	assert.Equal(t, nil, err, "err should be nil")
	assert.Equal(t, "ClusterNotReady", state, "State should be ClusterNotReady")

	aTestHelper.ClusterReady = true
	aTestHelper.NonCompliantPolicyInEnforce = true

	_, err = RunFSM(context.Background(), aFSM, &aTestHelper)
	if err != nil {
		fmt.Println(err)
	}

	state, err = aFSM.State(context.Background())
	assert.Equal(t, nil, err, "err should be nil")
	assert.Equal(t, "InProgress", state, "State should be InProgress")

	time.Sleep(6 * time.Second)

	_, err = RunFSM(context.Background(), aFSM, &aTestHelper)
	if err != nil {
		fmt.Println(err)
	}

	state, err = aFSM.State(context.Background())
	assert.Equal(t, nil, err, "err should be nil")
	assert.Equal(t, "TimedOut", state, "State should be TimedOut")

	aTestHelper.AllPoliciesCompliant = true

	_, err = RunFSM(context.Background(), aFSM, &aTestHelper)
	if err != nil {
		fmt.Println(err)
	}

	state, err = aFSM.State(context.Background())
	assert.Equal(t, nil, err, "err should be nil")
	assert.Equal(t, "Completed", state, "State should be Completed")
}

func TestFSMTimedoutClusterNotReady(t *testing.T) {
	var aTestHelper BaseFSMHelper
	aTestHelper.Init(configTimeout)
	aFSM, err := InitFSM(Start)
	assert.Equal(t, nil, err, "err should be nil")

	_, err = RunFSM(context.Background(), aFSM, &aTestHelper)
	if err != nil {
		fmt.Println(err)
	}

	var state any
	state, err = aFSM.State(context.Background())
	assert.Equal(t, nil, err, "err should be nil")
	assert.Equal(t, "Missing", state, "State should be Missing")

	aTestHelper.PoliciesMatched = true

	_, err = RunFSM(context.Background(), aFSM, &aTestHelper)
	if err != nil {
		fmt.Println(err)
	}

	state, err = aFSM.State(context.Background())
	assert.Equal(t, nil, err, "err should be nil")
	assert.Equal(t, "ClusterNotReady", state, "State should be ClusterNotReady")

	aTestHelper.ClusterReady = true
	aTestHelper.NonCompliantPolicyInEnforce = true

	_, err = RunFSM(context.Background(), aFSM, &aTestHelper)
	if err != nil {
		fmt.Println(err)
	}

	state, err = aFSM.State(context.Background())
	assert.Equal(t, nil, err, "err should be nil")
	assert.Equal(t, "InProgress", state, "State should be InProgress")

	time.Sleep(4 * time.Second)

	_, err = RunFSM(context.Background(), aFSM, &aTestHelper)
	if err != nil {
		fmt.Println(err)
	}

	state, err = aFSM.State(context.Background())
	assert.Equal(t, nil, err, "err should be nil")
	assert.Equal(t, "InProgress", state, "State should be InProgress")

	aTestHelper.ClusterReady = false

	_, err = RunFSM(context.Background(), aFSM, &aTestHelper)
	if err != nil {
		fmt.Println(err)
	}

	state, err = aFSM.State(context.Background())
	assert.Equal(t, nil, err, "err should be nil")
	assert.Equal(t, "ClusterNotReady", state, "State should be ClusterNotReady")

	time.Sleep(2 * time.Second)

	_, err = RunFSM(context.Background(), aFSM, &aTestHelper)
	if err != nil {
		fmt.Println(err)
	}

	state, err = aFSM.State(context.Background())
	assert.Equal(t, nil, err, "err should be nil")
	assert.Equal(t, "TimedOut", state, "State should be TimedOut")

	aTestHelper.AllPoliciesCompliant = true
	aTestHelper.ClusterReady = true

	_, err = RunFSM(context.Background(), aFSM, &aTestHelper)
	if err != nil {
		fmt.Println(err)
	}

	state, err = aFSM.State(context.Background())
	assert.Equal(t, nil, err, "err should be nil")
	assert.Equal(t, Completed, state, "State should be Completed")
}

func TestFSMCompleted(t *testing.T) {
	var aTestHelper BaseFSMHelper
	aTestHelper.Init(configTimeout)
	aFSM, err := InitFSM(Start)
	assert.Equal(t, nil, err, "err should be nil")

	_, err = RunFSM(context.Background(), aFSM, &aTestHelper)
	if err != nil {
		fmt.Println(err)
	}

	var state any
	state, err = aFSM.State(context.Background())
	assert.Equal(t, nil, err, "err should be nil")
	assert.Equal(t, "Missing", state, "State should be Missing")

	aTestHelper.PoliciesMatched = true

	_, err = RunFSM(context.Background(), aFSM, &aTestHelper)
	if err != nil {
		fmt.Println(err)
	}

	state, err = aFSM.State(context.Background())
	assert.Equal(t, nil, err, "err should be nil")
	assert.Equal(t, "ClusterNotReady", state, "State should be ClusterNotReady")

	aTestHelper.ClusterReady = true
	aTestHelper.NonCompliantPolicyInEnforce = true

	_, err = RunFSM(context.Background(), aFSM, &aTestHelper)
	if err != nil {
		fmt.Println(err)
	}

	state, err = aFSM.State(context.Background())
	assert.Equal(t, nil, err, "err should be nil")
	assert.Equal(t, "InProgress", state, "State should be InProgress")

	aTestHelper.AllPoliciesCompliant = true

	_, err = RunFSM(context.Background(), aFSM, &aTestHelper)
	if err != nil {
		fmt.Println(err)
	}

	state, err = aFSM.State(context.Background())
	assert.Equal(t, nil, err, "err should be nil")
	assert.Equal(t, "Completed", state, "State should be Completed")

	aTestHelper.AllPoliciesCompliant = false

	_, err = RunFSM(context.Background(), aFSM, &aTestHelper)
	if err != nil {
		fmt.Println(err)
	}

	state, err = aFSM.State(context.Background())
	assert.Equal(t, nil, err, "err should be nil")
	assert.Equal(t, "InProgress", state, "State should be InProgress")
}

func TestFSMCompletedAllCompliant(t *testing.T) {
	var aTestHelper BaseFSMHelper
	aTestHelper.Init(configTimeout)
	aFSM, err := InitFSM(Start)
	assert.Equal(t, nil, err, "err should be nil")

	_, err = RunFSM(context.Background(), aFSM, &aTestHelper)
	if err != nil {
		fmt.Println(err)
	}

	var state any
	state, err = aFSM.State(context.Background())
	assert.Equal(t, nil, err, "err should be nil")
	assert.Equal(t, "Missing", state, "State should be Missing")

	aTestHelper.PoliciesMatched = true

	_, err = RunFSM(context.Background(), aFSM, &aTestHelper)
	if err != nil {
		fmt.Println(err)
	}

	state, err = aFSM.State(context.Background())
	assert.Equal(t, nil, err, "err should be nil")
	assert.Equal(t, "ClusterNotReady", state, "State should be ClusterNotReady")

	aTestHelper.ClusterReady = true
	aTestHelper.AllPoliciesCompliant = true

	_, err = RunFSM(context.Background(), aFSM, &aTestHelper)
	if err != nil {
		fmt.Println(err)
	}

	state, err = aFSM.State(context.Background())
	assert.Equal(t, nil, err, "err should be nil")
	assert.Equal(t, "Completed", state, "State should be Completed")
}

func TestFSMTNoPolicies(t *testing.T) {
	var aTestHelper BaseFSMHelper
	aTestHelper.Init(configTimeout)
	aFSM, err := InitFSM(Start)
	assert.Equal(t, nil, err, "err should be nil")

	_, err = RunFSM(context.Background(), aFSM, &aTestHelper)
	if err != nil {
		fmt.Println(err)
	}

	var state any
	state, err = aFSM.State(context.Background())
	assert.Equal(t, nil, err, "err should be nil")
	assert.Equal(t, "Missing", state, "State should be Missing")

	aTestHelper.PoliciesMatched = true

	_, err = RunFSM(context.Background(), aFSM, &aTestHelper)
	if err != nil {
		fmt.Println(err)
	}

	state, err = aFSM.State(context.Background())
	assert.Equal(t, nil, err, "err should be nil")
	assert.Equal(t, "ClusterNotReady", state, "State should be ClusterNotReady")

	aTestHelper.ClusterReady = true
	aTestHelper.NonCompliantPolicyInEnforce = true

	_, err = RunFSM(context.Background(), aFSM, &aTestHelper)
	if err != nil {
		fmt.Println(err)
	}

	state, err = aFSM.State(context.Background())
	assert.Equal(t, nil, err, "err should be nil")
	assert.Equal(t, "InProgress", state, "State should be InProgress")

	time.Sleep(6 * time.Second)

	_, err = RunFSM(context.Background(), aFSM, &aTestHelper)
	if err != nil {
		fmt.Println(err)
	}

	state, err = aFSM.State(context.Background())
	assert.Equal(t, nil, err, "err should be nil")
	assert.Equal(t, "TimedOut", state, "State should be TimedOut")

	aTestHelper.PoliciesMatched = false

	_, err = RunFSM(context.Background(), aFSM, &aTestHelper)
	if err != nil {
		fmt.Println(err)
	}

	state, err = aFSM.State(context.Background())
	assert.Equal(t, nil, err, "err should be nil")
	assert.Equal(t, "Missing", state, "State should be Missing")
}

func TestFSMOutOfDate(t *testing.T) {
	var aTestHelper BaseFSMHelper
	aTestHelper.Init(configTimeout)
	aFSM, err := InitFSM(Start)
	assert.Equal(t, nil, err, "err should be nil")

	_, err = RunFSM(context.Background(), aFSM, &aTestHelper)
	if err != nil {
		fmt.Println(err)
	}

	var state any
	state, err = aFSM.State(context.Background())
	assert.Equal(t, nil, err, "err should be nil")
	assert.Equal(t, "Missing", state, "State should be Missing")

	aTestHelper.PoliciesMatched = true

	_, err = RunFSM(context.Background(), aFSM, &aTestHelper)
	if err != nil {
		fmt.Println(err)
	}

	state, err = aFSM.State(context.Background())
	assert.Equal(t, nil, err, "err should be nil")
	assert.Equal(t, "ClusterNotReady", state, "State should be ClusterNotReady")

	aTestHelper.ClusterReady = true
	aTestHelper.NonCompliantPolicyInEnforce = false
	aTestHelper.AllPoliciesCompliant = false

	_, err = RunFSM(context.Background(), aFSM, &aTestHelper)
	if err != nil {
		fmt.Println(err)
	}

	state, err = aFSM.State(context.Background())
	assert.Equal(t, nil, err, "err should be nil")
	assert.Equal(t, OutOfDate, state, "State should be OutOfDate")

}
func TestFSMTimedoutOutOfDate(t *testing.T) {
	var aTestHelper BaseFSMHelper
	aTestHelper.Init(configTimeout)
	aFSM, err := InitFSM(Start)
	assert.Equal(t, nil, err, "err should be nil")

	_, err = RunFSM(context.Background(), aFSM, &aTestHelper)
	if err != nil {
		fmt.Println(err)
	}

	var state any
	state, err = aFSM.State(context.Background())
	assert.Equal(t, nil, err, "err should be nil")
	assert.Equal(t, "Missing", state, "State should be Missing")

	aTestHelper.PoliciesMatched = true

	_, err = RunFSM(context.Background(), aFSM, &aTestHelper)
	if err != nil {
		fmt.Println(err)
	}

	state, err = aFSM.State(context.Background())
	assert.Equal(t, nil, err, "err should be nil")
	assert.Equal(t, "ClusterNotReady", state, "State should be ClusterNotReady")

	aTestHelper.ClusterReady = true
	aTestHelper.NonCompliantPolicyInEnforce = true

	_, err = RunFSM(context.Background(), aFSM, &aTestHelper)
	if err != nil {
		fmt.Println(err)
	}

	state, err = aFSM.State(context.Background())
	assert.Equal(t, nil, err, "err should be nil")
	assert.Equal(t, "InProgress", state, "State should be InProgress")

	time.Sleep(6 * time.Second)

	_, err = RunFSM(context.Background(), aFSM, &aTestHelper)
	if err != nil {
		fmt.Println(err)
	}

	state, err = aFSM.State(context.Background())
	assert.Equal(t, nil, err, "err should be nil")
	assert.Equal(t, "TimedOut", state, "State should be TimedOut")

	aTestHelper.ClusterReady = true
	aTestHelper.NonCompliantPolicyInEnforce = false
	aTestHelper.AllPoliciesCompliant = false

	_, err = RunFSM(context.Background(), aFSM, &aTestHelper)
	if err != nil {
		fmt.Println(err)
	}

	state, err = aFSM.State(context.Background())
	assert.Equal(t, nil, err, "err should be nil")
	assert.Equal(t, OutOfDate, state, "State should be OutOfDate")
}

func TestDisplayGraph(t *testing.T) {
	const docBase = `# Configuration applied state machine
The State diagram of the configuration state machine is generated below in dot format.
 A Link to the rendered diagram is at: [link](https://dreampuf.github.io/GraphvizOnline/?presentation#%s)
` + "```" + `
%s
` + "```" + `
For details on what happens when entering each states and guards before transitioning check: [fsm.go](internal/configfsm/fsm.go)`
	aFSM, err := InitFSM(Start)
	assert.Equal(t, nil, err, "err should be nil")
	err = os.WriteFile("../../docs/dev/config_fsm.md", []byte(fmt.Sprintf(docBase, url.PathEscape(aFSM.ToGraph()), aFSM.ToGraph())), 0o600)
	assert.Equal(t, nil, err, "err should be nil")
}

func (h *BaseFSMHelper) Init(configTimeout int) {
	h.ConfigTimeout = configTimeout
	h.CurrentState = Start
}

func (h *BaseFSMHelper) IsTimedOut() bool {
	if h.IsResetNonCompliantAt() {
		return false
	}
	return time.Since(h.NonCompliantAt) > time.Duration(h.ConfigTimeout)*time.Second
}
