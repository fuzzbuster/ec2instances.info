// Package hetzner scrapes Hetzner Cloud plans from public website data.
package hetzner

import (
	"encoding/json"
	"fmt"
	"github.com/fuzzbuster/ec2instances.info/utils"
	"html"
	"log"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	outputFilePath = "www/hetzner/instances.json"
	priceAPIURL    = "https://website-price-api.hetzner.com/api/v1/products/%s"
)

var productPages = []struct {
	family string
	url    string
}{
	{"Cost Optimized", "https://www.hetzner.com/cloud/cost-optimized/"},
	{"Regular Performance", "https://www.hetzner.com/cloud/regular-performance/"},
	{"General Purpose", "https://www.hetzner.com/cloud/general-purpose/"},
}

type Plan struct {
	Name       string
	Family     string
	VCPU       int
	Memory     float64
	Storage    int
	Arch       string
	ProductKey string
}

type PriceResponse struct {
	Locations []PriceLocation `json:"locations"`
}

type PriceLocation struct {
	CountryCode string `json:"countryCode"`
	Active      bool   `json:"active"`
	Datacenter  string `json:"datacenter"`
	Prices      struct {
		Hourly struct {
			USD string `json:"USD"`
			EUR string `json:"EUR"`
		} `json:"hourly"`
		Monthly struct {
			USD string `json:"USD"`
			EUR string `json:"EUR"`
		} `json:"monthly"`
	} `json:"prices"`
}

type VPSStorage struct {
	Size      int    `json:"size"`
	SizeUnit  string `json:"size_unit"`
	DiskType  string `json:"disk_type"`
	DiskCount int    `json:"disk_count"`
}

type VPSInstance struct {
	Provider           string                                   `json:"provider"`
	InstanceType       string                                   `json:"instance_type"`
	InstanceFamily     string                                   `json:"instance_family"`
	PrettyName         string                                   `json:"pretty_name"`
	VCPU               int                                      `json:"vCPU"`
	Memory             float64                                  `json:"memory"`
	Arch               []string                                 `json:"arch"`
	GPU                int                                      `json:"GPU"`
	Storage            VPSStorage                               `json:"storage"`
	NetworkPerformance string                                   `json:"network_performance"`
	Transfer           int                                      `json:"transfer"`
	Pricing            map[string]map[string]map[string]float64 `json:"pricing"`
	Regions            map[string]string                        `json:"regions"`
}

func DoHetznerScraping() {
	plans := fetchPlans()
	instances := make([]VPSInstance, 0, len(plans))

	for _, plan := range plans {
		instance := VPSInstance{
			Provider:           "hetzner",
			InstanceType:       plan.Name,
			InstanceFamily:     plan.Family,
			PrettyName:         fmt.Sprintf("Hetzner %s", plan.Name),
			VCPU:               plan.VCPU,
			Memory:             plan.Memory,
			Arch:               architecture(plan.Arch),
			GPU:                0,
			Storage:            VPSStorage{Size: plan.Storage, SizeUnit: "GB", DiskType: "NVMe SSD", DiskCount: 1},
			NetworkPerformance: "20 TB traffic included",
			Transfer:           20000,
			Pricing:            map[string]map[string]map[string]float64{},
			Regions:            map[string]string{},
		}

		for _, location := range fetchPrices(plan.ProductKey) {
			if !location.Active {
				continue
			}
			hourly := parsePrice(location.Prices.Hourly.USD)
			if hourly == 0 {
				monthly := parsePrice(location.Prices.Monthly.USD)
				if monthly > 0 {
					hourly = monthly / 730.0
				}
			}
			regionID := strings.ToLower(location.Datacenter)
			instance.Pricing[regionID] = map[string]map[string]float64{
				"linux": {"ondemand": hourly},
			}
			instance.Regions[regionID] = fmt.Sprintf("%s, %s", location.Datacenter, strings.ToUpper(location.CountryCode))
		}

		instances = append(instances, instance)
	}

	sort.Slice(instances, func(i, j int) bool {
		return instances[i].InstanceType < instances[j].InstanceType
	})

	utils.SaveInstances(instances, outputFilePath)
	log.Printf("[hetzner] wrote %d cloud plans", len(instances))
}

