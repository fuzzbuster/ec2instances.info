package azure

import (
	"testing"

	"github.com/fuzzbuster/ec2instances.info/utils"
)

func TestSplitAzureOfferKey(t *testing.T) {
	instanceType, tier, ok := splitAzureOfferKey("linux-e16-4as-v4-standard")
	if !ok || instanceType != "e16-4as-v4" || tier != "standard" {
		t.Fatalf("splitAzureOfferKey() = %q, %q, %v", instanceType, tier, ok)
	}
}

func TestProcessSpecsDataWithoutManagementAPI(t *testing.T) {
	specs := utils.NewSlowBuildingMap[string, *AzureSpecsApiIteratorItem](
		func(func(map[string]*AzureSpecsApiIteratorItem)) error { return nil },
	)
	instances := map[string]*AzureInstance{}
	attributes := map[string]map[string]any{
		"linux-e16-4as-v4-standard": {
			"instanceName": "E16-4as v4",
			"series":       "easv4",
			"category":     "memoryoptimized",
			"cores":        float64(4),
			"ram":          float64(128),
			"diskSize":     float64(32),
		},
	}

	if err := processSpecsDataResult(instances, attributes, specs); err != nil {
		t.Fatal(err)
	}
	instance := instances["e16-4as-v4"]
	if instance == nil {
		t.Fatal("constrained-vCPU instance was not parsed")
	}
	if instance.Vcpu != 4 || instance.Memory != 128 || instance.Size != 32 {
		t.Fatalf("unexpected anonymous specification: %+v", instance)
	}
}
