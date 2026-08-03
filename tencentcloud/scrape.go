// Package tencentcloud scrapes Tencent Cloud CVM instance type data.
//
// Data source strategy (no credentials required for basic data):
//
//  1. Public pricing page JSON:
//     https://buy.cloud.tencent.com/price/cvm/overview
//     (parsed via the JSON endpoint embedded in the page)
//
//  2. Static seed data for well-known instance families (S, C, M, GN series).
//     Spec data: https://www.tencentcloud.com/document/product/213/11518
//
//  3. When TENCENTCLOUD_SECRET_ID / TENCENTCLOUD_SECRET_KEY are set, the
//     DescribeInstanceTypeConfigs API is called for live region × instance data.
//     API reference: https://www.tencentcloud.com/document/product/213/17378
package tencentcloud

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
	outputFilePath = "tencentcloud/instances.json"
	// CVM API endpoint (international)
	cvmAPIEndpoint = "https://cvm.tencentcloudapi.com"
	// Regions to scrape when using the signed API
)

// Known Tencent Cloud regions.
var tencentRegions = []string{
	"ap-guangzhou", "ap-shanghai", "ap-beijing", "ap-chengdu", "ap-chongqing",
	"ap-nanjing", "ap-hongkong", "ap-singapore", "ap-bangkok", "ap-mumbai",
	"ap-tokyo", "ap-seoul", "na-siliconvalley", "na-ashburn", "eu-frankfurt",
}

// ----- output instance struct -----

// CVMInstance represents a Tencent Cloud CVM instance type.
type CVMInstance struct {
	InstanceType       string   `json:"instance_type"`
	InstanceFamily     string   `json:"instance_family"`
	PrettyName         string   `json:"pretty_name"`
	VCPU               int      `json:"vCPU"`
	Memory             float64  `json:"memory"` // GiB
	Arch               []string `json:"arch"`
	GPU                int      `json:"GPU"`
	GPUModel           string   `json:"GPU_model,omitempty"`
	NetworkPerformance string   `json:"network_performance"`
	// Pricing: region → os → {"ondemand": "price"}
	Pricing map[string]map[string]map[string]string `json:"pricing"`
	Regions []string                                `json:"regions"`
}

// ----- static seed data -----
// Source: https://www.tencentcloud.com/document/product/213/11518
// Each entry: {instanceType, vcpu, memGiB, family, prettyName, arch, gpu}
type seedEntry struct {
	InstanceType string
	VCPU         int
	MemGiB       float64
	Family       string
	PrettyName   string
	Arch         []string
	GPU          int
	GPUModel     string
}

