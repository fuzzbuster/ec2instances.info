// Package digitalocean scrapes DigitalOcean Droplet plans from public website data.
package digitalocean

import (
	"encoding/json"
	"fmt"
	"github.com/fuzzbuster/ec2instances.info/utils"
	"log"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	outputFilePath  = "digitalocean/instances.json"
	pricingURL      = "https://www.digitalocean.com/pricing/droplets"
	availabilityURL = "https://docs.digitalocean.com/platform/regional-availability/index.html.md"
)

type Plan struct {
	Memory float64 `json:"memory"`
	CPUs   int     `json:"cpus"`
	Disk   struct {
		Boot int `json:"boot"`
	} `json:"disk"`
	Network struct {
		Throughput float64 `json:"throughput"`
	} `json:"network"`
	Price struct {
		TransferQuota int     `json:"transferQuota"`
		Hourly        float64 `json:"hourly"`
		Monthly       float64 `json:"monthly"`
	} `json:"price"`
	Slug string `json:"slug"`
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

func DoDigitalOceanScraping() error {
	regions, err := fetchRegions()
	if err != nil {
		return err
	}
	availability, err := fetchDropletAvailability(regions)
	if err != nil {
		return err
	}
	plans, err := fetchPlans()
	if err != nil {
		return err
	}
	instances := make([]VPSInstance, 0, len(plans))

	for _, plan := range plans {
		family := instanceFamily(plan.Slug)
		instance := VPSInstance{
			Provider:           "digitalocean",
			InstanceType:       plan.Slug,
			InstanceFamily:     family,
			PrettyName:         fmt.Sprintf("DigitalOcean %s", plan.Slug),
			VCPU:               plan.CPUs,
			Memory:             plan.Memory,
			Arch:               []string{"x86_64"},
			GPU:                0,
			Storage:            VPSStorage{Size: plan.Disk.Boot, SizeUnit: "GB", DiskType: "SSD", DiskCount: 1},
			NetworkPerformance: networkPerformance(plan.Network.Throughput),
			Transfer:           plan.Price.TransferQuota,
			Pricing:            map[string]map[string]map[string]float64{},
			Regions:            map[string]string{},
		}

		for _, regionID := range availability[family] {
			instance.Pricing[regionID] = map[string]map[string]float64{
				"linux": {"ondemand": plan.Price.Hourly},
			}
			instance.Regions[regionID] = regions[regionID]
		}

		instances = append(instances, instance)
	}

	sort.Slice(instances, func(i, j int) bool {
		return instances[i].InstanceType < instances[j].InstanceType
	})

	if err := utils.SaveInstances(instances, outputFilePath); err != nil {
		return fmt.Errorf("save DigitalOcean instances: %w", err)
	}
	log.Printf("[digitalocean] wrote %d droplet plans", len(instances))
	return nil
}

func fetchPlans() ([]Plan, error) {
	body, err := utils.FetchWithRetry(pricingURL, nil)
	if err != nil {
		return nil, fmt.Errorf("fetch DigitalOcean pricing page: %w", err)
	}

	unescaped := strings.ReplaceAll(string(body), `\"`, `"`)
	matches := regexp.MustCompile(`\{"memory":[0-9.]+,"cpus":[0-9]+,"disk":\{[^}]*"boot":[0-9]+[^}]*\},"network":\{"throughput":[0-9.]+\}(?:,"api":(?:true|false))?,"price":\{"transferQuota":[0-9]+,"hourly":[0-9.]+,"monthly":[0-9.]+\},"slug":"[^"]+"\}`).FindAllString(unescaped, -1)

	seen := map[string]struct{}{}
	plans := make([]Plan, 0, len(matches))
	for _, raw := range matches {
		var plan Plan
		if err := json.Unmarshal([]byte(raw), &plan); err != nil {
			return nil, fmt.Errorf("parse DigitalOcean plan: %w", err)
		}
		if _, ok := seen[plan.Slug]; ok {
			continue
		}
		seen[plan.Slug] = struct{}{}
		plans = append(plans, plan)
	}
	if len(plans) == 0 {
		return nil, fmt.Errorf("no DigitalOcean droplet plans found")
	}
	return plans, nil
}

func fetchRegions() (map[string]string, error) {
	body, err := utils.FetchWithRetry(availabilityURL, nil)
	if err != nil {
		return nil, fmt.Errorf("fetch DigitalOcean regional availability: %w", err)
	}

	regions := map[string]string{}
	rowRE := regexp.MustCompile(`(?m)^\| ([A-Z0-9]+) \| ([^|]+) \| ` + "`" + `([a-z0-9]+)` + "`" + ` \|$`)
	for _, match := range rowRE.FindAllStringSubmatch(string(body), -1) {
		regions[match[3]] = strings.TrimSpace(match[2])
	}
	if len(regions) == 0 {
		return nil, fmt.Errorf("no DigitalOcean regions found")
	}
	return regions, nil
}

func fetchDropletAvailability(regions map[string]string) (map[string][]string, error) {
	body, err := utils.FetchWithRetry(availabilityURL, nil)
	if err != nil {
		return nil, fmt.Errorf("fetch DigitalOcean droplet availability: %w", err)
	}

	lines := strings.Split(string(body), "\n")
	header := []string{}
	availability := map[string][]string{}
	for _, line := range lines {
		if strings.HasPrefix(line, "| Droplet Plans |") {
			header = markdownCells(line)[1:]
			continue
		}
		if len(header) == 0 || !strings.HasPrefix(line, "| ") {
			continue
		}

		cells := markdownCells(line)
		if len(cells) != len(header)+1 {
			if len(availability) > 0 {
				break
			}
			continue
		}
		family := strings.TrimSpace(cells[0])
		if family == "---" {
			continue
		}
		for i, cell := range cells[1:] {
			if cell != "✓" {
				continue
			}
			regionID := strings.ToLower(header[i])
			if _, ok := regions[regionID]; ok {
				availability[family] = append(availability[family], regionID)
			}
		}
	}
	if len(availability) == 0 {
		return nil, fmt.Errorf("no DigitalOcean droplet availability found")
	}
	return availability, nil
}

func markdownCells(line string) []string {
	parts := strings.Split(strings.Trim(line, "|"), "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func instanceFamily(slug string) string {
	switch {
	case strings.HasPrefix(slug, "g-"), strings.HasPrefix(slug, "gd-"), strings.HasPrefix(slug, "g5_"), strings.HasPrefix(slug, "g6_"):
		return "General Purpose"
	case strings.HasPrefix(slug, "c-"), strings.HasPrefix(slug, "c2-"), strings.HasPrefix(slug, "c5-"):
		return "CPU-Optimized"
	case strings.HasPrefix(slug, "m-"), strings.HasPrefix(slug, "m3-"), strings.HasPrefix(slug, "m6-"):
		return "Memory-Optimized"
	case strings.HasPrefix(slug, "so-"), strings.HasPrefix(slug, "so1_"):
		return "Storage-Optimized"
	default:
		return "Basic"
	}
}

func networkPerformance(gbps float64) string {
	if gbps <= 0 {
		return "Unknown"
	}
	return strconv.FormatFloat(gbps, 'f', -1, 64) + " Gbps"
}
