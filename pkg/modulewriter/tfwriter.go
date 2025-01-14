/**
* Copyright 2022 Google LLC
*
* Licensed under the Apache License, Version 2.0 (the "License");
* you may not use this file except in compliance with the License.
* You may obtain a copy of the License at
*
*      http://www.apache.org/licenses/LICENSE-2.0
*
* Unless required by applicable law or agreed to in writing, software
* distributed under the License is distributed on an "AS IS" BASIS,
* WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
* See the License for the specific language governing permissions and
* limitations under the License.
 */

package modulewriter

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"

	"github.com/hashicorp/hcl/v2/ext/typeexpr"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/zclconf/go-cty/cty"
	"golang.org/x/exp/slices"

	"hpc-toolkit/pkg/config"
	"hpc-toolkit/pkg/modulereader"
)

const (
	tfStateFileName       = "terraform.tfstate"
	tfStateBackupFileName = "terraform.tfstate.backup"
)

// TFWriter writes terraform to the blueprint folder
type TFWriter struct {
	numModules int
}

// GetNumModules getter for module count of kind terraform
func (w *TFWriter) getNumModules() int {
	return w.numModules
}

// AddNumModules add value to module count
func (w *TFWriter) addNumModules(value int) {
	w.numModules += value
}

// createBaseFile creates a baseline file for all terraform/hcl including a
// license and any other boilerplate
func createBaseFile(path string) error {
	baseFile, err := os.Create(path)
	if err != nil {
		return err
	}
	defer baseFile.Close()
	_, err = baseFile.WriteString(license)
	return err
}

func appendHCLToFile(path string, hclBytes []byte) error {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err = file.Write(hclBytes); err != nil {
		return err
	}
	return nil
}

func writeOutputs(
	modules []config.Module,
	dst string,
) error {
	// Create file
	outputsPath := filepath.Join(dst, "outputs.tf")
	if err := createBaseFile(outputsPath); err != nil {
		return fmt.Errorf("error creating outputs.tf file: %v", err)
	}

	// Create hcl body
	hclFile := hclwrite.NewEmptyFile()
	hclBody := hclFile.Body()

	// Add all outputs from each module
	for _, mod := range modules {
		for _, output := range mod.Outputs {
			// Create output block
			outputName := config.AutomaticOutputName(output.Name, mod.ID)
			hclBody.AppendNewline()
			hclBlock := hclBody.AppendNewBlock("output", []string{outputName})
			blockBody := hclBlock.Body()

			// Add attributes (description, value)
			desc := output.Description
			if desc == "" {
				desc = fmt.Sprintf("Generated output from module '%s'", mod.ID)
			}
			blockBody.SetAttributeValue("description", cty.StringVal(desc))
			value := fmt.Sprintf("module.%s.%s", mod.ID, output.Name)
			blockBody.SetAttributeRaw("value", simpleTokens(value))
			if output.Sensitive {
				blockBody.SetAttributeValue("sensitive", cty.BoolVal(output.Sensitive))
			}
		}
	}

	// Write file
	hclBytes := hclFile.Bytes()
	hclBytes = escapeLiteralVariables(hclBytes)
	hclBytes = escapeBlueprintVariables(hclBytes)
	err := appendHCLToFile(outputsPath, hclBytes)
	if err != nil {
		return fmt.Errorf("error writing HCL to outputs.tf file: %v", err)
	}
	return nil
}

func writeTfvars(vars map[string]cty.Value, dst string) error {
	// Create file
	tfvarsPath := filepath.Join(dst, "terraform.tfvars")
	err := writeHclAttributes(vars, tfvarsPath)
	return err
}

func getHclType(t cty.Type) string {
	if t.IsPrimitiveType() {
		return typeexpr.TypeString(t)
	}
	if t.IsListType() || t.IsTupleType() || t.IsSetType() {
		return "list"
	}
	return typeexpr.TypeString(cty.DynamicPseudoType) // any
}

func getTypeTokens(v cty.Value) hclwrite.Tokens {
	return simpleTokens(getHclType(v.Type()))
}

