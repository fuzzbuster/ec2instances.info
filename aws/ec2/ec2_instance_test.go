package ec2

import (
	"github.com/fuzzbuster/ec2instances.info/utils"
	"testing"
)

func TestFormatClockSpeedFromMhz(t *testing.T) {
	tests := []struct {
		speedMhz uint
		want     string
	}{
		{2500, "2.5 GHz"},
		{2700, "2.7 GHz"},
		{3100, "3.1 GHz"},
	}
	for _, tc := range tests {
		if got := formatClockSpeedFromMhz(tc.speedMhz); got != tc.want {
			t.Errorf("formatClockSpeedFromMhz(%d) = %q, want %q", tc.speedMhz, got, tc.want)
		}
	}
}

func TestAddExtraDetailsSetsClockSpeedFromMeasuredData(t *testing.T) {
	instance := &EC2Instance{InstanceType: "m6g.medium"}
	instance.addExtraDetails()

	if instance.ClockSpeedGhz == nil {
		t.Fatal("ClockSpeedGhz is nil, want 2.5 GHz from measured extras data")
	}
	if *instance.ClockSpeedGhz != "2.5 GHz" {
		t.Errorf("ClockSpeedGhz = %q, want %q", *instance.ClockSpeedGhz, "2.5 GHz")
	}
}

func TestEnrichEc2InstancePrefersAwsClockSpeedOverExtras(t *testing.T) {
	instance := &EC2Instance{InstanceType: "m6g.medium"}
	instance.addExtraDetails()

	ec2ApiResponses := utils.NewSlowBuildingMap[string, *APIInstanceTypeInfo](func(pushChunk func(map[string]*APIInstanceTypeInfo)) error {
		return nil
	})
	enrichEc2Instance(instance, map[string]string{
		"instanceFamily": "General purpose",
		"vcpu":           "1",
		"memory":         "4 GiB",
		"clockSpeed":     "2.6 GHz",
		"ecu":            "Variable",
	}, ec2ApiResponses)

	if instance.ClockSpeedGhz == nil {
		t.Fatal("ClockSpeedGhz is nil")
	}
	if *instance.ClockSpeedGhz != "2.6 GHz" {
		t.Errorf("ClockSpeedGhz = %q, want AWS pricing value %q", *instance.ClockSpeedGhz, "2.6 GHz")
	}
}