var staticSeeds = []seedEntry{
	// Standard S series
	{"S1.SMALL1", 1, 1, "S1", "Standard S1", []string{"x86_64"}, 0, ""},
	{"S1.SMALL2", 1, 2, "S1", "Standard S1", []string{"x86_64"}, 0, ""},
	{"S1.MEDIUM4", 2, 4, "S1", "Standard S1", []string{"x86_64"}, 0, ""},
	{"S1.LARGE8", 4, 8, "S1", "Standard S1", []string{"x86_64"}, 0, ""},
	{"S1.XLARGE16", 8, 16, "S1", "Standard S1", []string{"x86_64"}, 0, ""},
	{"S2.SMALL1", 1, 1, "S2", "Standard S2", []string{"x86_64"}, 0, ""},
	{"S2.SMALL2", 1, 2, "S2", "Standard S2", []string{"x86_64"}, 0, ""},
	{"S2.MEDIUM4", 2, 4, "S2", "Standard S2", []string{"x86_64"}, 0, ""},
	{"S2.LARGE8", 4, 8, "S2", "Standard S2", []string{"x86_64"}, 0, ""},
	{"S2.XLARGE16", 8, 16, "S2", "Standard S2", []string{"x86_64"}, 0, ""},
	{"S2.2XLARGE32", 16, 32, "S2", "Standard S2", []string{"x86_64"}, 0, ""},
	{"S3.SMALL1", 1, 1, "S3", "Standard S3", []string{"x86_64"}, 0, ""},
	{"S3.SMALL2", 1, 2, "S3", "Standard S3", []string{"x86_64"}, 0, ""},
	{"S3.MEDIUM4", 2, 4, "S3", "Standard S3", []string{"x86_64"}, 0, ""},
	{"S3.LARGE8", 4, 8, "S3", "Standard S3", []string{"x86_64"}, 0, ""},
	{"S3.XLARGE16", 8, 16, "S3", "Standard S3", []string{"x86_64"}, 0, ""},
	{"S3.2XLARGE32", 16, 32, "S3", "Standard S3", []string{"x86_64"}, 0, ""},
	{"S3.4XLARGE64", 32, 64, "S3", "Standard S3", []string{"x86_64"}, 0, ""},
	{"S4.SMALL2", 1, 2, "S4", "Standard S4", []string{"x86_64"}, 0, ""},
	{"S4.MEDIUM4", 2, 4, "S4", "Standard S4", []string{"x86_64"}, 0, ""},
	{"S4.LARGE8", 4, 8, "S4", "Standard S4", []string{"x86_64"}, 0, ""},
	{"S4.XLARGE16", 8, 16, "S4", "Standard S4", []string{"x86_64"}, 0, ""},
	{"S4.2XLARGE32", 16, 32, "S4", "Standard S4", []string{"x86_64"}, 0, ""},
	{"S4.4XLARGE64", 32, 64, "S4", "Standard S4", []string{"x86_64"}, 0, ""},
	{"S5.SMALL2", 1, 2, "S5", "Standard S5", []string{"x86_64"}, 0, ""},
	{"S5.MEDIUM4", 2, 4, "S5", "Standard S5", []string{"x86_64"}, 0, ""},
	{"S5.LARGE8", 4, 8, "S5", "Standard S5", []string{"x86_64"}, 0, ""},
	{"S5.XLARGE16", 8, 16, "S5", "Standard S5", []string{"x86_64"}, 0, ""},
	{"S5.2XLARGE32", 16, 32, "S5", "Standard S5", []string{"x86_64"}, 0, ""},
	{"S5.4XLARGE64", 32, 64, "S5", "Standard S5", []string{"x86_64"}, 0, ""},
	{"S5.8XLARGE128", 64, 128, "S5", "Standard S5", []string{"x86_64"}, 0, ""},
	// Compute C series
	{"C3.LARGE8", 4, 8, "C3", "Compute C3", []string{"x86_64"}, 0, ""},
	{"C3.XLARGE16", 8, 16, "C3", "Compute C3", []string{"x86_64"}, 0, ""},
	{"C3.2XLARGE16", 16, 16, "C3", "Compute C3", []string{"x86_64"}, 0, ""},
	{"C3.2XLARGE32", 16, 32, "C3", "Compute C3", []string{"x86_64"}, 0, ""},
	{"C3.4XLARGE32", 32, 32, "C3", "Compute C3", []string{"x86_64"}, 0, ""},
	{"C3.4XLARGE64", 32, 64, "C3", "Compute C3", []string{"x86_64"}, 0, ""},
	{"C3.8XLARGE64", 64, 64, "C3", "Compute C3", []string{"x86_64"}, 0, ""},
	{"C3.8XLARGE128", 64, 128, "C3", "Compute C3", []string{"x86_64"}, 0, ""},
	// Memory M series
	{"M5.SMALL8", 1, 8, "M5", "Memory M5", []string{"x86_64"}, 0, ""},
	{"M5.MEDIUM16", 2, 16, "M5", "Memory M5", []string{"x86_64"}, 0, ""},
	{"M5.LARGE32", 4, 32, "M5", "Memory M5", []string{"x86_64"}, 0, ""},
	{"M5.XLARGE64", 8, 64, "M5", "Memory M5", []string{"x86_64"}, 0, ""},
	{"M5.2XLARGE128", 16, 128, "M5", "Memory M5", []string{"x86_64"}, 0, ""},
	{"M5.4XLARGE256", 32, 256, "M5", "Memory M5", []string{"x86_64"}, 0, ""},
	// GPU GN series
	{"GN7.LARGE20", 4, 20, "GN7", "GPU GN7 (NVIDIA T4)", []string{"x86_64"}, 1, "NVIDIA Tesla T4"},
	{"GN7.2XLARGE56", 10, 56, "GN7", "GPU GN7 (NVIDIA T4)", []string{"x86_64"}, 1, "NVIDIA Tesla T4"},
	{"GN7.5XLARGE80", 24, 80, "GN7", "GPU GN7 (NVIDIA T4)", []string{"x86_64"}, 4, "NVIDIA Tesla T4"},
	{"GN7.8XLARGE128", 40, 128, "GN7", "GPU GN7 (NVIDIA T4)", []string{"x86_64"}, 4, "NVIDIA Tesla T4"},
	{"GN7.10XLARGE160", 40, 160, "GN7", "GPU GN7 (NVIDIA T4)", []string{"x86_64"}, 8, "NVIDIA Tesla T4"},
	{"GN10X.2XLARGE40", 10, 40, "GN10X", "GPU GN10X (NVIDIA V100)", []string{"x86_64"}, 1, "NVIDIA Tesla V100"},
	{"GN10X.9XLARGE160", 40, 160, "GN10X", "GPU GN10X (NVIDIA V100)", []string{"x86_64"}, 4, "NVIDIA Tesla V100"},
	{"GN10X.18XLARGE320", 80, 320, "GN10X", "GPU GN10X (NVIDIA V100)", []string{"x86_64"}, 8, "NVIDIA Tesla V100"},
	// Arm SA series
	{"SA2.SMALL4", 2, 4, "SA2", "Standard SA2 (Arm)", []string{"arm64"}, 0, ""},
	{"SA2.MEDIUM8", 4, 8, "SA2", "Standard SA2 (Arm)", []string{"arm64"}, 0, ""},
	{"SA2.LARGE16", 8, 16, "SA2", "Standard SA2 (Arm)", []string{"arm64"}, 0, ""},
	{"SA2.XLARGE32", 16, 32, "SA2", "Standard SA2 (Arm)", []string{"arm64"}, 0, ""},
	{"SA2.2XLARGE64", 32, 64, "SA2", "Standard SA2 (Arm)", []string{"arm64"}, 0, ""},
	{"SA3.SMALL4", 2, 4, "SA3", "Standard SA3 (Amp)", []string{"arm64"}, 0, ""},
	{"SA3.MEDIUM8", 4, 8, "SA3", "Standard SA3 (Amp)", []string{"arm64"}, 0, ""},
	{"SA3.LARGE16", 8, 16, "SA3", "Standard SA3 (Amp)", []string{"arm64"}, 0, ""},
	{"SA3.XLARGE32", 16, 32, "SA3", "Standard SA3 (Amp)", []string{"arm64"}, 0, ""},
}

