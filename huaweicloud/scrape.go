// Package huaweicloud scrapes Huawei Cloud ECS instance type (flavor) data.
//
// Data source strategy:
//
//  1. Static seed data for all well-known flavor families (s, c, m, x, pi, p series).
//     Spec data: https://support.huaweicloud.com/intl/en-us/productdesc-ecs/ecs_01_0014.html
//
//  2. When HUAWEICLOUD_ACCESS_KEY / HUAWEICLOUD_SECRET_KEY and region-scoped
//     project IDs are set, the ECS ListFlavors API is called per region.
//     API reference: https://support.huaweicloud.com/intl/en-us/api-ecs/en-us_topic_0020212656.html
//
// No credentials are required to produce output — static seed covers GA flavors.
package huaweicloud

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/fuzzbuster/ec2instances.info/utils"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	outputFilePath = "huaweicloud/instances.json"
)

// Huawei Cloud regions.
var huaweiRegions = []string{
	"cn-north-4",     // Beijing 4
	"cn-east-3",      // Shanghai 1
	"cn-south-1",     // Guangzhou
	"cn-east-2",      // East 2 (Shanghai 2)
	"cn-north-1",     // North 1 (Beijing 1)
	"cn-southwest-2", // Guizhou
	"ap-southeast-1", // Hong Kong
	"ap-southeast-2", // Bangkok
	"ap-southeast-3", // Singapore
	"na-mexico-1",    // Mexico
}

// ----- output instance struct -----

// HWInstance represents a Huawei Cloud ECS flavor.
type HWInstance struct {
	InstanceType   string   `json:"instance_type"`
	InstanceFamily string   `json:"instance_family"`
	PrettyName     string   `json:"pretty_name"`
	VCPU           int      `json:"vCPU"`
	Memory         float64  `json:"memory"` // GiB
	Arch           []string `json:"arch"`
	GPU            int      `json:"GPU"`
	GPUModel       string   `json:"GPU_model,omitempty"`
	// Pricing: region → os → {"ondemand": "price"}
	Pricing map[string]map[string]map[string]string `json:"pricing"`
	Regions []string                                `json:"regions"`
}

// ----- static seed data -----
// Source: https://support.huaweicloud.com/intl/en-us/productdesc-ecs/ecs_01_0014.html
type seed struct {
	InstanceType string
	VCPU         int
	MemGiB       float64
	Family       string
	PrettyName   string
	Arch         []string
	GPU          int
	GPUModel     string
}

