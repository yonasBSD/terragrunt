package config

import (
	"encoding/json"
	"fmt"

	"github.com/gruntwork-io/terragrunt/config/hclparse"
	"github.com/gruntwork-io/terragrunt/internal/errors"
	"github.com/gruntwork-io/terragrunt/internal/experiment"
	"github.com/gruntwork-io/terragrunt/options"
	"github.com/gruntwork-io/terragrunt/pkg/log"
	"github.com/gruntwork-io/terragrunt/util"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
	ctyjson "github.com/zclconf/go-cty/cty/json"
)

// ParsedVariable structure with input name, default value and description.
type ParsedVariable struct {
	Name                    string
	Description             string
	Type                    string
	DefaultValue            string
	DefaultValuePlaceholder string
}

// ParseVariables - parse variables from tf files.
func ParseVariables(l log.Logger, opts *options.TerragruntOptions, directoryPath string) ([]*ParsedVariable, error) {
	walkWithSymlinks := opts.Experiments.Evaluate(experiment.Symlinks)

	// list all tf files
	tfFiles, err := util.ListTfFiles(directoryPath, walkWithSymlinks)
	if err != nil {
		return nil, errors.New(err)
	}

	parser := hclparse.NewParser(DefaultParserOptions(l, opts)...)

	// iterate over files and parse variables.
	var parsedInputs []*ParsedVariable

	for _, tfFile := range tfFiles {
		if _, err := parser.ParseFromFile(tfFile); err != nil {
			return nil, err
		}
	}

	for _, file := range parser.Files() {
		ctx := &hcl.EvalContext{}

		if body, ok := file.Body.(*hclsyntax.Body); ok {
			for _, block := range body.Blocks {
				if block.Type == "variable" {
					if len(block.Labels[0]) > 0 {
						// extract variable attributes
						name := block.Labels[0]

						var descriptionAttrText string

						descriptionAttr, err := readBlockAttribute(ctx, block, "description")
						if err != nil {
							l.Warnf("Failed to read descriptionAttr for %s %v", name, err)

							descriptionAttr = nil
						}

						if descriptionAttr != nil {
							descriptionAttrText = descriptionAttr.AsString()
						} else {
							descriptionAttrText = fmt.Sprintf("(variable %s did not define a description)", name)
						}

						var typeAttrText string

						typeAttr, err := readBlockAttribute(ctx, block, "type")
						if err != nil {
							l.Warnf("Failed to read type attribute for %s %v", name, err)
						}

						if typeAttr != nil {
							typeAttrText = typeAttr.AsString()
						} else {
							typeAttrText = fmt.Sprintf("(variable %s does not define a type)", name)
						}

						defaultValue, err := readBlockAttribute(ctx, block, "default")
						if err != nil {
							l.Warnf("Failed to read default value for %s %v", name, err)

							defaultValue = nil
						}

						defaultValueText := ""

						if defaultValue != nil {
							jsonBytes, err := ctyjson.Marshal(*defaultValue, cty.DynamicPseudoType)
							if err != nil {
								return nil, errors.New(err)
							}

							var ctyJSONOutput ctyJSONValue
							if err := json.Unmarshal(jsonBytes, &ctyJSONOutput); err != nil {
								return nil, errors.New(err)
							}

							jsonBytes, err = json.Marshal(ctyJSONOutput.Value)
							if err != nil {
								return nil, errors.New(err)
							}

							defaultValueText = string(jsonBytes)
						}

						input := &ParsedVariable{
							Name:                    name,
							Type:                    typeAttrText,
							Description:             descriptionAttrText,
							DefaultValue:            defaultValueText,
							DefaultValuePlaceholder: generateDefaultValue(typeAttrText),
						}

						parsedInputs = append(parsedInputs, input)
					}
				}
			}
		}
	}

	return parsedInputs, nil
}

// generateDefaultValue - generate hcl default value
// HCL type of variable https://developer.hashicorp.com/packer/docs/templates/hcl_templates/variables#type-constraints
func generateDefaultValue(variableType string) string {
	switch variableType {
	case "number":
		return "0"
	case "bool":
		return "false"
	case "list":
		return "[]"
	case "map":
		return "{}"
	case "object":
		return "{}"
	}
	// fallback to empty value
	return "\"\""
}

type ctyJSONValue struct {
	Value any `json:"Value"`
	Type  any `json:"Type"`
}

// readBlockAttribute - hcl block attribute.
func readBlockAttribute(ctx *hcl.EvalContext, block *hclsyntax.Block, name string) (*cty.Value, error) {
	if attr, ok := block.Body.Attributes[name]; ok {
		if attr.Expr != nil {
			if call, ok := attr.Expr.(*hclsyntax.FunctionCallExpr); ok {
				result := cty.StringVal(call.Name)
				return &result, nil
			}
			// check if first var is traversal
			if len(attr.Expr.Variables()) > 0 {
				v := attr.Expr.Variables()[0]
				// check if variable is traversal
				if varTr, ok := v[0].(hcl.TraverseRoot); ok {
					result := cty.StringVal(varTr.Name)
					return &result, nil
				}
			}

			value, err := attr.Expr.Value(ctx)
			if err != nil {
				return nil, err
			}

			return &value, nil
		}
	}

	return nil, nil
}