func fetchPlans() []Plan {
	plans := []Plan{}
	seen := map[string]struct{}{}

	for _, page := range productPages {
		body, err := utils.FetchWithRetry(page.url, nil)
		if err != nil {
			log.Fatalf("[hetzner] fetch %s: %v", page.url, err)
		}
		for _, plan := range parsePlans(string(body), page.family) {
			if _, ok := seen[plan.Name]; ok {
				continue
			}
			seen[plan.Name] = struct{}{}
			plans = append(plans, plan)
		}
	}
	if len(plans) == 0 {
		log.Fatal("[hetzner] no cloud plans found")
	}
	return plans
}

func parsePlans(pageHTML string, family string) []Plan {
	rowRE := regexp.MustCompile(`(?s)<div class="cloud-matrix-table-row">(.*?product-key="[^"]+".*?)<div class="table-card">`)
	nameRE := regexp.MustCompile(`(?s)<div class="name-cell">\s*<div class="">\s*([^<]+)`)
	cpuRE := regexp.MustCompile(`(?s)<div class="cpu-cell">\s*<img[^>]*>\s*([0-9]+)\s*(?:<div class="arch-type-badge">\s*([^<]+)\s*</div>)?`)
	ramRE := regexp.MustCompile(`(?s)<div class="ram-cell">.*?([0-9.]+)\s*GB`)
	driveRE := regexp.MustCompile(`(?s)<div class="drive-cell">.*?([0-9]+)\s*GB`)
	keyRE := regexp.MustCompile(`product-key="([^"]+)"`)

	plans := []Plan{}
	for _, match := range rowRE.FindAllStringSubmatch(pageHTML, -1) {
		row := match[1]
		name := submatch(row, nameRE, 1)
		if name == "" {
			continue
		}
		cpuMatch := cpuRE.FindStringSubmatch(row)
		if len(cpuMatch) < 2 {
			continue
		}
		vcpu, _ := strconv.Atoi(cpuMatch[1])
		arch := ""
		if len(cpuMatch) > 2 {
			arch = strings.TrimSpace(html.UnescapeString(cpuMatch[2]))
		}
		ram, _ := strconv.ParseFloat(submatch(row, ramRE, 1), 64)
		storage, _ := strconv.Atoi(submatch(row, driveRE, 1))
		productKey := submatch(row, keyRE, 1)
		if vcpu == 0 || ram == 0 || storage == 0 || productKey == "" {
			continue
		}

		plans = append(plans, Plan{
			Name:       strings.TrimSpace(html.UnescapeString(name)),
			Family:     family,
			VCPU:       vcpu,
			Memory:     ram,
			Storage:    storage,
			Arch:       arch,
			ProductKey: productKey,
		})
	}
	return plans
}

func fetchPrices(productKey string) []PriceLocation {
	url := fmt.Sprintf(priceAPIURL, productKey)
	body, err := utils.FetchWithRetry(url, nil)
	if err != nil {
		log.Fatalf("[hetzner] fetch prices for %s: %v", productKey, err)
	}

	var response PriceResponse
	if err := json.Unmarshal(body, &response); err != nil {
		log.Fatalf("[hetzner] parse prices for %s: %v", productKey, err)
	}
	return response.Locations
}

func submatch(value string, re *regexp.Regexp, index int) string {
	match := re.FindStringSubmatch(value)
	if len(match) <= index {
		return ""
	}
	return strings.TrimSpace(match[index])
}

func parsePrice(value string) float64 {
	price, _ := strconv.ParseFloat(value, 64)
	return price
}

func architecture(value string) []string {
	value = strings.ToLower(value)
	if strings.Contains(value, "arm") || strings.Contains(value, "ampere") {
		return []string{"arm64"}
	}
	return []string{"x86_64"}
}