var staticSeeds = []seed{
	// General Purpose s3 series
	{"s3.small.1", 1, 1, "s3", "General Purpose S3", []string{"x86_64"}, 0, ""},
	{"s3.medium.2", 1, 2, "s3", "General Purpose S3", []string{"x86_64"}, 0, ""},
	{"s3.large.2", 2, 4, "s3", "General Purpose S3", []string{"x86_64"}, 0, ""},
	{"s3.large.4", 2, 8, "s3", "General Purpose S3", []string{"x86_64"}, 0, ""},
	{"s3.xlarge.2", 4, 8, "s3", "General Purpose S3", []string{"x86_64"}, 0, ""},
	{"s3.xlarge.4", 4, 16, "s3", "General Purpose S3", []string{"x86_64"}, 0, ""},
	{"s3.2xlarge.2", 8, 16, "s3", "General Purpose S3", []string{"x86_64"}, 0, ""},
	{"s3.2xlarge.4", 8, 32, "s3", "General Purpose S3", []string{"x86_64"}, 0, ""},
	{"s3.4xlarge.2", 16, 32, "s3", "General Purpose S3", []string{"x86_64"}, 0, ""},
	{"s3.4xlarge.4", 16, 64, "s3", "General Purpose S3", []string{"x86_64"}, 0, ""},
	// General Purpose s6 series
	{"s6.small.1", 1, 1, "s6", "General Purpose S6", []string{"x86_64"}, 0, ""},
	{"s6.medium.2", 1, 2, "s6", "General Purpose S6", []string{"x86_64"}, 0, ""},
	{"s6.large.2", 2, 4, "s6", "General Purpose S6", []string{"x86_64"}, 0, ""},
	{"s6.large.4", 2, 8, "s6", "General Purpose S6", []string{"x86_64"}, 0, ""},
	{"s6.xlarge.2", 4, 8, "s6", "General Purpose S6", []string{"x86_64"}, 0, ""},
	{"s6.xlarge.4", 4, 16, "s6", "General Purpose S6", []string{"x86_64"}, 0, ""},
	{"s6.2xlarge.2", 8, 16, "s6", "General Purpose S6", []string{"x86_64"}, 0, ""},
	{"s6.2xlarge.4", 8, 32, "s6", "General Purpose S6", []string{"x86_64"}, 0, ""},
	{"s6.4xlarge.2", 16, 32, "s6", "General Purpose S6", []string{"x86_64"}, 0, ""},
	{"s6.4xlarge.4", 16, 64, "s6", "General Purpose S6", []string{"x86_64"}, 0, ""},
	// General Purpose Network-enhanced n3 series
	{"n3.large.2", 2, 4, "n3", "General Purpose Network-Enhanced N3", []string{"x86_64"}, 0, ""},
	{"n3.xlarge.2", 4, 8, "n3", "General Purpose Network-Enhanced N3", []string{"x86_64"}, 0, ""},
	{"n3.xlarge.4", 4, 16, "n3", "General Purpose Network-Enhanced N3", []string{"x86_64"}, 0, ""},
	{"n3.2xlarge.2", 8, 16, "n3", "General Purpose Network-Enhanced N3", []string{"x86_64"}, 0, ""},
	{"n3.2xlarge.4", 8, 32, "n3", "General Purpose Network-Enhanced N3", []string{"x86_64"}, 0, ""},
	{"n3.4xlarge.2", 16, 32, "n3", "General Purpose Network-Enhanced N3", []string{"x86_64"}, 0, ""},
	{"n3.4xlarge.4", 16, 64, "n3", "General Purpose Network-Enhanced N3", []string{"x86_64"}, 0, ""},
	{"n3.8xlarge.2", 32, 64, "n3", "General Purpose Network-Enhanced N3", []string{"x86_64"}, 0, ""},
	{"n3.8xlarge.4", 32, 128, "n3", "General Purpose Network-Enhanced N3", []string{"x86_64"}, 0, ""},
	// Compute Optimized c3 series
	{"c3.large.2", 2, 4, "c3", "Compute Optimized C3", []string{"x86_64"}, 0, ""},
	{"c3.xlarge.2", 4, 8, "c3", "Compute Optimized C3", []string{"x86_64"}, 0, ""},
	{"c3.xlarge.4", 4, 16, "c3", "Compute Optimized C3", []string{"x86_64"}, 0, ""},
	{"c3.2xlarge.2", 8, 16, "c3", "Compute Optimized C3", []string{"x86_64"}, 0, ""},
	{"c3.2xlarge.4", 8, 32, "c3", "Compute Optimized C3", []string{"x86_64"}, 0, ""},
	{"c3.4xlarge.2", 16, 32, "c3", "Compute Optimized C3", []string{"x86_64"}, 0, ""},
	{"c3.4xlarge.4", 16, 64, "c3", "Compute Optimized C3", []string{"x86_64"}, 0, ""},
	{"c3.8xlarge.2", 32, 64, "c3", "Compute Optimized C3", []string{"x86_64"}, 0, ""},
	// Memory Optimized m3 series
	{"m3.large.8", 2, 16, "m3", "Memory Optimized M3", []string{"x86_64"}, 0, ""},
	{"m3.xlarge.8", 4, 32, "m3", "Memory Optimized M3", []string{"x86_64"}, 0, ""},
	{"m3.2xlarge.8", 8, 64, "m3", "Memory Optimized M3", []string{"x86_64"}, 0, ""},
	{"m3.4xlarge.8", 16, 128, "m3", "Memory Optimized M3", []string{"x86_64"}, 0, ""},
	{"m3.8xlarge.8", 32, 256, "m3", "Memory Optimized M3", []string{"x86_64"}, 0, ""},
	// Disk-intensive d3 series
	{"d3.xlarge.4", 4, 16, "d3", "Disk-Intensive D3", []string{"x86_64"}, 0, ""},
	{"d3.2xlarge.4", 8, 32, "d3", "Disk-Intensive D3", []string{"x86_64"}, 0, ""},
	{"d3.4xlarge.4", 16, 64, "d3", "Disk-Intensive D3", []string{"x86_64"}, 0, ""},
	{"d3.8xlarge.4", 32, 128, "d3", "Disk-Intensive D3", []string{"x86_64"}, 0, ""},
	// GPU pi2 series (NVIDIA V100)
	{"pi2.2xlarge.4", 8, 32, "pi2", "GPU P2 (NVIDIA V100)", []string{"x86_64"}, 1, "NVIDIA V100"},
	{"pi2.8xlarge.4", 32, 128, "pi2", "GPU P2 (NVIDIA V100)", []string{"x86_64"}, 4, "NVIDIA V100"},
	// GPU p2s series (NVIDIA V100 32GB)
	{"p2s.2xlarge.8", 8, 64, "p2s", "GPU P2S (NVIDIA V100 32G)", []string{"x86_64"}, 1, "NVIDIA V100 32GB"},
	{"p2s.8xlarge.8", 32, 256, "p2s", "GPU P2S (NVIDIA V100 32G)", []string{"x86_64"}, 4, "NVIDIA V100 32GB"},
	// Arm Kunpeng kc1 series
	{"kc1.large.2", 2, 4, "kc1", "General Purpose Kunpeng KC1", []string{"arm64"}, 0, ""},
	{"kc1.xlarge.2", 4, 8, "kc1", "General Purpose Kunpeng KC1", []string{"arm64"}, 0, ""},
	{"kc1.xlarge.4", 4, 16, "kc1", "General Purpose Kunpeng KC1", []string{"arm64"}, 0, ""},
	{"kc1.2xlarge.2", 8, 16, "kc1", "General Purpose Kunpeng KC1", []string{"arm64"}, 0, ""},
	{"kc1.2xlarge.4", 8, 32, "kc1", "General Purpose Kunpeng KC1", []string{"arm64"}, 0, ""},
	{"kc1.4xlarge.2", 16, 32, "kc1", "General Purpose Kunpeng KC1", []string{"arm64"}, 0, ""},
	{"kc1.4xlarge.4", 16, 64, "kc1", "General Purpose Kunpeng KC1", []string{"arm64"}, 0, ""},
	{"kc1.8xlarge.2", 32, 64, "kc1", "General Purpose Kunpeng KC1", []string{"arm64"}, 0, ""},
	{"kc1.8xlarge.4", 32, 128, "kc1", "General Purpose Kunpeng KC1", []string{"arm64"}, 0, ""},
	// Arm Kunpeng Compute-optimised km1
	{"km1.large.2", 2, 4, "km1", "Memory Optimized Kunpeng KM1", []string{"arm64"}, 0, ""},
	{"km1.xlarge.4", 4, 16, "km1", "Memory Optimized Kunpeng KM1", []string{"arm64"}, 0, ""},
	{"km1.2xlarge.8", 8, 64, "km1", "Memory Optimized Kunpeng KM1", []string{"arm64"}, 0, ""},
}

