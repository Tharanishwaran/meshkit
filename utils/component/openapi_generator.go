package component

import (
	"encoding/json"
	"fmt"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	cueJson "cuelang.org/go/encoding/json"
	"github.com/layer5io/meshkit/utils"
	"github.com/layer5io/meshkit/utils/manifests"

	"gopkg.in/yaml.v3"

	"github.com/meshery/schemas/models/v1beta1"
	"github.com/meshery/schemas/models/v1beta1/category"
	"github.com/meshery/schemas/models/v1beta1/component"
	"github.com/meshery/schemas/models/v1beta1/model"
)

func GenerateFromOpenAPI(resource, sourceURL string) ([]component.ComponentDefinition, error) {
	if resource == "" {
		return nil, nil
	}
	resource, err := getResolvedManifest(resource)
	if err != nil {
		return nil, err
	}
	cuectx := cuecontext.New()
	cueParsedManExpr, err := cueJson.Extract("", []byte(resource))
	parsedManifest := cuectx.BuildExpr(cueParsedManExpr)
	definitions := parsedManifest.LookupPath(cue.ParsePath("components.schemas"))

	if err != nil {
		return nil, err
	}

	fields, err := definitions.Fields()
	if err != nil {
		fmt.Printf("%v\n", err)
		return nil, err
	}
	components := make([]component.ComponentDefinition, 0)

	for fields.Next() {
		fieldVal := fields.Value()
		kindCue := fieldVal.LookupPath(cue.ParsePath(`"x-kubernetes-group-version-kind"[0].kind`))
		if kindCue.Err() != nil {
			continue
		}
		kind, err := kindCue.String()
		kind = strings.ToLower(kind)
		if err != nil {
			fmt.Printf("%v", err)
			continue
		}

		crd, err := fieldVal.MarshalJSON()
		if err != nil {
			fmt.Printf("%v", err)
			continue
		}
		versionCue := fieldVal.LookupPath(cue.ParsePath(`"x-kubernetes-group-version-kind"[0].version`))
		groupCue := fieldVal.LookupPath(cue.ParsePath(`"x-kubernetes-group-version-kind"[0].group`))
		apiVersion, _ := versionCue.String()
		if g, _ := groupCue.String(); g != "" {
			apiVersion = g + "/" + apiVersion
		}
		modified := make(map[string]interface{}) //Remove the given fields which is either not required by End user (like status) or is prefilled by system (like apiVersion, kind and metadata)
		err = json.Unmarshal(crd, &modified)
		if err != nil {
			fmt.Printf("%v", err)
			continue
		}

		modifiedProps, err := UpdateProperties(fieldVal, cue.ParsePath("properties.spec"), apiVersion)
		if err == nil {
			modified = modifiedProps
		}

		DeleteFields(modified)
		crd, err = json.Marshal(modified)
		if err != nil {
			fmt.Printf("%v", err)
			continue
		}

		c := component.ComponentDefinition{
			SchemaVersion: v1beta1.ComponentSchemaVersion,
			Version:       "v1.0.0",

			Format: component.JSON,
			Component: component.Component{
				Kind:    kind,
				Version: apiVersion,
				Schema:  string(crd),
			},
			// Metadata:    compMetadata,
			DisplayName: manifests.FormatToReadableString(kind),
			Model: model.ModelDefinition{
				SchemaVersion: v1beta1.ModelSchemaVersion,
				Version:       "v1.0.0",

				Model: model.Model{
					Version: "version",
				},
				Name:        "kubernetes",
				DisplayName: "Kubernetes",
				Category: category.CategoryDefinition{
					Name: "Orchestration & Management",
				},
				Metadata: &model.ModelDefinition_Metadata{
					AdditionalProperties: map[string]interface{}{
						"source_uri": sourceURL,
					},
				},
			},
		}

		components = append(components, c)
	}
	return components, nil

}

func getResolvedManifest(manifest string) (string, error) {
	var m map[string]interface{}

	err := yaml.Unmarshal([]byte(manifest), &m)
	if err != nil {
		return "", utils.ErrDecodeYaml(err)
	}

	byt, err := json.Marshal(m)
	if err != nil {
		return "", utils.ErrMarshal(err)
	}

	cuectx := cuecontext.New()
	cueParsedManExpr, err := cueJson.Extract("", byt)
	parsedManifest := cuectx.BuildExpr(cueParsedManExpr)
	definitions := parsedManifest.LookupPath(cue.ParsePath("components.schemas"))
	if err != nil {
		return "", err
	}
	resol := manifests.ResolveOpenApiRefs{}
	cache := make(map[string][]byte)
	resolved, err := resol.ResolveReferences(byt, definitions, cache)
	if err != nil {
		return "", err
	}
	manifest = string(resolved)
	return manifest, nil
}