func TestEnrichEc2InstanceMapsAPIFields(t *testing.T) {
	cores := int32(2)
	maxENIs := int32(3)
	ipsPerENI := int32(10)
	baselineNetwork := 1.25
	peakNetwork := 12.5
	baselineBandwidth := int32(650)
	baselineIOPS := int32(3600)
	baselineThroughput := 81.25
	maxBandwidth := int32(10000)
	maxIOPS := int32(40000)
	maxThroughput := 1250.0
	diskCount := int32(1)
	diskSize := int64(118)
	fpgaCount := int32(2)
	bareMetal := false

	apiInfo := &APIInstanceTypeInfo{
		InstanceType:         "m7i.large",
		BareMetal:            &bareMetal,
		Hypervisor:           "nitro",
		NitroEnclavesSupport: "supported",
		ProcessorInfo: &APIProcessorInfo{
			SupportedArchitectures: []string{"x86_64"},
		},
		VCpuInfo: &APIVCpuInfo{DefaultCores: &cores},
		NetworkInfo: &APINetworkInfo{
			NetworkPerformance:        stringPtr("Up to 12.5 Gigabit"),
			EnaSupport:                "required",
			MaximumNetworkInterfaces:  &maxENIs,
			Ipv4AddressesPerInterface: &ipsPerENI,
			NetworkCards: []APINetworkCardInfo{{
				BaselineBandwidthInGbps: &baselineNetwork,
				PeakBandwidthInGbps:     &peakNetwork,
			}},
		},
		EbsInfo: &APIEbsInfo{
			EbsOptimizedInfo: &APIEbsOptimizedInfo{
				BaselineBandwidthInMbps:  &baselineBandwidth,
				BaselineIops:             &baselineIOPS,
				BaselineThroughputInMBps: &baselineThroughput,
				MaximumBandwidthInMbps:   &maxBandwidth,
				MaximumIops:              &maxIOPS,
				MaximumThroughputInMBps:  &maxThroughput,
			},
		},
		InstanceStorageInfo: &APIInstanceStorageInfo{
			NvmeSupport: "required",
			Disks: []APIDiskInfo{{
				Count:    &diskCount,
				SizeInGB: &diskSize,
				Type:     "ssd",
			}},
		},
		FpgaInfo: &APIFpgaInfo{
			Fpgas: []APIFpgaDeviceInfo{{Count: &fpgaCount}},
		},
	}
	responses := utils.NewSlowBuildingMap[string, *APIInstanceTypeInfo](func(pushChunk func(map[string]*APIInstanceTypeInfo)) error {
		pushChunk(map[string]*APIInstanceTypeInfo{"m7i.large": apiInfo})
		return nil
	})
	instance := &EC2Instance{InstanceType: "m7i.large"}

	err := enrichEc2Instance(instance, baseEnrichmentAttributes(), responses)
	if err != nil {
		t.Fatal(err)
	}
	if len(instance.Arch) != 1 || instance.Arch[0] != "x86_64" {
		t.Fatalf("Arch = %v", instance.Arch)
	}
	if instance.Cores == nil || *instance.Cores != 2 {
		t.Fatalf("Cores = %v", instance.Cores)
	}
	if instance.VPC == nil || instance.VPC.MaxENIs != 3 || instance.VPC.IPsPerENI != 10 {
		t.Fatalf("VPC = %#v", instance.VPC)
	}
	if instance.BaselineBandwidth == nil || *instance.BaselineBandwidth != 1.25 ||
		instance.BurstBandwidth == nil || *instance.BurstBandwidth != 12.5 {
		t.Fatalf("network bandwidth = %v/%v", instance.BaselineBandwidth, instance.BurstBandwidth)
	}
	if !instance.EBSOptimized || instance.EBSIOPS != 40000 || instance.EBSMaxBandwidth != 10000 {
		t.Fatalf("EBS fields = %#v", instance)
	}
	if instance.Storage == nil || instance.Storage.Devices != 1 || instance.Storage.Size != 118 || !instance.Storage.NVMeSSD {
		t.Fatalf("Storage = %#v", instance.Storage)
	}
	if instance.FPGA != 2 {
		t.Fatalf("FPGA = %d", instance.FPGA)
	}
	if instance.NitroSupport == nil || !*instance.NitroSupport ||
		instance.NitroEnclaveSupport == nil || !*instance.NitroEnclaveSupport {
		t.Fatalf("Nitro fields = %v/%v", instance.NitroSupport, instance.NitroEnclaveSupport)
	}
}

func TestEnrichEc2InstanceAllowsMissingOptionalAPIFields(t *testing.T) {
	responses := utils.NewSlowBuildingMap[string, *APIInstanceTypeInfo](func(pushChunk func(map[string]*APIInstanceTypeInfo)) error {
		pushChunk(map[string]*APIInstanceTypeInfo{
			"m7i.large": {InstanceType: "m7i.large"},
		})
		return nil
	})
	instance := &EC2Instance{InstanceType: "m7i.large"}

	if err := enrichEc2Instance(instance, baseEnrichmentAttributes(), responses); err != nil {
		t.Fatal(err)
	}
	if instance.VPC != nil || instance.Storage != nil || instance.Cores != nil {
		t.Fatalf("unexpected optional fields: VPC=%#v Storage=%#v Cores=%#v", instance.VPC, instance.Storage, instance.Cores)
	}
}

func baseEnrichmentAttributes() map[string]string {
	return map[string]string{
		"instanceFamily":        "General purpose",
		"vcpu":                  "2",
		"memory":                "8 GiB",
		"physicalProcessor":     "Intel Xeon",
		"currentGeneration":     "Yes",
		"ecu":                   "Variable",
		"processorFeatures":     "",
		"processorArchitecture": "64-bit",
	}
}

func stringPtr(value string) *string {
	return &value
}
