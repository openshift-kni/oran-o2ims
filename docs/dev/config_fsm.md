# Configuration applied state machine
The State diagram of the configuration state machine is generated below in dot format.
 A Link to the rendered diagram is at: [link](https://dreampuf.github.io/GraphvizOnline/?presentation#digraph%20%7B%0A%09compound=true%3B%0A%09node%20%5Bshape=Mrecord%5D%3B%0A%09rankdir=%22LR%22%3B%0A%0A%09ClusterNotReady%20%5Blabel=%22ClusterNotReady%7Centry%20%2F%20func2%22%5D%3B%0A%09Completed%20%5Blabel=%22Completed%7Centry%20%2F%20func5%22%5D%3B%0A%09InProgress%20%5Blabel=%22InProgress%7Centry%20%2F%20func3%22%5D%3B%0A%09Missing%20%5Blabel=%22Missing%7Centry%20%2F%20func1%22%5D%3B%0A%09OutOfDate%20%5Blabel=%22OutOfDate%7Centry%20%2F%20func4%22%5D%3B%0A%09Start%20%5Blabel=%22Start%22%5D%3B%0A%09TimedOut%20%5Blabel=%22TimedOut%7Centry%20%2F%20func6%22%5D%3B%0A%09ClusterNotReady%20-%3E%20ClusterNotReady%20%5Blabel=%22ClusterNotReady-&gt%3BClusterNotReady%22%5D%3B%0A%09ClusterNotReady%20-%3E%20InProgress%20%5Blabel=%22ClusterNotReady-&gt%3BInProgress%22%5D%3B%0A%09ClusterNotReady%20-%3E%20Missing%20%5Blabel=%22ClusterNotReady-&gt%3BMissing%22%5D%3B%0A%09ClusterNotReady%20-%3E%20TimedOut%20%5Blabel=%22ClusterNotReady-&gt%3BTimedOut%22%5D%3B%0A%09Completed%20-%3E%20Completed%20%5Blabel=%22Completed-&gt%3BCompleted%22%5D%3B%0A%09Completed%20-%3E%20InProgress%20%5Blabel=%22Completed-&gt%3BInProgress%22%5D%3B%0A%09InProgress%20-%3E%20ClusterNotReady%20%5Blabel=%22InProgress-&gt%3BClusterNotReady%22%5D%3B%0A%09InProgress%20-%3E%20Completed%20%5Blabel=%22InProgress-&gt%3BCompleted%22%5D%3B%0A%09InProgress%20-%3E%20InProgress%20%5Blabel=%22InProgress-&gt%3BInProgress%22%5D%3B%0A%09InProgress%20-%3E%20OutOfDate%20%5Blabel=%22InProgress-&gt%3BOutOfDate%22%5D%3B%0A%09InProgress%20-%3E%20TimedOut%20%5Blabel=%22InProgress-&gt%3BTimedOut%22%5D%3B%0A%09Missing%20-%3E%20ClusterNotReady%20%5Blabel=%22Missing-&gt%3BClusterNotReady%22%5D%3B%0A%09Missing%20-%3E%20Missing%20%5Blabel=%22Missing-&gt%3BMissing%22%5D%3B%0A%09OutOfDate%20-%3E%20InProgress%20%5Blabel=%22OutOfDate-&gt%3BInProgress%22%5D%3B%0A%09OutOfDate%20-%3E%20OutOfDate%20%5Blabel=%22OutOfDate-&gt%3BOutOfDate%22%5D%3B%0A%09Start%20-%3E%20Missing%20%5Blabel=%22Start-&gt%3BMissing%22%5D%3B%0A%09TimedOut%20-%3E%20InProgress%20%5Blabel=%22TimedOut-&gt%3BInProgress%22%5D%3B%0A%09TimedOut%20-%3E%20TimedOut%20%5Blabel=%22TimedOut-&gt%3BTimedOut%22%5D%3B%0A%09init%20%5Blabel=%22%22%2C%20shape=point%5D%3B%0A%09init%20-%3E%20Start%0A%7D%0A)
```
digraph {
	compound=true;
	node [shape=Mrecord];
	rankdir="LR";

	ClusterNotReady [label="ClusterNotReady|entry / func2"];
	Completed [label="Completed|entry / func5"];
	InProgress [label="InProgress|entry / func3"];
	Missing [label="Missing|entry / func1"];
	OutOfDate [label="OutOfDate|entry / func4"];
	Start [label="Start"];
	TimedOut [label="TimedOut|entry / func6"];
	ClusterNotReady -> ClusterNotReady [label="ClusterNotReady-&gt;ClusterNotReady"];
	ClusterNotReady -> InProgress [label="ClusterNotReady-&gt;InProgress"];
	ClusterNotReady -> Missing [label="ClusterNotReady-&gt;Missing"];
	ClusterNotReady -> TimedOut [label="ClusterNotReady-&gt;TimedOut"];
	Completed -> Completed [label="Completed-&gt;Completed"];
	Completed -> InProgress [label="Completed-&gt;InProgress"];
	InProgress -> ClusterNotReady [label="InProgress-&gt;ClusterNotReady"];
	InProgress -> Completed [label="InProgress-&gt;Completed"];
	InProgress -> InProgress [label="InProgress-&gt;InProgress"];
	InProgress -> OutOfDate [label="InProgress-&gt;OutOfDate"];
	InProgress -> TimedOut [label="InProgress-&gt;TimedOut"];
	Missing -> ClusterNotReady [label="Missing-&gt;ClusterNotReady"];
	Missing -> Missing [label="Missing-&gt;Missing"];
	OutOfDate -> InProgress [label="OutOfDate-&gt;InProgress"];
	OutOfDate -> OutOfDate [label="OutOfDate-&gt;OutOfDate"];
	Start -> Missing [label="Start-&gt;Missing"];
	TimedOut -> InProgress [label="TimedOut-&gt;InProgress"];
	TimedOut -> TimedOut [label="TimedOut-&gt;TimedOut"];
	init [label="", shape=point];
	init -> Start
}

```
For details on what happens when entering each states and guards before transitioning check: [fsm.go](internal/configfsm/fsm.go)