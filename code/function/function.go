// Copyright 2021 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package hello

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"google.golang.org/api/compute/v1"
)

// PubSubMessage is the payload of a Pub/Sub event.
// See the documentation for more details:
// https://cloud.google.com/pubsub/docs/reference/rest/v1/PubsubMessage
type PubSubMessage struct {
	Data []byte `json:"data"`
}

// BillingNotice is a translation of the data that gets sent via PUB/SUB when
// there is a billing overage
type BillingNotice struct {
	Name              string    `json:"budgetDisplayName"`
	ThresholdExceeded float32   `json:"alertThresholdExceeded"`
	Cost              float32   `json:"costAmount"`
	CostStart         time.Time `json:"costIntervalStart"`
	Budget            float32   `json:"budgetAmount"`
	BudgetType        string    `json:"budgetAmountType"`
	Code              string    `json:"currencyCode"`
}

// LimitUsage will read in Billing overages and shut down machine's accordingly.
func LimitUsage(ctx context.Context, m PubSubMessage) error {
	project := os.Getenv("GOOGLE_CLOUD_PROJECT")
	label := os.Getenv("LABEL")

	data := string(m.Data)

	notice := BillingNotice{}
	if err := json.Unmarshal([]byte(data), &notice); err != nil {
		return fmt.Errorf("cannot unmarshall Pub/Sub message")
	}

	if notice.Cost <= notice.Budget {
		fmt.Println("Underbudget, no action needed.")
		return nil
	}
	fmt.Println("Overbudget, stopping VMs.")

	l := fmt.Sprintf("labels.%s = true", label)
	filters := []string{
		"status = RUNNING",
		l,
	}

	computeService, err := compute.NewService(ctx)
	if err != nil {
		fmt.Printf("error: %s \n", err)
		return err
	}

	instances, err := getInstances(project, computeService, filters)
	if err != nil {
		fmt.Printf("error: %s \n", err)
		return err
	}

	if err := stopInstances(project, computeService, instances); err != nil {
		fmt.Printf("error: %s \n", err)
		return err
	}

	fmt.Printf("Cost Sentry stopped %d instances\n", len(instances.Items))
	return nil
}

func stopInstances(project string, computeService *compute.Service, instanceList *compute.InstanceList) error {
	for _, v := range instanceList.Items {
		zoneStr := strings.Split(v.Zone, "/")
		zone := zoneStr[len(zoneStr)-1]
		stopCall := computeService.Instances.Stop(project, zone, v.Name)

		if _, err := stopCall.Do(); err != nil {
			return fmt.Errorf("error stopInstances: %s", err)
		}
	}

	return nil
}

func getInstances(project string, computeService *compute.Service, filters []string) (*compute.InstanceList, error) {
	instances := &compute.InstanceList{}
	zoneListCall := computeService.Zones.List(project)
	zoneList, err := zoneListCall.Do()
	if err != nil {
		return nil, fmt.Errorf("error - getInstances cannot get zone list")
	}

	var wg sync.WaitGroup
	wg.Add(len(zoneList.Items))

	for _, zone := range zoneList.Items {
		go func(zone *compute.Zone) error {
			defer wg.Done()
			instanceListCall := computeService.Instances.List(project, zone.Name)
			instanceListCall.Filter(strings.Join(filters[:], " "))
			instanceList, err := instanceListCall.Do()
			if err != nil {
				return fmt.Errorf("error - getInstances cannot get instance list")
			}
			instances.Items = append(instances.Items, instanceList.Items...)
			return nil
		}(zone)
	}
	wg.Wait()
	return instances, nil
}

// Parse billing pubsub message
// Determine if you should deactivate machines
// if so get a list of machines to deactivate
// deactivate them