func writeVariables(vars map[string]cty.Value, extraVars []modulereader.VarInfo, dst string) error {
	// Create file
	variablesPath := filepath.Join(dst, "variables.tf")
	if err := createBaseFile(variablesPath); err != nil {
		return fmt.Errorf("error creating variables.tf file: %v", err)
	}

	var inputs []modulereader.VarInfo
	for k, v := range vars {
		typeStr := getHclType(v.Type())
		newInput := modulereader.VarInfo{
			Name:        k,
			Type:        typeStr,
			Description: fmt.Sprintf("Toolkit deployment variable: %s", k),
		}
		inputs = append(inputs, newInput)
	}
	inputs = append(inputs, extraVars...)
	slices.SortFunc(inputs, func(i, j modulereader.VarInfo) bool { return i.Name < j.Name })

	// Create HCL Body
	hclFile := hclwrite.NewEmptyFile()
	hclBody := hclFile.Body()

	// create variable block for each input
	for _, k := range inputs {
		hclBody.AppendNewline()
		hclBlock := hclBody.AppendNewBlock("variable", []string{k.Name})
		blockBody := hclBlock.Body()
		blockBody.SetAttributeValue("description", cty.StringVal(k.Description))
		blockBody.SetAttributeRaw("type", simpleTokens(k.Type))
	}

	// Write file
	if err := appendHCLToFile(variablesPath, hclFile.Bytes()); err != nil {
		return fmt.Errorf("error writing HCL to variables.tf file: %v", err)
	}
	return nil
}

func writeMain(
	modules []config.Module,
	tfBackend config.TerraformBackend,
	dst string,
) error {
	// Create file
	mainPath := filepath.Join(dst, "main.tf")
	if err := createBaseFile(mainPath); err != nil {
		return fmt.Errorf("error creating main.tf file: %v", err)
	}

	// Create HCL Body
	hclFile := hclwrite.NewEmptyFile()
	hclBody := hclFile.Body()

	// Write Terraform backend if needed
	if tfBackend.Type != "" {
		tfBody := hclBody.AppendNewBlock("terraform", []string{}).Body()
		backendBlock := tfBody.AppendNewBlock("backend", []string{tfBackend.Type})
		backendBody := backendBlock.Body()
		vals := tfBackend.Configuration.Items()
		for _, setting := range orderKeys(vals) {
			backendBody.SetAttributeValue(setting, vals[setting])
		}
		hclBody.AppendNewline()
	}

	for _, mod := range modules {
		// Convert settings to cty.Value
		ctySettings, err := config.ConvertMapToCty(mod.Settings)
		if err != nil {
			return fmt.Errorf(
				"error converting setting in module %s to cty when writing main.tf: %v",
				mod.ID, err)
		}

		// Add block
		moduleBlock := hclBody.AppendNewBlock("module", []string{mod.ID})
		moduleBody := moduleBlock.Body()

		// Add source attribute
		moduleBody.SetAttributeValue("source", cty.StringVal(mod.DeploymentSource))

		// For each Setting
		for _, setting := range orderKeys(ctySettings) {
			value := ctySettings[setting]
			if wrap, ok := mod.WrapSettingsWith[setting]; ok {
				if len(wrap) != 2 {
					return fmt.Errorf(
						"invalid length of WrapSettingsWith for %s.%s, expected 2 got %d",
						mod.ID, setting, len(wrap))
				}
				toks, err := tokensForWrapped(wrap[0], value, wrap[1])
				if err != nil {
					return fmt.Errorf("failed to process %s.%s: %v", mod.ID, setting, err)
				}
				moduleBody.SetAttributeRaw(setting, toks)
			} else {
				moduleBody.SetAttributeRaw(setting, TokensForValue(value))
			}
		}
		hclBody.AppendNewline()
	}
	// Write file
	hclBytes := hclFile.Bytes()
	hclBytes = escapeLiteralVariables(hclBytes)
	hclBytes = escapeBlueprintVariables(hclBytes)
	hclBytes = hclwrite.Format(hclBytes)
	if err := appendHCLToFile(mainPath, hclBytes); err != nil {
		return fmt.Errorf("error writing HCL to main.tf file: %v", err)
	}
	return nil
}

