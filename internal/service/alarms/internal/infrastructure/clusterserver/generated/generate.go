package generated

//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -config oapi-codegen.yaml ../../../../../cluster/api/openapi.yaml
//go:generate mockgen -source=client.gen.go -destination=mock_generated/mock_client.gen.go -package=mock_generated
