package collector

import (
	"github.com/openshift-kni/oran-o2ims/internal/model"
	"github.com/openshift-kni/oran-o2ims/internal/service"
)

// getClusterGraphqlVars returns the graphql variables needed to query the managed clusters
func getClusterGraphqlVars() *model.SearchInput {
	input := model.SearchInput{}
	itemKind := service.KindCluster
	input.Filters = []*model.SearchFilter{
		{
			Property: "kind",
			Values:   []*string{&itemKind},
		},
	}

	return &input
}

// getNodeGraphqlVars returns the graphql variables needed to query the node instances
func getNodeGraphqlVars() *model.SearchInput {
	input := model.SearchInput{}
	kindNode := service.KindNode
	nonEmpty := "!"
	input.Filters = []*model.SearchFilter{
		{
			Property: "kind",
			Values: []*string{
				&kindNode,
			},
		},
		// Filter results without '_systemUUID' property (could happen with stale objects)
		{
			Property: "_systemUUID",
			Values:   []*string{&nonEmpty},
		},
	}
	return &input
}