func tokensForWrapped(pref string, val cty.Value, suf string) (hclwrite.Tokens, error) {
	var toks hclwrite.Tokens
	if !val.Type().IsListType() && !val.Type().IsTupleType() {
		return toks, fmt.Errorf(
			"invalid value for wrapped setting, expected sequence, got %#v", val.Type())
	}
	toks = append(toks, simpleTokens(pref)...)

	it, first := val.ElementIterator(), true
	for it.Next() {
		if !first {
			toks = append(toks, &hclwrite.Token{
				Type:  hclsyntax.TokenComma,
				Bytes: []byte{','}})
		}
		_, el := it.Element()
		toks = append(toks, TokensForValue(el)...)
		first = false
	}
	toks = append(toks, simpleTokens(suf)...)

	return toks, nil
}

var simpleTokens = hclwrite.TokensForIdentifier

func writeProviders(vars map[string]cty.Value, dst string) error {
	// Create file
	providersPath := filepath.Join(dst, "providers.tf")
	if err := createBaseFile(providersPath); err != nil {
		return fmt.Errorf("error creating providers.tf file: %v", err)
	}

	// Create HCL Body
	hclFile := hclwrite.NewEmptyFile()
	hclBody := hclFile.Body()

	for _, prov := range []string{"google", "google-beta"} {
		provBlock := hclBody.AppendNewBlock("provider", []string{prov})
		provBody := provBlock.Body()
		if _, ok := vars["project_id"]; ok {
			provBody.SetAttributeRaw("project", simpleTokens("var.project_id"))
		}
		if _, ok := vars["zone"]; ok {
			provBody.SetAttributeRaw("zone", simpleTokens("var.zone"))
		}
		if _, ok := vars["region"]; ok {
			provBody.SetAttributeRaw("region", simpleTokens("var.region"))
		}
		hclBody.AppendNewline()
	}

	// Write file
	hclBytes := hclFile.Bytes()
	hclBytes = escapeLiteralVariables(hclBytes)
	hclBytes = escapeBlueprintVariables(hclBytes)
	if err := appendHCLToFile(providersPath, hclBytes); err != nil {
		return fmt.Errorf("error writing HCL to providers.tf file: %v", err)
	}
	return nil
}

func writeVersions(dst string) error {
	// Create file
	versionsPath := filepath.Join(dst, "versions.tf")
	if err := createBaseFile(versionsPath); err != nil {
		return fmt.Errorf("error creating versions.tf file: %v", err)
	}
	// Write hard-coded version information
	if err := appendHCLToFile(versionsPath, []byte(tfversions)); err != nil {
		return fmt.Errorf("error writing HCL to versions.tf file: %v", err)
	}
	return nil
}

func printTerraformInstructions(grpPath string, moduleName string, printIntergroupWarning bool) {
	printInstructionsPreamble("Terraform", grpPath, moduleName)
	if printIntergroupWarning {
		fmt.Print(intergroupWarning)
	}
	fmt.Printf("  terraform -chdir=%s init\n", grpPath)
	fmt.Printf("  terraform -chdir=%s validate\n", grpPath)
	fmt.Printf("  terraform -chdir=%s apply\n\n", grpPath)
}