// ----- API structs -----

type hwFlavor struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	VCPU         string `json:"vcpus"` // string in HW API
	RAM          int    `json:"ram"`   // MiB
	OsExtraSpecs struct {
		EcsInstanceArchitecture string `json:"ecs:instance_architecture"`
		EcsPerformanceType      string `json:"ecs:performancetype"`
		EcsGenerations          string `json:"ecs:generations"`
		GPU                     string `json:"pci_passthrough:count"`
		GPUSpec                 string `json:"pci_passthrough:alias"`
	} `json:"os_extra_specs"`
}

type hwFlavorsResponse struct {
	Flavors []hwFlavor `json:"flavors"`
}

// ----- optional API fetch -----

func fetchSpecsFromAPI(ak, sk string, projectIDs map[string]string) map[string]*HWInstance {
	all := map[string]*HWInstance{}
	for _, region := range huaweiRegions {
		projectID := projectIDs[region]
		if projectID == "" {
			continue
		}
		flavors, err := listFlavors(ak, sk, projectID, region)
		if err != nil {
			log.Printf("[huaweicloud] WARN region %s: %v", region, err)
			continue
		}
		for _, f := range flavors {
			itype := f.ID
			inst, exists := all[itype]
			if !exists {
				vcpu := parseVCPU(f.VCPU)
				family := extractFamily(f.Name)
				arch := []string{"x86_64"}
				if strings.Contains(f.OsExtraSpecs.EcsInstanceArchitecture, "arm") {
					arch = []string{"arm64"}
				}
				inst = &HWInstance{
					InstanceType:   itype,
					InstanceFamily: family,
					PrettyName:     prettyName(family),
					VCPU:           vcpu,
					Memory:         float64(f.RAM) / 1024.0,
					Arch:           arch,
					Pricing:        map[string]map[string]map[string]string{},
				}
				all[itype] = inst
			}
			inst.Regions = utils.AppendUnique(inst.Regions, region)
		}
		log.Printf("[huaweicloud] region %s: %d flavors", region, len(flavors))
	}
	return all
}

