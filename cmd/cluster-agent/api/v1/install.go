// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-2020 Datadog, Inc.

package v1

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/DataDog/datadog-agent/pkg/util/cloudfoundry"
	"github.com/gorilla/mux"

	"github.com/DataDog/datadog-agent/pkg/clusteragent"
	"github.com/DataDog/datadog-agent/pkg/telemetry"
	as "github.com/DataDog/datadog-agent/pkg/util/kubernetes/apiserver"
	"github.com/DataDog/datadog-agent/pkg/util/log"
)

var (
	apiRequests = telemetry.NewCounterWithOpts("", "api_requests",
		[]string{"handler", "status"}, "Counter of requests made to the cluster agent API.",
		telemetry.Options{NoDoubleUnderscoreSep: true})
)

func incrementRequestMetric(handler string, status int) {
	apiRequests.Inc(handler, strconv.Itoa(status))
}

// Install registers v1 API endpoints
func Install(r *mux.Router, sc clusteragent.ServerContext) {
	r.HandleFunc("/tags/pod/{nodeName}/{ns}/{podName}", getPodMetadata).Methods("GET")
	r.HandleFunc("/tags/pod/{nodeName}", getPodMetadataForNode).Methods("GET")
	r.HandleFunc("/tags/pod", getAllMetadata).Methods("GET")
	r.HandleFunc("/tags/node/{nodeName}", getNodeMetadata).Methods("GET")
	r.HandleFunc("/tags/cf/apps", getAllCFAppsMetadata).Methods("GET")
	installClusterCheckEndpoints(r, sc)
	installEndpointsCheckEndpoints(r, sc)
}

