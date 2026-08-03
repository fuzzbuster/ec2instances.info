package ec2

import (
	"context"
	"fmt"
	"github.com/fuzzbuster/ec2instances.info/aws/awsutils"
	"github.com/fuzzbuster/ec2instances.info/utils"
	"log"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

var OS_REMAP = map[string]string{
	"Windows": "mswin",
	"Linux":   "linux",
}

var R_VALUES_MAPPING = []string{
	"<5%", "5-10%", "10-15%", "15-20%", ">20%",
}

func getSpotDataPartial() (*spotDataPartial, error) {
	var spotData spotDataPartial
	err := awsutils.FetchDataFromAWSWebsite(
		"https://spot-bid-advisor.s3.amazonaws.com/spot-advisor-data.json",
		&spotData,
	)
	if err != nil {
		return nil, fmt.Errorf("fetch spot data: %w", err)
	}
	return &spotData, nil
}

func processSpotInterruptData(region string, os string, instance *EC2Instance, s int, r int, china bool) error {
	remap, ok := OS_REMAP[os]
	if !ok {
		if !china {
			utils.SendWarning("Spot interrupt data has unknown OS", os)
		}
		return nil
	}

	if r > len(R_VALUES_MAPPING) {
		utils.SendWarning("Spot interrupt data has unknown R value", r, "for", instance.InstanceType)
		return nil
	}
	rValue := R_VALUES_MAPPING[r]

	regionMap := instance.Pricing[region]
	if regionMap == nil {
		if !china {
			utils.SendWarning("Spot interrupt data has unknown region", region, "for", instance.InstanceType)
		}
		return nil
	}

	osResult, ok := regionMap[remap].(*EC2PricingData)
	if !ok {
		if !china {
			utils.SendWarning("Spot interrupt data has unknown OS", os, "for", instance.InstanceType)
		}
		return nil
	}

	osResult.PCTInterrupt = rValue
	osResult.PCTSavingsOD = &s
	onDemand := osResult.OnDemand
	if onDemand == "" {
		onDemand = "0"
	}
	onDemandPrice, err := awsutils.Floaty(onDemand)
	if err != nil {
		return err
	}
	estSpot := 0.01 * float64(100-s) * onDemandPrice
	if osResult.SpotAvg == 0 {
		osResult.SpotAvg = Price(estSpot)
	}
	return nil
}

type spotAdvisorData struct {
	S int `json:"s"`
	R int `json:"r"`
}

type spotDataPartial struct {
	SpotAdvisor map[string]map[string]map[string]spotAdvisorData `json:"spot_advisor"`
}

func addSpotInterruptInfo(instances map[string]*EC2Instance, spotDataPartialGetter func() (*spotDataPartial, error), china bool) error {
	log.Default().Println("Adding spot interrupt info to EC2")

	spotData, err := spotDataPartialGetter()
	if err != nil {
		return err
	}
	for region, operatingSystems := range spotData.SpotAdvisor {
		for os, spotAdvisorData := range operatingSystems {
			for instanceType, data := range spotAdvisorData {
				instance, ok := instances[instanceType]
				if !ok {
					if !china {
						utils.SendWarning("Spot interrupt data has unknown instance type", instanceType)
					}
					continue
				}

				if err := processSpotInterruptData(region, os, instance, data.S, data.R, china); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func int32Ptr(i int32) *int32 {
	return &i
}

func addSpotPricing(instances map[string]*EC2Instance, regions map[string]string) error {
	log.Default().Println("Adding spot pricing to EC2")

	var success uintptr
	var regionFg utils.FunctionGroup
	instancesMu := sync.Mutex{}
	for region := range regions {
		regionFg.Add(func() error {
			// Create a new configuration
			awsConfig, err := config.LoadDefaultConfig(context.Background(),
				config.WithRetryMaxAttempts(10),
				config.WithRetryMode(aws.RetryModeAdaptive),
			)
			if err != nil {
				return fmt.Errorf("load AWS config for region %s: %w", region, err)
			}
			awsConfig.Region = region
			ec2Client := ec2.NewFromConfig(awsConfig)

			// Setup the iterator
			instanceTypes := make([]types.InstanceType, 0, len(instances))
			for instanceType := range instances {
				instanceTypes = append(instanceTypes, types.InstanceType(instanceType))
			}
			now := time.Now()
			paginator := ec2.NewDescribeSpotPriceHistoryPaginator(ec2Client, &ec2.DescribeSpotPriceHistoryInput{
				InstanceTypes: instanceTypes,
				StartTime:     &now,
				MaxResults:    int32Ptr(100),
			})

			// Process the spot price history
			firstPage := true
			for paginator.HasMorePages() {
				output, err := paginator.NextPage(context.TODO())
				if err != nil {
					if firstPage {
						// NEVER allow a ratelimit error.
						if strings.Contains(err.Error(), "RateLimitExceeded") {
							return fmt.Errorf("EC2 region %s has a rate limit error: %w", region, err)
						}

						// Use us-east-1 as the canary to make sure this works
						// Otherwise, this is probably fine
						if region == "us-east-1" {
							return fmt.Errorf("get spot pricing for us-east-1: %w", err)
						}
						break
					} else {
						return fmt.Errorf("get next spot pricing page for %s: %w", region, err)
					}
				}
				firstPage = false
				atomic.AddUintptr(&success, 1)

				for _, price := range output.SpotPriceHistory {
					// Get the instance and platform this is relating to
					instancesMu.Lock()
					instance := instances[string(price.InstanceType)]
					if instance == nil {
						instancesMu.Unlock()
						return fmt.Errorf("EC2 spot pricing has unknown instance type %s", price.InstanceType)
					}
					platform := awsutils.TranslatePlatformName(
						string(price.ProductDescription),
						"NA",
					)
					az := *price.AvailabilityZone
					region := az[:len(az)-1]

					// Get the platform pricing data
					pricingData := instance.Pricing[region]
					created := false
					if pricingData == nil {
						created = true
						pricingData = make(map[OS]any)
						instance.Pricing[region] = pricingData
					}
					instancesMu.Unlock()
					osMap, _ := pricingData[platform].(*EC2PricingData)
					if osMap == nil {
						created = true
						osMap = &EC2PricingData{}
					}

					if created {
						// Newly created pricing data - add ourself as the only item
						value, err := awsutils.Floaty(*price.SpotPrice)
						if err != nil {
							return err
						}
						spotPrice := Price(value)
						osMap.spot = []Price{spotPrice}
						osMap.SpotMin = &spotPrice
						osMap.SpotMax = &spotPrice
					} else {
						// Append and sort everything
						if osMap.spot == nil {
							osMap.spot = make([]Price, 0)
						}
						value, err := awsutils.Floaty(*price.SpotPrice)
						if err != nil {
							return err
						}
						osMap.spot = append(osMap.spot, Price(value))
						slices.Sort(osMap.spot)
						osMap.SpotMin = &osMap.spot[0]
						osMap.SpotMax = &osMap.spot[len(osMap.spot)-1]
					}
					var avg Price = 0.0
					for _, spot := range osMap.spot {
						avg += spot
					}
					avg /= Price(len(osMap.spot))
					osMap.SpotAvg = Price(avg)
				}
			}
			return nil
		})
	}
	if err := regionFg.Run(); err != nil {
		return err
	}

	if success == 0 {
		return fmt.Errorf("EC2 spot pricing failed to get any data")
	}
	return nil
}