func listFlavors(ak, sk, projectID, region string) ([]hwFlavor, error) {
	endpoint := fmt.Sprintf("https://ecs.%s.myhuaweicloud.com/v1/%s/cloudservers/flavors", region, projectID)

	timestamp := time.Now().UTC()
	authHeader, sdkDate, err := hwSignRequest(ak, sk, "GET", endpoint, "", region, timestamp)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 30 * time.Second}
	req, _ := http.NewRequest("GET", endpoint, nil)
	req.Header.Set("Authorization", authHeader)
	req.Header.Set("X-Sdk-Date", sdkDate)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d for %s", resp.StatusCode, endpoint)
	}

	var out hwFlavorsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Flavors, nil
}

// hwSignRequest implements Huawei Cloud AKSK authentication (HMAC-SHA256).
// Reference: https://support.huaweicloud.com/intl/en-us/api-ecs/ecs_03_0010.html
func hwSignRequest(ak, sk, method, endpoint, body, region string, t time.Time) (string, string, error) {
	datetime := t.Format("20060102T150405Z")
	date := t.Format("20060102")

	payloadHash := hashSHA256(body)
	canonicalHeaders := fmt.Sprintf("host:%s\nx-sdk-date:%s\n", extractHost(endpoint), datetime)
	signedHeaders := "host;x-sdk-date"

	canonicalRequest := strings.Join([]string{
		method, extractPath(endpoint), "",
		canonicalHeaders, signedHeaders, payloadHash,
	}, "\n")

	credentialScope := fmt.Sprintf("%s/%s/ecs/sdk_request", date, region)
	stringToSign := strings.Join([]string{
		"SDK-HMAC-SHA256", datetime, credentialScope, hashSHA256(canonicalRequest),
	}, "\n")

	signingKey := hmacSHA256Bytes(
		hmacSHA256Bytes(
			hmacSHA256Bytes(
				hmacSHA256Bytes([]byte(sk), date),
				region),
			"ecs"),
		"sdk_request")
	signature := hex.EncodeToString(hmacSHA256Bytes(signingKey, stringToSign))

	return fmt.Sprintf(
		"SDK-HMAC-SHA256 Access=%s, SignedHeaders=%s, Signature=%s",
		ak, signedHeaders, signature,
	), datetime, nil
}

func huaweiProjectIDEnv(region string) string {
	return "HUAWEICLOUD_PROJECT_ID_" + strings.ToUpper(strings.ReplaceAll(region, "-", "_"))
}

func collectHuaweiProjectIDs() map[string]string {
	projectIDs := map[string]string{}
	for _, region := range huaweiRegions {
		if projectID := os.Getenv(huaweiProjectIDEnv(region)); projectID != "" {
			projectIDs[region] = projectID
		}
	}

	projectID := os.Getenv("HUAWEICLOUD_PROJECT_ID")
	region := os.Getenv("HUAWEICLOUD_REGION")
	if projectID != "" && region != "" {
		projectIDs[region] = projectID
	}
	if projectID != "" && region == "" && len(projectIDs) == 0 {
		log.Println("[huaweicloud] HUAWEICLOUD_PROJECT_ID set without HUAWEICLOUD_REGION; skipping live API to avoid cross-region project reuse")
	}
	return projectIDs
}