// getCFAppMetadata is only used when the node agent hits the DCA for the list of cloudfoundry applications tags
// It return a list of tags for each application that can be directly used in the tagger
func getAllCFAppsMetadata(w http.ResponseWriter, r *http.Request) {
	bbsCache, err := cloudfoundry.GetGlobalBBSCache()
	if err != nil {
		log.Errorf("Could not retrieve BBS cache: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		apiRequests.Inc("getCFAppMetadata", strconv.Itoa(http.StatusInternalServerError))
		return
	}

	tags := bbsCache.ExtractTags()

	tagsBytes, err := json.Marshal(tags)
	if err != nil {
		log.Errorf("Could not process tags for CF applications: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		apiRequests.Inc(
			"getAllCFAppsMetadata",
			strconv.Itoa(http.StatusInternalServerError),
		)
		return
	}
	if len(tagsBytes) > 0 {
		w.WriteHeader(http.StatusOK)
		w.Write(tagsBytes)
		apiRequests.Inc(
			"getAllCFAppsMetadata",
			strconv.Itoa(http.StatusOK),
		)
		return
	}
}

// getNodeMetadata is only used when the node agent hits the DCA for the list of labels
func getNodeMetadata(w http.ResponseWriter, r *http.Request) {
	/*
		Input
			localhost:5001/api/v1/tags/node/localhost
		Outputs
			Status: 200
			Returns: []string
			Example: ["label1:value1", "label2:value2"]

			Status: 404
			Returns: string
			Example: 404 page not found

			Status: 500
			Returns: string
			Example: "no cached metadata found for the node localhost"
	*/

	vars := mux.Vars(r)
	var labelBytes []byte
	nodeName := vars["nodeName"]
	nodeLabels, err := as.GetNodeLabels(nodeName)
	if err != nil {
		log.Errorf("Could not retrieve the node labels of %s: %v", nodeName, err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		apiRequests.Inc(
			"getNodeMetadata",
			strconv.Itoa(http.StatusInternalServerError),
		)
		return
	}
	labelBytes, err = json.Marshal(nodeLabels)
	if err != nil {
		log.Errorf("Could not process the labels of the node %s from the informer's cache: %v", nodeName, err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		apiRequests.Inc(
			"getNodeMetadata",
			strconv.Itoa(http.StatusInternalServerError),
		)
		return
	}
	if len(labelBytes) > 0 {
		w.WriteHeader(http.StatusOK)
		w.Write(labelBytes)
		apiRequests.Inc(
			"getNodeMetadata",
			strconv.Itoa(http.StatusOK),
		)
		return
	}
	w.WriteHeader(http.StatusNotFound)
	apiRequests.Inc(
		"getNodeMetadata",
		strconv.Itoa(http.StatusNotFound),
	)
	w.Write([]byte(fmt.Sprintf("Could not find labels on the node: %s", nodeName)))
}

// getPodMetadata is only used when the node agent hits the DCA for the tags list.
// It returns a list of all the tags that can be directly used in the tagger of the agent.
func getPodMetadata(w http.ResponseWriter, r *http.Request) {
	/*
		Input
			localhost:5001/api/v1/metadata/localhost/default/my-nginx-5d69
		Outputs
			Status: 200
			Returns: []string
			Example: ["kube_service:my-nginx-service"]

			Status: 404
			Returns: string
			Example: 404 page not found

			Status: 500
			Returns: string
			Example: "no cached metadata found for the pod my-nginx-5d69 on the node localhost"
	*/

	vars := mux.Vars(r)
	var metaBytes []byte
	nodeName := vars["nodeName"]
	podName := vars["podName"]
	ns := vars["ns"]
	metaList, errMetaList := as.GetPodMetadataNames(nodeName, ns, podName)
	if errMetaList != nil {
		log.Errorf("Could not retrieve the metadata of: %s from the cache", podName)
		http.Error(w, errMetaList.Error(), http.StatusInternalServerError)
		apiRequests.Inc(
			"getPodMetadata",
			strconv.Itoa(http.StatusInternalServerError),
		)
		return
	}

	metaBytes, err := json.Marshal(metaList)
	if err != nil {
		log.Errorf("Could not process the list of services for: %s", podName)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		apiRequests.Inc(
			"getPodMetadata",
			strconv.Itoa(http.StatusInternalServerError),
		)
		return
	}
	if len(metaBytes) != 0 {
		w.WriteHeader(http.StatusOK)
		w.Write(metaBytes)
		apiRequests.Inc(
			"getPodMetadata",
			strconv.Itoa(http.StatusOK),
		)
		return
	}
	w.WriteHeader(http.StatusNotFound)
	apiRequests.Inc(
		"getPodMetadata",
		strconv.Itoa(http.StatusNotFound),
	)
	w.Write([]byte(fmt.Sprintf("Could not find associated metadata mapped to the pod: %s on node: %s", podName, nodeName)))
}

// getPodMetadataForNode has the same signature as getAllMetadata, but is only scoped on one node.
func getPodMetadataForNode(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	nodeName := vars["nodeName"]
	log.Tracef("Fetching metadata map on all pods of the node %s", nodeName)
	metaList, errNodes := as.GetMetadataMapBundleOnNode(nodeName)
	if errNodes != nil {
		log.Warnf("Could not collect the service map for %s, err: %v", nodeName, errNodes)
	}
	slcB, err := json.Marshal(metaList)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		apiRequests.Inc(
			"getPodMetadataForNode",
			strconv.Itoa(http.StatusInternalServerError),
		)
		return
	}

	if len(slcB) != 0 {
		w.WriteHeader(http.StatusOK)
		w.Write(slcB)
		apiRequests.Inc(
			"getPodMetadataForNode",
			strconv.Itoa(http.StatusOK),
		)
		return
	}
	w.WriteHeader(http.StatusNotFound)
	apiRequests.Inc(
		"getPodMetadata",
		strconv.Itoa(http.StatusNotFound),
	)
	return
}

// getAllMetadata is used by the svcmap command.
func getAllMetadata(w http.ResponseWriter, r *http.Request) {
	/*
		Input
			localhost:5001/api/v1/metadata
		Outputs
			Status: 200
			Returns: map[string][]string
			Example: ["Node1":["pod1":["svc1"],"pod2":["svc2"]],"Node2":["pod3":["svc1"]], "Error":"the key KubernetesMetadataMapping/Node3 not found in the cache"]

			Status: 404
			Returns: string
			Example: 404 page not found

			Status: 503
			Returns: map[string]string
			Example: "["Error":"could not collect the service map for all nodes: List services is not permitted at the cluster scope."]
	*/
	log.Trace("Computing metadata map on all nodes")
	cl, err := as.GetAPIClient()
	if err != nil {
		log.Errorf("Can't create client to query the API Server: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		apiRequests.Inc(
			"getAllMetadata",
			strconv.Itoa(http.StatusInternalServerError),
		)
		return
	}
	metaList, errAPIServer := as.GetMetadataMapBundleOnAllNodes(cl)
	// If we hit an error at this point, it is because we don't have access to the API server.
	if errAPIServer != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		log.Errorf("There was an error querying the nodes from the API: %s", errAPIServer.Error())
	} else {
		w.WriteHeader(http.StatusOK)
	}
	metaListBytes, err := json.Marshal(metaList)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		apiRequests.Inc(
			"getAllMetadata",
			strconv.Itoa(http.StatusInternalServerError),
		)
		return
	}
	if len(metaListBytes) != 0 {
		w.Write(metaListBytes)
		apiRequests.Inc(
			"getAllMetadata",
			strconv.Itoa(http.StatusOK),
		)
		return
	}
	w.WriteHeader(http.StatusNotFound)
	apiRequests.Inc(
		"getAllMetadata",
		strconv.Itoa(http.StatusNotFound),
	)
	return
}