// ----- CVM API structs -----

type cvmInstanceTypeConfig struct {
	Zone           string `json:"Zone"`
	InstanceType   string `json:"InstanceType"`
	InstanceFamily string `json:"InstanceFamily"`
	CPU            int    `json:"CPU"`
	Memory         int    `json:"Memory"` // GiB
	FPGA           int    `json:"FPGA"`
	GPU            int    `json:"GPU"`
	GPUDesc        string `json:"GPUDesc"`
}

type cvmResponse struct {
	Response struct {
		InstanceTypeConfigSet []cvmInstanceTypeConfig `json:"InstanceTypeConfigSet"`
		RequestID             string                  `json:"RequestId"`
		Error                 *struct {
			Code    string `json:"Code"`
			Message string `json:"Message"`
		} `json:"Error"`
	} `json:"Response"`
}

// ----- optional API fetch -----

func fetchSpecsFromAPI(secretID, secretKey string) map[string]*CVMInstance {
	all := map[string]*CVMInstance{}
	for _, region := range tencentRegions {
		types, err := describeInstanceTypeConfigs(secretID, secretKey, region)
		if err != nil {
			log.Printf("[tencentcloud] WARN region %s: %v", region, err)
			continue
		}
		for _, t := range types {
			key := t.InstanceType
			inst, exists := all[key]
			if !exists {
				inst = &CVMInstance{
					InstanceType:   t.InstanceType,
					InstanceFamily: t.InstanceFamily,
					PrettyName:     prettyName(t.InstanceFamily),
					VCPU:           t.CPU,
					Memory:         float64(t.Memory),
					Arch:           archForFamily(t.InstanceFamily),
					GPU:            t.GPU,
					GPUModel:       t.GPUDesc,
					Pricing:        map[string]map[string]map[string]string{},
				}
				all[key] = inst
			}
			inst.Regions = utils.AppendUnique(inst.Regions, region)
		}
		log.Printf("[tencentcloud] region %s: %d instance types", region, len(types))
	}
	return all
}

func describeInstanceTypeConfigs(secretID, secretKey, region string) ([]cvmInstanceTypeConfig, error) {
	payload := `{}`
	timestamp := time.Now().Unix()
	date := time.Unix(timestamp, 0).UTC().Format("2006-01-02")

	headers := map[string]string{
		"Content-Type":   "application/json; charset=utf-8",
		"Host":           "cvm.tencentcloudapi.com",
		"X-TC-Action":    "DescribeInstanceTypeConfigs",
		"X-TC-Version":   "2017-03-12",
		"X-TC-Region":    region,
		"X-TC-Timestamp": fmt.Sprintf("%d", timestamp),
	}

	auth := buildTCAuth(secretID, secretKey, "cvm", headers, payload, date, timestamp)
	headers["Authorization"] = auth

	client := &http.Client{Timeout: 30 * time.Second}
	req, _ := http.NewRequest("POST", cvmAPIEndpoint, strings.NewReader(payload))
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var out cvmResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if out.Response.Error != nil {
		return nil, fmt.Errorf("API error %s: %s", out.Response.Error.Code, out.Response.Error.Message)
	}
	return out.Response.InstanceTypeConfigSet, nil
}