func hashSHA256(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func hmacSHA256Bytes(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(data))
	return mac.Sum(nil)
}

func extractHost(endpoint string) string {
	endpoint = strings.TrimPrefix(endpoint, "https://")
	endpoint = strings.TrimPrefix(endpoint, "http://")
	if idx := strings.Index(endpoint, "/"); idx >= 0 {
		return endpoint[:idx]
	}
	return endpoint
}

func extractPath(endpoint string) string {
	endpoint = strings.TrimPrefix(endpoint, "https://")
	endpoint = strings.TrimPrefix(endpoint, "http://")
	if idx := strings.Index(endpoint, "/"); idx >= 0 {
		return endpoint[idx:]
	}
	return "/"
}

func extractFamily(name string) string {
	// Huawei flavor names: s6.xlarge.4, c3.large.2, kc1.2xlarge.8
	// Family = everything before the first dot
	if idx := strings.Index(name, "."); idx >= 0 {
		return name[:idx]
	}
	return name
}

func parseVCPU(s string) int {
	n := 0
	fmt.Sscanf(s, "%d", &n)
	return n
}

func prettyName(family string) string {
	f := strings.ToLower(family)
	switch {
	case strings.HasPrefix(f, "kc"):
		return "General Purpose Kunpeng " + strings.ToUpper(family)
	case strings.HasPrefix(f, "km"):
		return "Memory Optimized Kunpeng " + strings.ToUpper(family)
	case strings.HasPrefix(f, "pi"), strings.HasPrefix(f, "p2"):
		return "GPU " + strings.ToUpper(family)
	case strings.HasPrefix(f, "s"):
		return "General Purpose " + strings.ToUpper(family)
	case strings.HasPrefix(f, "n"):
		return "General Purpose Network-Enhanced " + strings.ToUpper(family)
	case strings.HasPrefix(f, "c"):
		return "Compute Optimized " + strings.ToUpper(family)
	case strings.HasPrefix(f, "m"):
		return "Memory Optimized " + strings.ToUpper(family)
	case strings.HasPrefix(f, "d"):
		return "Disk-Intensive " + strings.ToUpper(family)
	}
	return "Huawei Cloud ECS " + strings.ToUpper(family)
}

// ----- main entry point -----

// DoHuaweicloudScraping is called from main.go.
func DoHuaweicloudScraping() error {
	log.Println("[huaweicloud] starting scrape")

	all := map[string]*HWInstance{}

	// Static seeds first.
	for _, s := range staticSeeds {
		all[s.InstanceType] = &HWInstance{
			InstanceType:   s.InstanceType,
			InstanceFamily: s.Family,
			PrettyName:     s.PrettyName,
			VCPU:           s.VCPU,
			Memory:         s.MemGiB,
			Arch:           s.Arch,
			GPU:            s.GPU,
			GPUModel:       s.GPUModel,
			Pricing:        map[string]map[string]map[string]string{},
			Regions:        []string{"cn-north-4", "cn-east-3"},
		}
	}

	ak := os.Getenv("HUAWEICLOUD_ACCESS_KEY")
	sk := os.Getenv("HUAWEICLOUD_SECRET_KEY")
	projectIDs := collectHuaweiProjectIDs()
	if ak != "" && sk != "" && len(projectIDs) > 0 {
		log.Println("[huaweicloud] credentials found, fetching live data …")
		apiInstances := fetchSpecsFromAPI(ak, sk, projectIDs)
		for k, v := range apiInstances {
			all[k] = v
		}
	} else if ak != "" && sk != "" {
		log.Println("[huaweicloud] credentials found but no region-scoped project IDs configured — using static seed data only")
	} else {
		log.Println("[huaweicloud] no credentials — using static seed data only")
	}

	sorted := make([]*HWInstance, 0, len(all))
	for _, v := range all {
		sorted = append(sorted, v)
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].InstanceType < sorted[j].InstanceType
	})

	if err := utils.SaveInstances(sorted, outputFilePath); err != nil {
		return fmt.Errorf("save Huawei Cloud instances: %w", err)
	}
	log.Printf("[huaweicloud] wrote %d instances to %s", len(sorted), outputFilePath)
	return nil
}
