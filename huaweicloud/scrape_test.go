package huaweicloud

import "testing"

func TestMergePortalVM(t *testing.T) {
	oneYear := 1
	instances := map[string]*HWInstance{}
	vm := portalVM{
		CPU:             "8BSSUNIT.pluralUnit.23",
		Memory:          "32BSSUNIT.pluralUnit.102",
		InstanceArch:    "x86",
		AcceleratorCard: "2 * NVIDIA T4 / 2 * 16G",
		LocalDisk:       "2*1.8T NVMe SSD",
		Spec:            "g6.2xlarge.4",
		ImageSpec:       "linux",
		PlanList: []portalPlan{
			{BillingMode: "ONDEMAND", Amount: 4.25},
			{BillingMode: "MONTHLY", PeriodNum: &oneYear, Amount: 2040},
			{BillingMode: "YEARLY", PeriodNum: &oneYear, Amount: 20400},
		},
	}

	mergePortalVM(instances, "cn-north-4", vm)
	mergePortalVM(instances, "cn-east-3", vm)

	instance := instances["g6.2xlarge.4"]
	if instance == nil {
		t.Fatal("instance was not added")
	}
	if instance.VCPU != 8 || instance.Memory != 32 {
		t.Fatalf("unexpected spec: vCPU=%d memory=%v", instance.VCPU, instance.Memory)
	}
	if instance.GPU != 2 || instance.GPUModel != "NVIDIA T4" {
		t.Fatalf("unexpected GPU: count=%d model=%q", instance.GPU, instance.GPUModel)
	}
	if instance.LocalStorage != "2*1.8T NVMe SSD" {
		t.Fatalf("unexpected local storage %q", instance.LocalStorage)
	}
	if len(instance.Regions) != 2 {
		t.Fatalf("unexpected regions %v", instance.Regions)
	}
	prices := instance.Pricing["cn-north-4"]["linux"]
	if prices["ondemand"] != "4.25" || prices["monthly"] != "2040" || prices["yearly_1"] != "20400" {
		t.Fatalf("unexpected prices %v", prices)
	}
}

func TestMergePortalVMIgnoresWindows(t *testing.T) {
	instances := map[string]*HWInstance{}
	mergePortalVM(instances, "cn-north-4", portalVM{
		Spec:      "s7.large.2",
		ImageSpec: "win",
	})
	if len(instances) != 0 {
		t.Fatalf("expected Windows SKU to be ignored, got %v", instances)
	}
}
