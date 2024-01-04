package controllers

import (
	"testing"

	"github.com/k0sproject/dig"
	"gotest.tools/v3/assert"
	"sigs.k8s.io/yaml"
)

func TestMergeValues(t *testing.T) {
	oldData := `
  password: "foo"
  someField: "asdf"
  other: "text"
  `
	newData := `
  someField: "newstring"
  other: "text"
  `
	protect := []string{"password"}

	targetData := `
  password: "foo"
  someField: "newstring"
  other: "text"
  `

	mergedValues, err := MergeValues(oldData, newData, protect)
	if err != nil {
		t.Fail()
	}

	targetDataMap := dig.Mapping{}
	if err := yaml.Unmarshal([]byte(targetData), &targetDataMap); err != nil {
		t.Fail()
	}

	mergedDataMap := dig.Mapping{}
	if err := yaml.Unmarshal([]byte(mergedValues), &mergedDataMap); err != nil {
		t.Fail()
	}

	assert.DeepEqual(t, targetDataMap, mergedDataMap)

}