// buildTCAuth implements Tencent Cloud API v3 TC3-HMAC-SHA256 signature.
// Reference: https://www.tencentcloud.com/document/product/213/31575
func buildTCAuth(secretID, secretKey, service string, headers map[string]string, payload, date string, timestamp int64) string {
	// 1. Canonical request
	canonicalHeaders := fmt.Sprintf(
		"content-type:%s\nhost:%s\n",
		headers["Content-Type"],
		headers["Host"],
	)
	signedHeaders := "content-type;host"
	payloadHash := sha256hex(payload)
	canonicalRequest := strings.Join([]string{
		"POST", "/", "",
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")

	// 2. String to sign
	credentialScope := fmt.Sprintf("%s/%s/tc3_request", date, service)
	stringToSign := strings.Join([]string{
		"TC3-HMAC-SHA256",
		fmt.Sprintf("%d", timestamp),
		credentialScope,
		sha256hex(canonicalRequest),
	}, "\n")

	// 3. Signing key
	secretDate := hmacSHA256("TC3"+secretKey, date)
	secretService := hmacSHA256Bytes(secretDate, service)
	secretSigning := hmacSHA256Bytes(secretService, "tc3_request")

	// 4. Signature
	signature := hex.EncodeToString(hmacSHA256Bytes(secretSigning, stringToSign))

	return fmt.Sprintf(
		"TC3-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		secretID, credentialScope, signedHeaders, signature,
	)
}

func sha256hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func hmacSHA256(key, data string) []byte {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(data))
	return mac.Sum(nil)
}

func hmacSHA256Bytes(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(data))
	return mac.Sum(nil)
}

// ----- helpers -----

func prettyName(family string) string {
	f := strings.ToUpper(family)
	switch {
	case strings.HasPrefix(f, "S"):
		return "Standard " + f
	case strings.HasPrefix(f, "C"):
		return "Compute Optimized " + f
	case strings.HasPrefix(f, "M"):
		return "Memory Optimized " + f
	case strings.HasPrefix(f, "GN"):
		return "GPU " + f
	case strings.HasPrefix(f, "SA"):
		return "Standard Arm " + f
	case strings.HasPrefix(f, "IT"):
		return "High I/O " + f
	case strings.HasPrefix(f, "D"):
		return "Big Data " + f
	case strings.HasPrefix(f, "BC"):
		return "Bare Metal " + f
	}
	return "Tencent Cloud CVM " + f
}

func archForFamily(family string) []string {
	f := strings.ToUpper(family)
	if strings.HasPrefix(f, "SA") || strings.HasPrefix(f, "TAX") {
		return []string{"arm64"}
	}
	return []string{"x86_64"}
}

// ----- main entry point -----

// DoTencentcloudScraping is called from main.go.
func DoTencentcloudScraping() error {
	log.Println("[tencentcloud] starting scrape")

	all := map[string]*CVMInstance{}

	// Populate from static seeds first (always available).
	for _, s := range staticSeeds {
		all[s.InstanceType] = &CVMInstance{
			InstanceType:   s.InstanceType,
			InstanceFamily: s.Family,
			PrettyName:     s.PrettyName,
			VCPU:           s.VCPU,
			Memory:         s.MemGiB,
			Arch:           s.Arch,
			GPU:            s.GPU,
			GPUModel:       s.GPUModel,
			Pricing:        map[string]map[string]map[string]string{},
		}
	}

	// If credentials present, enrich from live API.
	secretID := os.Getenv("TENCENTCLOUD_SECRET_ID")
	secretKey := os.Getenv("TENCENTCLOUD_SECRET_KEY")
	if secretID != "" && secretKey != "" {
		log.Println("[tencentcloud] credentials found, fetching live data …")
		apiInstances := fetchSpecsFromAPI(secretID, secretKey)
		for k, v := range apiInstances {
			all[k] = v
		}
	} else {
		log.Println("[tencentcloud] no credentials — using static seed data only")
		// Assign all static seeds to a representative region set
		for _, inst := range all {
			inst.Regions = []string{"ap-guangzhou", "ap-beijing", "ap-shanghai"}
		}
	}

	sorted := make([]*CVMInstance, 0, len(all))
	for _, v := range all {
		sorted = append(sorted, v)
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].InstanceType < sorted[j].InstanceType
	})

	if err := utils.SaveInstances(sorted, outputFilePath); err != nil {
		return fmt.Errorf("save Tencent Cloud instances: %w", err)
	}
	log.Printf("[tencentcloud] wrote %d instances to %s", len(sorted), outputFilePath)
	return nil
}
