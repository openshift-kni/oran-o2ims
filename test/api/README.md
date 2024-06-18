# ORAN API TEST SUITE
## Introduction

This test suite needs of a functional Openshift deployment with ACM and ORAN operator, servers must be started too

```bash
oran-o2ims                                         Active   12d
oran-o2ims-system                                  Active   12d
rhacm                                              Active   12d
```

In this case *rhacm* is the ACM namespace, *oran-o2ims-system* is the operator namespace and *oran-o2ims* is where the servers are deployed

```bash
NAME                                         READY   STATUS    RESTARTS   AGE
deployment-manager-server-66d5b544f4-94p96   1/1     Running   0          12d
metadata-server-64b4597c94-9q4sk             1/1     Running   0          12d
resource-server-646bccdb87-65jwr             1/1     Running   0          12d
```

## Preparation

For running correctly the testsuite we need to set TEST_HOST env variable, this variable is the search-api URL that we can get using this command

```bash
 oc describe routes -n oran-o2ims | grep Host
 Requested Host:         o2ims.apps.ocp-mobius-cluster-assisted-0.qe.lab.redhat.com
```

Then we can use this
```bash
export TEST_HOST="o2ims.apps.ocp-mobius-cluster-assisted-0.qe.lab.redhat.com"
```

Or we can launch the test suite without setting with export this way

```bash
TEST_HOST="o2ims.apps.ocp-mobius-cluster-assisted-0.qe.lab.redhat.com" go run github.com/onsi/ginkgo/v2/ginkgo -v
```

## Execution and results

To run the test we have some ways to do it, for example, if we have used the export command we can run

```bash
ginkgo -v
```
**Note:** probably you get a ginkgo version mismatch, if you want to get rid of the message is better to launch with the repo version like this

```bash
go run github.com/onsi/ginkgo/v2/ginkgo -v
```

This gives a result like this

```bash
Running Suite: OranO2ims Suite - /home/sdelacru/repos/oran-o2ims
================================================================
Random Seed: 1717072815

Will run 2 of 7 specs
------------------------------
Metadata Server API testing When getting infrastructure Inventory API version should return OK in the response and json response should match reference json
/home/sdelacru/repos/oran-o2ims/oran_o2ims_suite_test.go:18
  STEP: Executing https petition @ 05/30/24 14:40:17.779
  STEP: Checking OK status response @ 05/30/24 14:40:18.091
  STEP: Checking response JSON is equal to expected JSON @ 05/30/24 14:40:18.092
• [0.313 seconds]
------------------------------
Metadata Server API testing When getting infrastructure Inventory description should return OK in the response and json response should match reference json
/home/sdelacru/repos/oran-o2ims/oran_o2ims_suite_test.go:40
  STEP: Executing https petition @ 05/30/24 14:40:18.092
  STEP: Checking OK status response @ 05/30/24 14:40:18.418
  STEP: Checking response JSON is equal to expected JSON @ 05/30/24 14:40:18.418
• [0.326 seconds]
------------------------------
SSSSS

Ran 2 of 7 Specs in 0.640 seconds
SUCCESS! -- 2 Passed | 0 Failed | 0 Pending | 5 Skipped
PASS

Ginkgo ran 1 suite in 3.296458911s
Test Suite Passed
```

## Advanced usage
We can run selected groups of tests, like this way

```bash
TEST_HOST="o2ims.apps.ocp-hub-0.qe.lab.redhat.com" go run github.com/onsi/ginkgo/v2/ginkgo --focus=Metadata -v
```
with *focus* parameter we can launch only a group of tests, in this case metadata server only test cases, we can do the same with the other servers, for example *--focus=Resources* will run only resource server tests


## TODO

-[ ] Check running servers in BeforeSuite
-[ ] Add labels to tag the indivual tests so we can run specific tests depending on test phase (integration, development, release...etc)
-[ ] Add more tests in next iteration