// writeDeploymentGroup creates and sets up the provided terraform deployment
// group in the provided deployment directory
// depGroup: The deployment group that is being written
// globalVars: The top-level variables, needed for writing terraform.tfvars and
// variables.tf
// groupDir: The path to the directory the resource group will be created in
func (w TFWriter) writeDeploymentGroup(
	dc config.DeploymentConfig,
	groupIndex int,
	deploymentDir string,
) (groupMetadata, error) {
	depGroup := dc.Config.DeploymentGroups[groupIndex]
	deploymentVars := filterVarsByGraph(dc.Config.Vars.Items(), depGroup, dc.GetModuleConnections())
	intergroupVars := findIntergroupVariables(depGroup, dc.GetModuleConnections())
	intergroupInputs := make(map[string]bool)
	for _, igVar := range intergroupVars {
		intergroupInputs[igVar.Name] = true
	}
	gmd := groupMetadata{
		Name:             depGroup.Name,
		DeploymentInputs: orderKeys(deploymentVars),
		IntergroupInputs: orderKeys(intergroupInputs),
		Outputs:          getAllOutputs(depGroup),
	}

	writePath := filepath.Join(deploymentDir, depGroup.Name)

	// Write main.tf file
	if err := writeMain(
		depGroup.Modules, depGroup.TerraformBackend, writePath,
	); err != nil {
		return groupMetadata{}, fmt.Errorf("error writing main.tf file for deployment group %s: %v",
			depGroup.Name, err)
	}

	// Write variables.tf file
	if err := writeVariables(deploymentVars, intergroupVars, writePath); err != nil {
		return groupMetadata{}, fmt.Errorf(
			"error writing variables.tf file for deployment group %s: %v",
			depGroup.Name, err)
	}

	// Write outputs.tf file
	if err := writeOutputs(depGroup.Modules, writePath); err != nil {
		return groupMetadata{}, fmt.Errorf(
			"error writing outputs.tf file for deployment group %s: %v",
			depGroup.Name, err)
	}

	// Write terraform.tfvars file
	if err := writeTfvars(deploymentVars, writePath); err != nil {
		return groupMetadata{}, fmt.Errorf(
			"error writing terraform.tfvars file for deployment group %s: %v",
			depGroup.Name, err)
	}

	// Write providers.tf file
	if err := writeProviders(deploymentVars, writePath); err != nil {
		return groupMetadata{}, fmt.Errorf(
			"error writing providers.tf file for deployment group %s: %v",
			depGroup.Name, err)
	}

	// Write versions.tf file
	if err := writeVersions(writePath); err != nil {
		return groupMetadata{}, fmt.Errorf(
			"error writing versions.tf file for deployment group %s: %v",
			depGroup.Name, err)
	}

	printTerraformInstructions(writePath, depGroup.Name, len(intergroupInputs) > 0)

	return gmd, nil
}

// Transfers state files from previous resource groups (in .ghpc/) to a newly written blueprint
func (w TFWriter) restoreState(deploymentDir string) error {
	prevDeploymentGroupPath := filepath.Join(
		deploymentDir, hiddenGhpcDirName, prevDeploymentGroupDirName)
	files, err := ioutil.ReadDir(prevDeploymentGroupPath)
	if err != nil {
		return fmt.Errorf(
			"Error trying to read previous modules in %s, %w",
			prevDeploymentGroupPath, err)
	}

	for _, f := range files {
		var tfStateFiles = []string{tfStateFileName, tfStateBackupFileName}
		for _, stateFile := range tfStateFiles {
			src := filepath.Join(prevDeploymentGroupPath, f.Name(), stateFile)
			dest := filepath.Join(deploymentDir, f.Name(), stateFile)

			if bytesRead, err := ioutil.ReadFile(src); err == nil {
				err = ioutil.WriteFile(dest, bytesRead, 0644)
				if err != nil {
					return fmt.Errorf("failed to write previous state file %s, %w", dest, err)
				}
			}
		}

	}
	return nil
}

func orderKeys[T any](settings map[string]T) []string {
	keys := make([]string, 0, len(settings))
	for k := range settings {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func filterVarsByGraph(vars map[string]cty.Value, group config.DeploymentGroup, graph map[string][]config.ModConnection) map[string]cty.Value {
	// labels must always be written as a variable as it is implicitly added
	groupInputs := map[string]bool{
		"labels": true,
	}
	for _, mod := range group.Modules {
		if connections, ok := graph[mod.ID]; ok {
			for _, conn := range connections {
				if conn.IsDeploymentKind() {
					for _, v := range conn.GetSharedVariables() {
						groupInputs[v] = true
					}
				}
			}
		}
	}

	filteredVars := make(map[string]cty.Value)
	for key, val := range vars {
		if groupInputs[key] {
			filteredVars[key] = val
		}
	}
	return filteredVars
}

func getAllOutputs(group config.DeploymentGroup) []string {
	outputs := make(map[string]bool)
	for _, mod := range group.Modules {
		for _, output := range mod.Outputs {
			outputs[config.AutomaticOutputName(output.Name, mod.ID)] = true
		}
	}
	return orderKeys(outputs)
}
